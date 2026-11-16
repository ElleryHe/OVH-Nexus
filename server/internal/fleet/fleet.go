package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
)

const maxRequestBodyBytes = 2 << 20

// OVHClient abstracts authenticated OVHcloud API calls (satisfied by *ovh.Client).
type OVHClient interface {
	GetWithContext(ctx context.Context, path string, resType any) error
	PostWithContext(ctx context.Context, path string, reqBody, resType any) error
	PutWithContext(ctx context.Context, path string, reqBody, resType any) error
	DeleteWithContext(ctx context.Context, path string, resType any) error
}

// Service implements Cloud Fleet, Multi-Account, Settings, Account Hub,
// and Dedicated Server Control HTTP handlers.
type Service struct {
	cfg     *config.Config
	store   *store.Store
	ovhPool *ovhclient.Pool
	auth    *auth.Service
	log     *logger.Hub

	ipmiPollInterval time.Duration
	onAccountDeleted func(accountID string)

	clientForRequestFn func(r *http.Request) (OVHClient, *store.OVHAccount, error)
	clientForAccountFn func(ctx context.Context, accountID string) (OVHClient, *store.OVHAccount, error)
	newCredClientFn    func(endpoint, appKey, appSecret, consumerKey string) (OVHClient, error)
}

// NewService creates a new Cloud Fleet HTTP service.
func NewService(
	cfg *config.Config,
	st *store.Store,
	ovhPool *ovhclient.Pool,
	authSvc *auth.Service,
	log *logger.Hub,
) *Service {
	s := &Service{
		cfg:              cfg,
		store:            st,
		ovhPool:          ovhPool,
		auth:             authSvc,
		log:              log,
		ipmiPollInterval: 2 * time.Second,
	}
	s.clientForRequestFn = func(r *http.Request) (OVHClient, *store.OVHAccount, error) {
		if s.ovhPool == nil {
			return nil, nil, ovhclient.ErrCredentialsMissing
		}
		c, acc, err := s.ovhPool.ClientFromRequest(r)
		if err != nil {
			return nil, acc, err
		}
		return c, acc, nil
	}
	s.clientForAccountFn = func(ctx context.Context, accountID string) (OVHClient, *store.OVHAccount, error) {
		if s.ovhPool == nil {
			return nil, nil, ovhclient.ErrCredentialsMissing
		}
		c, acc, err := s.ovhPool.ClientForAccount(ctx, accountID)
		if err != nil {
			return nil, acc, err
		}
		return c, acc, nil
	}
	s.newCredClientFn = func(endpoint, appKey, appSecret, consumerKey string) (OVHClient, error) {
		return ovhclient.NewClientWithCredentials(endpoint, appKey, appSecret, consumerKey)
	}
	return s
}

// SetOnAccountDeleted registers an optional cleanup callback triggered after account deletion.
func (s *Service) SetOnAccountDeleted(fn func(accountID string)) {
	s.onAccountDeleted = fn
}

// RegisterAccountSecrets loads stored accounts and registers their credential values
// in the logger redaction hub so secrets never leak into logs.
func (s *Service) RegisterAccountSecrets(ctx context.Context) error {
	if s.store == nil || s.log == nil {
		return nil
	}
	accounts, err := s.store.ListAccounts(ctx)
	if err != nil {
		return err
	}
	for _, acc := range accounts {
		s.registerSecrets(acc.AppKey, acc.AppSecret, acc.ConsumerKey)
	}
	return nil
}

func (s *Service) registerSecrets(values ...string) {
	if s.log == nil {
		return
	}
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			s.log.AddSensitiveValue(v)
		}
	}
}

