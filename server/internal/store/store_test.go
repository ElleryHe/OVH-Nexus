package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ovh-nexus/server/internal/vault"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	v, err := vault.New("store-test-encryption-key-0001")
	if err != nil {
		t.Fatalf("vault.New failed: %v", err)
	}
	st, err := Open(dir, v)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st, dir
}

func TestStoreOpenPermissionsAndWAL(t *testing.T) {
	st, dir := openTestStore(t)

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("expected data dir mode 0700, got %o", perm)
	}

	dbInfo, err := os.Stat(filepath.Join(dir, "nexus.db"))
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected nexus.db mode 0600, got %o", perm)
	}

	var mode string
	if err := st.DB().QueryRow(`PRAGMA journal_mode;`).Scan(&mode); err != nil {
		t.Fatalf("pragma journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}
}

func TestStoreKVAndSessionSecret(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	if _, ok, err := st.GetSetting(ctx, "missing"); err != nil || ok {
		t.Fatalf("expected missing key, ok=%v err=%v", ok, err)
	}
	if err := st.SetSetting(ctx, "panel_url", "https://nexus.example.com"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	val, ok, err := st.GetSetting(ctx, "panel_url")
	if err != nil || !ok || val != "https://nexus.example.com" {
		t.Fatalf("GetSetting mismatch: val=%q ok=%v err=%v", val, ok, err)
	}

	sec1, err := st.GetOrCreateSessionSecret(ctx)
	if err != nil || len(sec1) < 32 {
		t.Fatalf("GetOrCreateSessionSecret 1: sec=%q err=%v", sec1, err)
	}
	sec2, err := st.GetOrCreateSessionSecret(ctx)
	if err != nil || sec1 != sec2 {
		t.Fatalf("GetOrCreateSessionSecret not idempotent: %q != %q", sec1, sec2)
	}

	raw, _, _ := st.GetSetting(ctx, "session_secret")
	if !vault.IsEncrypted(raw) {
		t.Fatalf("expected session_secret in kv_settings to be encrypted, got %q", raw)
	}
}

func TestStoreAccountsEncryptionAndDefaultResolution(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	acc1, err := st.UpsertAccount(ctx, OVHAccount{
		ID:          "acct-eu",
		Alias:       "Primary EU",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "plain-app-key-1",
		AppSecret:   "plain-app-secret-1",
		ConsumerKey: "plain-consumer-key-1",
	})
	if err != nil {
		t.Fatalf("UpsertAccount 1: %v", err)
	}
	if !acc1.IsDefault {
		t.Fatal("expected first account to automatically become default")
	}

	// Verify SQLite columns store enc:v1 ciphertext, never plaintext.
	var rawAK, rawAS, rawCK string
	if err := st.DB().QueryRow(`SELECT app_key, app_secret, consumer_key FROM ovh_accounts WHERE id = ?`, "acct-eu").
		Scan(&rawAK, &rawAS, &rawCK); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	for _, raw := range []string{rawAK, rawAS, rawCK} {
		if !vault.IsEncrypted(raw) {
			t.Fatalf("expected encrypted column in SQLite, got %q", raw)
		}
	}

	_, err = st.UpsertAccount(ctx, OVHAccount{
		ID:          "acct-us",
		Alias:       "US Ops",
		Endpoint:    "ovh-us",
		Zone:        "US",
		AppKey:      "plain-app-key-2",
		AppSecret:   "plain-app-secret-2",
		ConsumerKey: "plain-consumer-key-2",
	})
	if err != nil {
		t.Fatalf("UpsertAccount 2: %v", err)
	}

	activeDefault, err := st.GetActiveAccount(ctx, "")
	if err != nil || activeDefault.ID != "acct-eu" || activeDefault.AppKey != "plain-app-key-1" {
		t.Fatalf("expected default acct-eu decrypted, got %+v err=%v", activeDefault, err)
	}
	activeHeader, err := st.GetActiveAccount(ctx, "acct-us")
	if err != nil || activeHeader.ID != "acct-us" || activeHeader.ConsumerKey != "plain-consumer-key-2" {
		t.Fatalf("expected header acct-us decrypted, got %+v err=%v", activeHeader, err)
	}

	if err := st.SetDefaultAccount(ctx, "acct-us"); err != nil {
		t.Fatalf("SetDefaultAccount: %v", err)
	}
	activeDefault, err = st.GetActiveAccount(ctx, "")
	if err != nil || activeDefault.ID != "acct-us" {
		t.Fatalf("expected new default acct-us, got %+v err=%v", activeDefault, err)
	}

	if err := st.DeleteAccount(ctx, "acct-us"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	remaining, err := st.ListAccounts(ctx)
	if err != nil || len(remaining) != 1 || !remaining[0].IsDefault {
		t.Fatalf("expected acct-eu promoted to default, got %+v err=%v", remaining, err)
	}
}

func TestStoreTOTPQueueHistoryStats(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	// TOTP state encryption at rest
	if err := st.SaveTOTPState(ctx, &TOTPState{
		Enabled:            true,
		Secret:             "JBSWY3DPEHPK3PXP",
		PendingSecret:      "PENDINGSECRET1234",
		RecoveryCodeHashes: []string{"hash1", "hash2"},
		LastUsedStep:       555,
		EnabledAt:          "2026-11-16T00:00:00Z",
	}); err != nil {
		t.Fatalf("SaveTOTPState: %v", err)
	}
	var rawSec, rawPend string
	if err := st.DB().QueryRow(`SELECT secret, pending_secret FROM totp_state WHERE id = 1`).Scan(&rawSec, &rawPend); err != nil {
		t.Fatalf("query raw totp: %v", err)
	}
	if !vault.IsEncrypted(rawSec) || !vault.IsEncrypted(rawPend) {
		t.Fatalf("expected encrypted totp secrets, got %q / %q", rawSec, rawPend)
	}
	loadedTOTP, err := st.LoadTOTPState(ctx)
	if err != nil || !loadedTOTP.Enabled || loadedTOTP.Secret != "JBSWY3DPEHPK3PXP" || loadedTOTP.LastUsedStep != 555 {
		t.Fatalf("LoadTOTPState mismatch: %+v err=%v", loadedTOTP, err)
	}

	// Queue defaults: auto_pay=false, waive_retractation_period=false
	qItem, err := st.CreateQueueItem(ctx, QueueItem{
		AccountID:   "acct-eu",
		PlanCode:    "24ska01",
		Datacenters: []string{"gra", "rbx"},
		Options:     []string{"ram-64g"},
	})
	if err != nil {
		t.Fatalf("CreateQueueItem: %v", err)
	}
	if qItem.AutoPay || qItem.WaiveRetractationPeriod {
		t.Fatal("expected AutoPay and WaiveRetractationPeriod to default to false")
	}
	if qItem.Source != "manual" || qItem.SourceRef != "" {
		t.Fatalf("expected default queue source='manual' and empty sourceRef, got source=%q sourceRef=%q", qItem.Source, qItem.SourceRef)
	}
	qItem.Status = "paused"
	qItem.Source = "stock-monitor"
	qItem.SourceRef = "24ska01"
	qItem.PricingMode = "upfront12"
	qItem.Duration = "P12M"
	qItem.Coupon = "BF26"
	if _, err := st.UpdateQueueItem(ctx, *qItem); err != nil {
		t.Fatalf("UpdateQueueItem: %v", err)
	}
	gotQ, err := st.GetQueueItem(ctx, qItem.ID)
	if err != nil || gotQ.Status != "paused" || gotQ.Source != "stock-monitor" || gotQ.SourceRef != "24ska01" ||
		gotQ.PricingMode != "upfront12" || gotQ.Duration != "P12M" || gotQ.Coupon != "BF26" {
		t.Fatalf("GetQueueItem: %+v err=%v", gotQ, err)
	}

	// History & Stats
	h1, err := st.AddHistory(ctx, PurchaseHistory{
		AccountID:    "acct-eu",
		PlanCode:     "24ska01",
		Datacenter:   "gra",
		PricingMode:  "upfront12",
		Duration:     "P12M",
		Coupon:       "BF26",
		PeriodPrice:  346.47,
		MonthlyPrice: 28.87,
		Status:       "success",
		OrderID:      "123456",
		PriceText:    "346.47 EUR",
	})
	if err != nil {
		t.Fatalf("AddHistory success: %v", err)
	}
	listedHist, err := st.ListHistory(ctx, "acct-eu")
	if err != nil || len(listedHist) != 1 || listedHist[0].PricingMode != "upfront12" || listedHist[0].Duration != "P12M" || listedHist[0].Coupon != "BF26" || listedHist[0].PeriodPrice != 346.47 || listedHist[0].MonthlyPrice != 28.87 {
		t.Fatalf("ListHistory promo fields mismatch: %+v err=%v", listedHist, err)
	}
	if _, err := st.AddHistory(ctx, PurchaseHistory{
		AccountID:    "acct-eu",
		PlanCode:     "24ska01",
		Datacenter:   "rbx",
		Status:       "failed",
		ErrorMessage: "out of stock",
	}); err != nil {
		t.Fatalf("AddHistory failed: %v", err)
	}
	if err := st.MarkOrderDeliveredNotified(ctx, h1.ID); err != nil {
		t.Fatalf("MarkOrderDeliveredNotified: %v", err)
	}

	stats, err := st.ComputeStats(ctx, "acct-eu")
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.ActiveQueues != 1 || stats.PurchaseSuccess != 1 || stats.PurchaseFailed != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	if _, err := st.ClearQueue(ctx, "acct-eu"); err != nil {
		t.Fatalf("ClearQueue: %v", err)
	}
	if _, err := st.ClearHistory(ctx, "acct-eu"); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}
}

func TestStoreSubscriptionsSKUWatcherNotificationsMCP(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	// Stock subscriptions
	inserted, updated, err := st.BatchUpsertStockSubs(ctx, []StockSubscription{
		{AccountID: "a1", PlanCode: "24ska01", NotifyAvailable: true, Datacenters: []string{"gra"}},
		{AccountID: "a1", PlanCode: "24ska02", NotifyAvailable: true},
	})
	if err != nil || inserted != 2 || updated != 0 {
		t.Fatalf("BatchUpsertStockSubs: ins=%d upd=%d err=%v", inserted, updated, err)
	}
	subs, err := st.ListStockSubs(ctx, "a1")
	if err != nil || len(subs) != 2 {
		t.Fatalf("ListStockSubs: %+v err=%v", subs, err)
	}
	if err := st.DeleteStockSub(ctx, "a1", "24ska02"); err != nil {
		t.Fatalf("DeleteStockSub: %v", err)
	}
	if _, err := st.ClearStockSubs(ctx, "a1"); err != nil {
		t.Fatalf("ClearStockSubs: %v", err)
	}

	// VPS subscriptions & history pruning (max 100)
	vpsSub, err := st.UpsertVPSSub(ctx, VPSSubscription{
		PlanCode:        "vps-2025-model1",
		OVHSubsidiary:   "IE",
		MonitorLinux:    true,
		NotifyAvailable: true,
	})
	if err != nil {
		t.Fatalf("UpsertVPSSub: %v", err)
	}
	for i := range 105 {
		if err := st.AppendVPSHistory(ctx, vpsSub.ID, VPSHistoryEntry{
			Datacenter:     "Gravelines",
			DatacenterCode: "GRA",
			Status:         "available",
			ChangeType:     "available",
			OldStatus:      fmt.Sprintf("step-%d", i),
		}); err != nil {
			t.Fatalf("AppendVPSHistory %d: %v", i, err)
		}
	}
	vpsHist, err := st.GetVPSHistory(ctx, vpsSub.ID)
	if err != nil || len(vpsHist) != 100 || vpsHist[0].OldStatus != "step-104" {
		t.Fatalf("expected 100 pruned vps history newest first, got len=%d first=%+v err=%v", len(vpsHist), vpsHist[0], err)
	}
	if err := st.DeleteVPSSub(ctx, vpsSub.ID); err != nil {
		t.Fatalf("DeleteVPSSub: %v", err)
	}

	// SKU watcher state
	if err := st.SaveSKUWatcherState(ctx, &SKUWatcherState{
		Initialized: true,
		Endpoint:    "ovh-eu",
		SKUsJSON:    json.RawMessage(`{"24ska01":{"name":"KS-A"}}`),
		MissingJSON: json.RawMessage(`{}`),
		ChangesJSON: json.RawMessage(`[{"type":"added"}]`),
	}); err != nil {
		t.Fatalf("SaveSKUWatcherState: %v", err)
	}
	skuSt, err := st.LoadSKUWatcherState(ctx)
	if err != nil || !skuSt.Initialized || !strings.Contains(string(skuSt.SKUsJSON), "24ska01") {
		t.Fatalf("LoadSKUWatcherState: %+v err=%v", skuSt, err)
	}

	// Notification channels secret encryption + secret preservation + settings + markers
	if _, err := st.UpsertChannel(ctx, NotificationChannel{
		ID:      "tg-1",
		Type:    "telegram",
		Name:    "Ops TG",
		Enabled: true,
		Events:  []string{"stock.available"},
		Config:  map[string]any{"token": "123456:tg-secret-token", "chatId": "-100123"},
	}); err != nil {
		t.Fatalf("UpsertChannel tg: %v", err)
	}
	if _, err := st.UpsertChannel(ctx, NotificationChannel{
		ID:      "smtp-1",
		Type:    "smtp",
		Name:    "Ops SMTP",
		Enabled: true,
		Events:  []string{"order.success"},
		Config:  map[string]any{"host": "smtp.example.com", "password": "smtp-super-secret-password"},
	}); err != nil {
		t.Fatalf("UpsertChannel smtp: %v", err)
	}
	var rawTgCfg string
	if err := st.DB().QueryRow(`SELECT config_json FROM notification_channels WHERE id = 'tg-1'`).Scan(&rawTgCfg); err != nil {
		t.Fatalf("raw channel query: %v", err)
	}
	if strings.Contains(rawTgCfg, "tg-secret-token") || !strings.Contains(rawTgCfg, vault.EncPrefix) {
		t.Fatalf("expected encrypted telegram token in SQLite, got %s", rawTgCfg)
	}
	// Replacing channels with masked placeholder must preserve secret
	replaced, err := st.ReplaceChannels(ctx, []NotificationChannel{
		{
			ID:      "tg-1",
			Type:    "telegram",
			Name:    "Ops TG Renamed",
			Enabled: true,
			Events:  []string{"stock.available"},
			Config:  map[string]any{"token": "1234****oken", "chatId": "-100123"},
		},
	})
	if err != nil || len(replaced) != 1 || replaced[0].Config["token"] != "123456:tg-secret-token" {
		t.Fatalf("expected preserved telegram token after ReplaceChannels, got %+v err=%v", replaced, err)
	}
	redacted := replaced[0].Redacted()
	if redacted.Config["token"] == "123456:tg-secret-token" || !strings.Contains(redacted.Config["token"].(string), "****") {
		t.Fatalf("expected masked token on Redacted(), got %v", redacted.Config["token"])
	}

	notifSettings, err := st.GetNotificationSettings(ctx)
	if err != nil || notifSettings.Locale != "en" {
		t.Fatalf("expected default notification locale en, got %+v err=%v", notifSettings, err)
	}
	savedSettings, err := st.SaveNotificationSettings(ctx, NotificationSettings{
		PanelURL: "https://panel.local",
		Locale:   "fr",
	})
	if err != nil || savedSettings.Locale != "fr" {
		t.Fatalf("SaveNotificationSettings: %+v err=%v", savedSettings, err)
	}

	hasM, _ := st.HasNotifiedMarker(ctx, "srv1:2026-12-01")
	if hasM {
		t.Fatal("expected marker absent initially")
	}
	if err := st.SetNotifiedMarker(ctx, "srv1:2026-12-01", "expiry"); err != nil {
		t.Fatalf("SetNotifiedMarker: %v", err)
	}
	hasM, _ = st.HasNotifiedMarker(ctx, "srv1:2026-12-01")
	if !hasM {
		t.Fatal("expected marker present after SetNotifiedMarker")
	}

	// MCP config, aliases, audit
	if err := st.SaveMCPConfig(ctx, &MCPConfig{
		Enabled:          true,
		TokenHash:        vault.HashTokenSHA256("nxs_mcp_test_1234567890"),
		MaskedToken:      "nxs_mcp_te****7890",
		Scopes:           MCPScopes{Read: true, Procurement: true},
		AllowAllAccounts: true,
		HourlyOrderLimit: 5,
	}); err != nil {
		t.Fatalf("SaveMCPConfig: %v", err)
	}
	mcpCfg, err := st.LoadMCPConfig(ctx)
	if err != nil || !mcpCfg.Enabled || !mcpCfg.Scopes.Procurement {
		t.Fatalf("LoadMCPConfig: %+v err=%v", mcpCfg, err)
	}

	aliases := emptyMCPAliasState()
	aliases.Forward["baremetal"]["ns12345.ip-1-2-3.eu"] = "bm-01"
	aliases.Reverse["baremetal"]["bm-01"] = "ns12345.ip-1-2-3.eu"
	aliases.Meta["baremetal"]["bm-01"] = map[string]any{"label": "ProdNode"}
	aliases.Counters["baremetal"] = 1
	if err := st.SaveMCPAliases(ctx, aliases); err != nil {
		t.Fatalf("SaveMCPAliases: %v", err)
	}
	loadedAliases, err := st.LoadMCPAliases(ctx)
	if err != nil || loadedAliases.Reverse["baremetal"]["bm-01"] != "ns12345.ip-1-2-3.eu" || loadedAliases.Counters["baremetal"] != 1 {
		t.Fatalf("LoadMCPAliases: %+v err=%v", loadedAliases, err)
	}

	if _, err := st.AppendMCPAudit(ctx, MCPAuditLog{
		Tool:   "baremetal_list",
		Scope:  "read",
		Status: "success",
	}); err != nil {
		t.Fatalf("AppendMCPAudit: %v", err)
	}
	audits, err := st.ListMCPAudit(ctx, 10)
	if err != nil || len(audits) != 1 || audits[0].Tool != "baremetal_list" {
		t.Fatalf("ListMCPAudit: %+v err=%v", audits, err)
	}
	if err := st.ClearMCPAudit(ctx); err != nil {
		t.Fatalf("ClearMCPAudit: %v", err)
	}
	if err := st.DeleteChannel(ctx, "missing-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting missing channel, got %v", err)
	}
}

func TestStoreSystemLogsPruningAndQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()

	// Insert 10,035 rows and verify ring-buffer prunes to <= 10,000 keeping newest entries.
	tx, err := st.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	for i := 1; i <= 10020; i++ {
		if _, err := tx.Exec(`INSERT INTO system_logs (timestamp, level, source, message, attrs_json) VALUES (?, 'INFO', 'server', ?, '{}')`,
			"2026-11-16T00:00:00Z", fmt.Sprintf("seed-msg-%d", i)); err != nil {
			t.Fatalf("seed insert %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}
	st.mu.Lock()
	st.logCount = 10020
	st.mu.Unlock()

	for i := 10021; i <= 10035; i++ {
		if _, err := st.InsertLog(ctx, "2026-11-16T00:00:01Z", "WARNING", "auth", fmt.Sprintf("live-msg-%d", i), `{"ip":"1.2.3.4"}`); err != nil {
			t.Fatalf("InsertLog %d: %v", i, err)
		}
	}

	var total int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM system_logs`).Scan(&total); err != nil {
		t.Fatalf("count system_logs: %v", err)
	}
	if total > 10000 {
		t.Fatalf("expected system_logs pruned to <= 10000 rows, got %d", total)
	}

	rows, err := st.QueryLogs(ctx, "WARNING", "auth", "live-msg-1003", 50)
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 matching live-msg-1003* rows, got %d", len(rows))
	}
	if rows[0].Message != "live-msg-10030" || rows[len(rows)-1].Message != "live-msg-10035" {
		t.Fatalf("expected chronological oldest->newest order, got first=%q last=%q", rows[0].Message, rows[len(rows)-1].Message)
	}

	if err := st.ClearLogs(ctx); err != nil {
		t.Fatalf("ClearLogs: %v", err)
	}
	afterClear, err := st.QueryLogs(ctx, "", "", "", 10)
	if err != nil || len(afterClear) != 0 {
		t.Fatalf("expected 0 logs after clear, got %d err=%v", len(afterClear), err)
	}
}
