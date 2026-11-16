package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/httpx"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "OVH Nexus configuration error: %v\n", err)
		return 1
	}

	v, err := vault.New(cfg.DataEncryptionKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OVH Nexus vault initialization error: %v\n", err)
		return 1
	}
	// Wipe raw master encryption key from Config immediately after Vault derives its AES-256-GCM key.
	cfg.WipeEncryptionKey()

	st, err := store.Open(cfg.DataDir, v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OVH Nexus database initialization error: %v\n", err)
		return 1
	}

	initCtx := context.Background()
	svcs, err := httpx.WireServices(initCtx, cfg, v, st)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OVH Nexus service wiring error: %v\n", err)
		_ = st.Close()
		return 1
	}

	serverLog := svcs.Log.WithSource(logger.SourceServer)
	serverLog.Info("OVH Nexus Go server starting",
		slog.String("version", httpx.Version),
		slog.String("port", cfg.Port),
		slog.String("dataDir", cfg.DataDir),
		slog.String("tz", cfg.TZ),
		slog.String("logLevel", cfg.LogLevel),
		slog.String("logFormat", cfg.LogFormat),
		slog.Bool("cookieSecure", cfg.CookieSecure),
		slog.String("defaultLocale", cfg.DefaultLocale),
	)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	// Start procurement queue processor and background stock/VPS/SKU/lifecycle monitors.
	svcs.Start(workerCtx)

	router := httpx.NewRouter(svcs)
	srv := httpx.NewHTTPServer(cfg, router)

	errCh := make(chan error, 1)
	go func() {
		serverLog.Info("HTTP listener ready", slog.String("addr", srv.Addr))
		if listenErr := srv.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
			return
		}
		errCh <- nil
	}()

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	exitCode := 0
	select {
	case listenErr := <-errCh:
		if listenErr != nil {
			serverLog.Error("HTTP server terminated unexpectedly", slog.String("error", listenErr.Error()))
			exitCode = 1
		}
	case <-sigCtx.Done():
		serverLog.Info("Shutdown signal received; stopping OVH Nexus server")
	}

	// 1. Signal long-lived SSE connections to terminate so HTTP shutdown does not stall.
	svcs.SignalShutdown()

	// 2. Gracefully drain active HTTP requests.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		serverLog.Warn("HTTP server forced close after shutdown timeout", slog.String("error", err.Error()))
		_ = srv.Close()
		exitCode = 1
	}
	shutdownCancel()

	// 3. Stop background procurement and monitor worker goroutines.
	workerCancel()

	// 4. Emit final log entry while SQLite is still open, then close SQLite cleanly.
	serverLog.Info("OVH Nexus server shutdown complete")
	if err := svcs.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "OVH Nexus database close error: %v\n", err)
		exitCode = 1
	}
	return exitCode
}