// MountRoutes mounts all Fleet, Settings, Account Hub, and Server Control endpoints
// onto an /api chi.Router.
func (s *Service) MountRoutes(r chi.Router) {
	// Accounts, Settings, Auth Verification & Diagnostics
	r.Get("/accounts", s.HandleListAccounts)
	r.Post("/accounts", s.HandleUpsertAccount)
	r.Put("/accounts/default", s.HandleSetDefaultAccount)
	r.Post("/accounts/resolve-info", s.HandleResolveAccountInfo)
	r.Delete("/accounts/{id}", s.HandleDeleteAccount)

	r.Get("/settings", s.HandleGetSettings)
	r.Post("/settings", s.HandleSaveSettings)
	r.Get("/verify-auth", s.HandleVerifyAuth)
	r.Post("/verify-auth", s.HandleVerifyAuth)
	r.Get("/endpoint-config", s.HandleEndpointConfig)
	r.Get("/debug/account", s.HandleDebugAccount)

	// Account Hub (/api/ovh/*)
	r.Route("/ovh", func(r chi.Router) {
		r.Get("/account/info", s.HandleAccountInfo)
		r.Get("/account/orders", s.HandleAccountOrders)
		r.Get("/account/orders/{orderId}/follow-up", s.HandleOrderFollowUp)
		r.Get("/account/services-overview", s.HandleServicesOverview)

		r.Get("/account/ssh-keys", s.HandleListSSHKeys)
		r.Post("/account/ssh-keys", s.HandleAddSSHKey)
		r.Put("/account/ssh-keys/*", s.HandleUpdateSSHKey)
		r.Delete("/account/ssh-keys/*", s.HandleDeleteSSHKey)

		r.Get("/vps/list", s.HandleListVPS)
		r.Post("/vps/{serviceName}/reboot", s.HandleRebootVPS)

		r.Get("/account/refunds", s.HandleAccountRefunds)
		r.Get("/account/email-history", s.HandleEmailHistory)
		r.Get("/account/credit-balance", s.HandleCreditBalance)
		r.Get("/account/sub-accounts", s.HandleSubAccounts)
		r.Get("/account/bills", s.HandleAccountBills)

		r.Get("/contact-change-requests", s.HandleListContactChangeRequests)
		r.Get("/contact-change-requests/{taskId}", s.HandleGetContactChangeRequest)
		r.Post("/contact-change-requests/{taskId}/accept", s.HandleAcceptContactChange)
		r.Post("/contact-change-requests/{taskId}/refuse", s.HandleRefuseContactChange)
		r.Post("/contact-change-requests/{taskId}/resend-email", s.HandleResendContactChangeEmail)
	})

	// Dedicated Server Control (/api/server-control/*)
	r.Route("/server-control", func(r chi.Router) {
		r.Get("/list", s.HandleListDedicatedServers)
		r.Get("/servers", s.HandleListDedicatedServers)

		r.Route("/{serviceName}", func(r chi.Router) {
			r.Get("/", s.HandleGetDedicatedServerDetail)
			r.Post("/reboot", s.HandleRebootServer)
			r.Get("/templates", s.HandleGetOSTemplates)
			r.Post("/install", s.HandleInstallOS)
			r.Get("/install/status", s.HandleGetInstallStatus)
			r.Get("/partition-schemes", s.HandleGetPartitionSchemes)
			r.Get("/hardware-disk-info", s.HandleGetHardwareDiskInfo)
			r.Get("/hardware-raid-profiles", s.HandleGetHardwareRAIDProfiles)

			r.Get("/tasks", s.HandleGetServerTasks)
			r.Get("/tasks/{taskId}/available-timeslots", s.HandleGetTaskTimeslots)
			r.Post("/tasks/{taskId}/schedule", s.HandleScheduleTask)

			r.Get("/boot", s.HandleGetBootConfig)
			r.Put("/boot/{bootId}", s.HandleSetBootConfig)
			r.Get("/boot-mode", s.HandleGetBootModes)
			r.Put("/boot-mode", s.HandleSetBootMode)

			r.Get("/monitoring", s.HandleGetMonitoring)
			r.Put("/monitoring", s.HandleSetMonitoring)

			r.Get("/hardware", s.HandleGetHardware)
			r.Post("/hardware/replace", s.HandleHardwareReplace)
			r.Get("/serviceinfo", s.HandleGetServiceInfo)
			r.Get("/network-specs", s.HandleGetNetworkSpecs)
			r.Get("/network-interfaces", s.HandleGetNetworkInterfaces)
			r.Get("/network-stats", s.HandleGetNetworkStats)
			r.Get("/mrtg", s.HandleGetMRTG)
			r.Get("/statistics", s.HandleGetStatistics)

			r.Get("/ips", s.HandleGetServerIPs)
			r.Get("/ip-specs", s.HandleGetIPSpecs)
			r.Get("/reverse", s.HandleGetReverseDNS)
			r.Post("/reverse", s.HandleSetReverseDNS)
			r.Get("/ip/can-be-moved-to", s.HandleIPCanBeMovedTo)
			r.Get("/ip/country-available", s.HandleIPCountryAvailable)
			r.Post("/ip/move", s.HandleMoveIP)

			r.Get("/burst", s.HandleGetBurst)
			r.Put("/burst", s.HandleSetBurst)
			r.Get("/firewall", s.HandleGetFirewall)
			r.Put("/firewall", s.HandleSetFirewall)

			r.Get("/ip-security", s.HandleGetIPSecurity)
			r.Post("/ip-security/rules", s.HandleCreateIPFirewallRule)
			r.Delete("/ip-security/rules/{ruleId}", s.HandleDeleteIPFirewallRule)
			r.Post("/ip-security/mitigation", s.HandleUpdateIPMitigation)

			r.Get("/console", s.HandleGetIPMIConsole)

			r.Get("/backup-ftp", s.HandleGetBackupFTP)
			r.Post("/backup-ftp", s.HandleActivateBackupFTP)
			r.Delete("/backup-ftp", s.HandleDeleteBackupFTP)
			r.Get("/backup-ftp/access", s.HandleGetBackupFTPAccess)
			r.Post("/backup-ftp/access", s.HandleAddBackupFTPAccess)
			r.Delete("/backup-ftp/access/*", s.HandleDeleteBackupFTPAccess)
			r.Post("/backup-ftp/password", s.HandleResetBackupFTPPassword)
			r.Get("/backup-ftp/authorizable-blocks", s.HandleGetBackupFTPAuthorizableBlocks)

			r.Get("/backup-cloud", s.HandleGetBackupCloud)
			r.Get("/backup-cloud/offer-details", s.HandleGetBackupCloudOfferDetails)

			r.Get("/secondary-dns", s.HandleGetSecondaryDNS)
			r.Post("/secondary-dns", s.HandleAddSecondaryDNS)
			r.Delete("/secondary-dns/*", s.HandleDeleteSecondaryDNS)

			r.Get("/virtual-mac", s.HandleGetVirtualMACs)
			r.Post("/virtual-mac", s.HandleCreateVirtualMAC)

			r.Get("/virtual-network-interface", s.HandleGetVirtualNetworkInterfaces)
			r.Post("/virtual-network-interface/{uuid}/enable", s.HandleEnableVirtualNetworkInterface)
			r.Post("/virtual-network-interface/{uuid}/disable", s.HandleDisableVirtualNetworkInterface)

			r.Post("/ola/aggregation", s.HandleOLAAggregation)
			r.Post("/ola/reset", s.HandleOLAReset)
			r.Post("/ola/group", s.HandleOLAGroup)
			r.Post("/ola/ungroup", s.HandleOLAUngroup)

			r.Get("/vrack", s.HandleGetVRack)
			r.Delete("/vrack/*", s.HandleRemoveFromVRack)

			r.Get("/orderable/bandwidth", s.HandleGetOrderableBandwidth)
			r.Get("/orderable/traffic", s.HandleGetOrderableTraffic)
			r.Get("/orderable/ip", s.HandleGetOrderableIP)
			r.Get("/options", s.HandleGetServerOptions)

			r.Get("/interventions", s.HandleGetInterventions)
			r.Get("/interventions/{interventionId}", s.HandleGetInterventionDetail)
			r.Get("/planned-interventions", s.HandleGetPlannedInterventions)
			r.Get("/planned-interventions/{interventionId}", s.HandleGetPlannedInterventionDetail)

			r.Get("/ongoing", s.HandleGetOngoingTasks)
			r.Get("/license/windows/compliant", s.HandleGetCompliantWindows)
			r.Get("/license/windows-sql/compliant", s.HandleGetCompliantWindowsSQL)

			r.Post("/change-contact", s.HandleChangeContact)
			r.Post("/terminate", s.HandleTerminateService)
			r.Post("/confirm-termination", s.HandleConfirmTermination)

			r.Get("/spla", s.HandleGetSPLAList)
			r.Post("/spla", s.HandleCreateSPLA)

			r.Get("/bios-settings", s.HandleGetBIOSSettings)
			r.Get("/bios-settings/sgx", s.HandleGetBIOSSettingsSGX)
		})
	})
}

