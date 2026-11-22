package httpx

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/catalog"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/fleet"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/mcp"
	"ovh-nexus/server/internal/monitor"
	"ovh-nexus/server/internal/notify"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/procurement"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
	"ovh-nexus/server/internal/webui"
)

// Version identifies the OVH Nexus Go backend release version.
const Version = "1.4.0"

// Services holds all wired OVH Nexus domain services and shared infrastructure.
type Services struct {
	Config      *config.Config
	Vault       *vault.Vault
	Store       *store.Store
	Log         *logger.Hub
	LogsHandler *logger.LogsHandler
	OVHPool     *ovhclient.Pool
	Auth        *auth.Service
	Notify      *notify.Dispatcher
	Catalog     *catalog.Service
	Procurement *procurement.Service
	Monitor     *monitor.Service
	Fleet       *fleet.Service
	MCP         *mcp.Service
	WebUI       http.Handler

	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}

// OpenAndWire initializes Vault (wiping the raw encryption key from cfg immediately),
// opens the encrypted SQLite store, and wires all domain services together.
func OpenAndWire(ctx context.Context, cfg *config.Config) (*Services, error) {
	if cfg == nil {
		return nil, errors.New("httpx: config must not be nil")
	}

	v, err := vault.New(cfg.DataEncryptionKey)
	if err != nil {
		return nil, err
	}
	cfg.WipeEncryptionKey()

	st, err := store.Open(cfg.DataDir, v)
	if err != nil {
		return nil, err
	}

	svcs, err := WireServices(ctx, cfg, v, st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return svcs, nil
}

// WireServices constructs and connects all OVH Nexus services around an initialized Config, Vault, and Store.
func WireServices(ctx context.Context, cfg *config.Config, v *vault.Vault, st *store.Store) (*Services, error) {
	if cfg == nil {
		return nil, errors.New("httpx: config must not be nil")
	}
	if v == nil {
		return nil, errors.New("httpx: vault must not be nil")
	}
	if st == nil {
		return nil, errors.New("httpx: store must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	logHub := logger.NewHub(st, logger.Options{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	})
	logHub.AddSensitiveValue(cfg.APISecretKey)
	logHub.AddSensitiveValue(cfg.InternalToken)

	logsHandler := logger.NewLogsHandler(logHub, st)
	ovhPool := ovhclient.NewPool(st, logHub)

	authSvc, err := auth.NewService(ctx, cfg, st, logHub)
	if err != nil {
		return nil, err
	}

	notifyDisp := notify.NewDispatcher(st, v, logHub, ovhPool)
	catSvc := catalog.NewService(st, logHub, ovhPool, cfg.DataDir)
	procSvc := procurement.NewService(st, logHub, ovhPool, catSvc, notifyDisp)
	monSvc := monitor.NewService(st, logHub, ovhPool, catSvc, procSvc, notifyDisp)
	catSvc.SetListedAtResolver(monSvc.ResolveSKUListedAt)
	_, _, _ = monSvc.GetSKUStatusAndChanges(ctx)
	fleetSvc := fleet.NewService(cfg, st, ovhPool, authSvc, logHub)

	fleetSvc.SetOnAccountDeleted(func(accountID string) {
		ovhPool.Invalidate(accountID)
		pruneDeletedAccountFromMCP(context.Background(), st, accountID)
	})

	if err := fleetSvc.RegisterAccountSecrets(ctx); err != nil {
		return nil, err
	}
	if err := RegisterChannelSecrets(ctx, st, logHub); err != nil {
		return nil, err
	}

	mcpSvc, err := mcp.NewService(mcp.Dependencies{
		Config:      cfg,
		Store:       st,
		OVHPool:     ovhPool,
		Auth:        authSvc,
		TOTP:        authSvc,
		Log:         logHub,
		Catalog:     &catalogAdapter{cat: catSvc, store: st, mon: monSvc},
		Procurement: &procurementAdapter{proc: procSvc},
		Monitor:     &monitorAdapter{mon: monSvc, store: st, cat: catSvc},
		Notifier:    &notifyAdapter{disp: notifyDisp},
	})
	if err != nil {
		return nil, err
	}

	return &Services{
		Config:      cfg,
		Vault:       v,
		Store:       st,
		Log:         logHub,
		LogsHandler: logsHandler,
		OVHPool:     ovhPool,
		Auth:        authSvc,
		Notify:      notifyDisp,
		Catalog:     catSvc,
		Procurement: procSvc,
		Monitor:     monSvc,
		Fleet:       fleetSvc,
		MCP:         mcpSvc,
		WebUI:       webui.Handler(),
		shutdownCh:  make(chan struct{}),
	}, nil
}

// Start launches background procurement and monitoring worker loops.
func (s *Services) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if s.Procurement != nil {
		s.Procurement.Start(ctx)
	}
	if s.Monitor != nil {
		s.Monitor.Start(ctx)
	}
}

// SignalShutdown closes active long-lived SSE streams so http.Server.Shutdown completes promptly.
func (s *Services) SignalShutdown() {
	if s == nil {
		return
	}
	s.shutdownOnce.Do(func() {
		if s.shutdownCh != nil {
			close(s.shutdownCh)
		}
		if s.Log != nil {
			s.Log.Close()
		}
	})
}

// Close closes the underlying SQLite database store.
func (s *Services) Close() error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.Close()
}

// NewRouter builds the complete chi HTTP router with security middleware, auth gates,
// MCP Streamable HTTP endpoint, domain API routes, and embedded WebUI SPA fallback.
func NewRouter(svcs *Services) http.Handler {
	if svcs == nil {
		panic("httpx: services must not be nil")
	}

	r := chi.NewRouter()

	var corsOrigins []string
	cookieSecure := false
	if svcs.Config != nil {
		corsOrigins = svcs.Config.CORSOrigins
		cookieSecure = svcs.Config.CookieSecure
	}

	r.Use(RecoveryMiddleware(svcs.Log))
	r.Use(SecurityHeadersWithConfig(cookieSecure))
	r.Use(CORSMiddleware(corsOrigins))
	r.Use(CSRFProtectionMiddleware(corsOrigins))
	r.Use(MaxBodySizeMiddleware(MaxRequestBodyBytes))
	r.Use(GzipMiddleware)

	r.Get("/health", handleHealth)

	r.Route("/api", func(api chi.Router) {
		// Whitelisted routes outside the session-cookie gate.
		api.Get("/health", handleHealth)
		if svcs.Auth != nil {
			api.Post("/auth/login", svcs.Auth.HandleLogin)
			api.Post("/auth/logout", svcs.Auth.HandleLogout)
			api.Get("/auth/status", svcs.Auth.HandleStatus)
		}

		// MCP Streamable HTTP JSON-RPC 2.0 endpoint uses its own Bearer token auth.
		if svcs.MCP != nil {
			svcs.MCP.MountMCPRoutes(api)
			api.HandleFunc("/mcp/", svcs.MCP.HandleMCPStreamableHTTP)
		}

		// All remaining /api/* routes are gated by session cookie / loopback API key and 2FA tiers.
		api.Group(func(protected chi.Router) {
			if svcs.Auth != nil {
				protected.Use(svcs.Auth.RequireAuthAndTOTPGate)
			}
			protected.Use(sseStreamMiddleware(svcs.shutdownCh))

			if svcs.Auth != nil {
				protected.Get("/auth/totp/status", svcs.Auth.HandleTOTPStatus)
				protected.Post("/auth/totp/setup", svcs.Auth.HandleTOTPSetup)
				protected.Post("/auth/totp/activate", svcs.Auth.HandleTOTPActivate)
				protected.Post("/auth/totp/verify-sudo", svcs.Auth.HandleTOTPVerifySudo)
				protected.Post("/auth/totp/disable", svcs.Auth.HandleTOTPDisable)
				protected.Post("/auth/totp/recovery-codes/regenerate", svcs.Auth.HandleTOTPRegenerateRecovery)
			}

			if svcs.LogsHandler != nil {
				svcs.LogsHandler.MountRoutes(protected)
			} else if svcs.Log != nil {
				logger.NewLogsHandler(svcs.Log, svcs.Store).MountRoutes(protected)
			}
			if svcs.Notify != nil {
				svcs.Notify.MountRoutes(protected)
			}
			if svcs.Catalog != nil {
				svcs.Catalog.MountRoutes(protected)
			}
			if svcs.Procurement != nil {
				svcs.Procurement.MountRoutes(protected)
			}
			if svcs.Monitor != nil {
				svcs.Monitor.MountRoutes(protected)
			}
			if svcs.Fleet != nil {
				svcs.Fleet.MountRoutes(protected)
			}
			if svcs.MCP != nil {
				svcs.MCP.MountAdminRoutes(protected)
			}

			protected.HandleFunc("/", writeAPINotFoundJSON)
			protected.HandleFunc("/*", writeAPINotFoundJSON)
		})

		api.MethodNotAllowed(writeAPIMethodNotAllowedJSON)
		api.NotFound(writeAPINotFoundJSON)
	})

	webUI := svcs.WebUI
	if webUI == nil {
		webUI = webui.Handler()
	}
	r.Handle("/", webUI)
	r.NotFound(webUI.ServeHTTP)
	r.MethodNotAllowed(webUI.ServeHTTP)

	return r
}

// NewHTTPServer builds a hardened *http.Server configured against Slowloris and oversized headers.
func NewHTTPServer(cfg *config.Config, handler http.Handler) *http.Server {
	port := "8080"
	if cfg != nil && cfg.Port != "" {
		port = cfg.Port
	}
	return &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": Version,
	})
}

func writeAPINotFoundJSON(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "API endpoint not found.")
}

func writeAPIMethodNotAllowedJSON(w http.ResponseWriter, _ *http.Request) {
	writeJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "HTTP method is not allowed for this endpoint.")
}