// ==================== Shared Helpers ====================

func (s *Service) clientFromRequest(r *http.Request) (OVHClient, *store.OVHAccount, bool) {
	c, acc, err := s.clientForRequestFn(r)
	if err == nil && c != nil {
		return c, acc, true
	}
	return nil, acc, false
}

func (s *Service) requireOVHClient(w http.ResponseWriter, r *http.Request) (OVHClient, *store.OVHAccount, bool) {
	c, acc, err := s.clientForRequestFn(r)
	if err == nil && c != nil {
		return c, acc, true
	}
	if errors.Is(err, ovhclient.ErrCredentialsMissing) || c == nil {
		writeCredentialsMissing(w)
		return nil, acc, false
	}
	writeJSONError(w, http.StatusInternalServerError, "OVH_CLIENT_ERROR", err.Error())
	return nil, acc, false
}

func writeCredentialsMissing(w http.ResponseWriter) {
	const msg = "OVH API credentials are not configured. Please add an API account first."
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"success": false,
		"status":  "error",
		"code":    "OVH_CREDENTIALS_MISSING",
		"error":   msg,
		"message": msg,
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"status":  "error",
		"code":    code,
		"error":   message,
		"message": message,
	})
}

func writeStatusError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"status":  "error",
		"success": false,
		"error":   message,
		"message": message,
	})
}

func writeSuccessError(w http.ResponseWriter, status int, errMsg string) {
	writeJSON(w, status, map[string]any{
		"success": false,
		"error":   errMsg,
		"message": errMsg,
	})
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r == nil || r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func formatOVHError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if qid := ovhclient.ExtractQueryID(err); qid != "" && !strings.Contains(msg, qid) {
		msg = fmt.Sprintf("%s OVH-Query-ID: %s", msg, qid)
	}
	return msg
}

func isOVHNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *ovh.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		if apiErr.Code == http.StatusNotFound {
			return true
		}
	}
	low := strings.ToLower(err.Error())
	for _, needle := range []string{
		"does not exist",
		"not exist",
		"not found",
		"not supported",
		"object does not exist",
		"no installation",
		"not being installed",
		"not being reinstalled",
		"being installed or reinstalled",
		"no os installation",
		"not installing",
		"installation not found",
		"no task",
		"resource not found",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

func daysUntil(dateStr any) *int {
	raw := strings.TrimSpace(asString(dateStr))
	if raw == "" {
		return nil
	}
	var parsed time.Time
	var err error
	if strings.Contains(raw, "T") {
		parsed, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			parsed, err = time.Parse("2006-01-02T15:04:05-07:00", raw)
		}
		if err != nil && len(raw) >= 19 {
			parsed, err = time.ParseInLocation("2006-01-02T15:04:05", raw[:19], time.UTC)
		}
	} else if len(raw) >= 10 {
		parsed, err = time.ParseInLocation("2006-01-02", raw[:10], time.UTC)
	} else {
		return nil
	}
	if err != nil {
		return nil
	}
	days := int(parsed.UTC().Sub(time.Now().UTC()).Seconds() / 86400)
	return &days
}

func computeRenewMode(renewObj map[string]any) string {
	if len(renewObj) == 0 {
		return "manual"
	}
	if asBool(renewObj["deleteAtExpiration"]) {
		return "deleteAtExpiration"
	}
	if asBool(renewObj["automatic"]) {
		return "auto"
	}
	return "manual"
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		low := strings.ToLower(strings.TrimSpace(t))
		return low == "true" || low == "1" || low == "yes"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i != 0
		}
		if f, err := t.Float64(); err == nil {
			return f != 0
		}
		return false
	default:
		return false
	}
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case nil:
		return def
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		if f, err := t.Float64(); err == nil {
			return int(f)
		}
		return def
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return i
		}
		return def
	default:
		return def
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}

func unescapeWildcardParam(r *http.Request) string {
	raw := chi.URLParam(r, "*")
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "%") {
		if decoded, err := url.PathUnescape(raw); err == nil {
			return decoded
		}
	}
	return raw
}

func isPublicIP(ipStr string) bool {
	trimmed := strings.TrimSpace(ipStr)
	if trimmed == "" {
		return false
	}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() &&
		!addr.IsPrivate() &&
		!addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() &&
		!addr.IsUnspecified()
}

func (s *Service) logInfo(source, msg string, attrs ...any) {
	if s.log != nil {
		s.log.Info(source, msg, attrs...)
	}
}

func (s *Service) logWarn(source, msg string, attrs ...any) {
	if s.log != nil {
		s.log.Warn(source, msg, attrs...)
	}
}

func (s *Service) logError(source, msg string, attrs ...any) {
	if s.log != nil {
		s.log.Error(source, msg, attrs...)
	}
}

func (s *Service) logDebug(source, msg string, attrs ...any) {
	if s.log != nil {
		s.log.Debug(source, msg, attrs...)
	}
}
