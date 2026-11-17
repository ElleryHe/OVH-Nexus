package config

import (
	"net/netip"
	"strings"
	"testing"
)

func baseEnv() map[string]string {
	return map[string]string{
		"API_SECRET_KEY":      "strong-api-secret-key-1234",
		"DATA_ENCRYPTION_KEY": "strong-data-encryption-key-5678",
	}
}

func mapLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadFromEnvDefaults(t *testing.T) {
	cfg, err := LoadFromEnv(mapLookup(baseEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("expected default PORT 8080, got %q", cfg.Port)
	}
	if cfg.DataDir != "/data" {
		t.Fatalf("expected default DATA_DIR /data, got %q", cfg.DataDir)
	}
	if cfg.TZ != "UTC" || cfg.Location.String() != "UTC" {
		t.Fatalf("expected default TZ UTC, got %q", cfg.TZ)
	}
	if cfg.CookieSecure {
		t.Fatal("expected COOKIE_SECURE false by default")
	}
	if cfg.DefaultLocale != "en" {
		t.Fatalf("expected DEFAULT_LOCALE en, got %q", cfg.DefaultLocale)
	}
	if cfg.LogLevel != "INFO" {
		t.Fatalf("expected LOG_LEVEL INFO, got %q", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Fatalf("expected LOG_FORMAT text, got %q", cfg.LogFormat)
	}
	if cfg.AdminUsername != "Nexus" {
		t.Fatalf("expected default AdminUsername Nexus, got %q", cfg.AdminUsername)
	}
	if !cfg.VerifyAdminUsername("Nexus") {
		t.Fatal("expected VerifyAdminUsername(\"Nexus\") to succeed")
	}
	if cfg.VerifyAdminUsername("nexus") {
		t.Fatal("expected VerifyAdminUsername(\"nexus\") to fail (case-sensitive)")
	}
	if cfg.VerifyAdminUsername("") {
		t.Fatal("expected VerifyAdminUsername(\"\") to fail")
	}
	if !cfg.VerifyAPISecretKey("strong-api-secret-key-1234") {
		t.Fatal("expected VerifyAPISecretKey to succeed")
	}
	if cfg.VerifyAPISecretKey("wrong-secret-key-1234") {
		t.Fatal("expected VerifyAPISecretKey to reject wrong secret")
	}
	if !cfg.VerifyInternalToken(cfg.InternalToken) {
		t.Fatal("expected VerifyInternalToken to succeed")
	}
	cfg.WipeEncryptionKey()
	if cfg.DataEncryptionKey != "" {
		t.Fatal("expected WipeEncryptionKey to clear DataEncryptionKey")
	}
}

func TestLoadFromEnvValidationErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]string)
		wantErr string
	}{
		{
			name:    "missing API_SECRET_KEY",
			mutate:  func(m map[string]string) { delete(m, "API_SECRET_KEY") },
			wantErr: "API_SECRET_KEY",
		},
		{
			name:    "short API_SECRET_KEY",
			mutate:  func(m map[string]string) { m["API_SECRET_KEY"] = "short123" },
			wantErr: "too short",
		},
		{
			name:    "weak API_SECRET_KEY",
			mutate:  func(m map[string]string) { m["API_SECRET_KEY"] = "ovh-nexus-default-secret-key" },
			wantErr: "weak",
		},
		{
			name:    "missing DATA_ENCRYPTION_KEY",
			mutate:  func(m map[string]string) { delete(m, "DATA_ENCRYPTION_KEY") },
			wantErr: "DATA_ENCRYPTION_KEY",
		},
		{
			name:    "short DATA_ENCRYPTION_KEY",
			mutate:  func(m map[string]string) { m["DATA_ENCRYPTION_KEY"] = "123456789012345" },
			wantErr: "too short",
		},
		{
			name:    "weak DATA_ENCRYPTION_KEY",
			mutate:  func(m map[string]string) { m["DATA_ENCRYPTION_KEY"] = "ovh-nexus-default-encryption-key" },
			wantErr: "weak",
		},
		{
			name: "equal API_SECRET_KEY and DATA_ENCRYPTION_KEY",
			mutate: func(m map[string]string) {
				m["API_SECRET_KEY"] = "identical-secret-key-99999"
				m["DATA_ENCRYPTION_KEY"] = "identical-secret-key-99999"
			},
			wantErr: "must differ",
		},
		{
			name:    "invalid PORT",
			mutate:  func(m map[string]string) { m["PORT"] = "99999" },
			wantErr: "PORT",
		},
		{
			name:    "invalid TZ",
			mutate:  func(m map[string]string) { m["TZ"] = "Invalid/Timezone_XYZ" },
			wantErr: "TZ",
		},
		{
			name:    "invalid COOKIE_SECURE",
			mutate:  func(m map[string]string) { m["COOKIE_SECURE"] = "maybe" },
			wantErr: "COOKIE_SECURE",
		},
		{
			name:    "invalid TRUSTED_PROXIES",
			mutate:  func(m map[string]string) { m["TRUSTED_PROXIES"] = "not-an-ip" },
			wantErr: "TRUSTED_PROXIES",
		},
		{
			name:    "invalid LOG_LEVEL",
			mutate:  func(m map[string]string) { m["LOG_LEVEL"] = "TRACE" },
			wantErr: "LOG_LEVEL",
		},
		{
			name:    "invalid LOG_FORMAT",
			mutate:  func(m map[string]string) { m["LOG_FORMAT"] = "xml" },
			wantErr: "LOG_FORMAT",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := baseEnv()
			tc.mutate(env)
			_, err := LoadFromEnv(mapLookup(env))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestTrustedProxiesAndCustomOptions(t *testing.T) {
	env := baseEnv()
	env["PORT"] = "9090"
	env["DATA_DIR"] = "/tmp/ovh-test"
	env["TZ"] = "Europe/Paris"
	env["COOKIE_SECURE"] = "true"
	env["DEFAULT_LOCALE"] = "zh-cn"
	env["TRUSTED_PROXIES"] = "127.0.0.1, 10.0.0.0/8, ::1"
	env["LOG_LEVEL"] = "warn"
	env["LOG_FORMAT"] = "JSON"

	cfg, err := LoadFromEnv(mapLookup(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" || cfg.DataDir != "/tmp/ovh-test" || !cfg.CookieSecure {
		t.Fatalf("unexpected config values: %+v", cfg)
	}
	if cfg.DefaultLocale != "zh-CN" {
		t.Fatalf("expected normalized locale zh-CN, got %q", cfg.DefaultLocale)
	}
	if cfg.LogLevel != "WARNING" || cfg.LogFormat != "json" {
		t.Fatalf("unexpected log config: level=%q format=%q", cfg.LogLevel, cfg.LogFormat)
	}

	if !cfg.IsTrustedProxy(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("expected 127.0.0.1 to be trusted")
	}
	if !cfg.IsTrustedProxy(netip.MustParseAddr("10.20.30.40")) {
		t.Fatal("expected 10.20.30.40 to be trusted")
	}
	if !cfg.IsTrustedProxy(netip.MustParseAddr("::ffff:10.1.2.3")) {
		t.Fatal("expected IPv4-mapped IPv6 to be trusted")
	}
	if !cfg.IsTrustedProxy(netip.MustParseAddr("::1")) {
		t.Fatal("expected ::1 to be trusted")
	}
	if cfg.IsTrustedProxy(netip.MustParseAddr("192.168.1.50")) {
		t.Fatal("did not expect 192.168.1.50 to be trusted")
	}

	// Custom ADMIN_USERNAME takes precedence over NEXUS_USERNAME
	envCustom := baseEnv()
	envCustom["ADMIN_USERNAME"] = "OpsLead"
	envCustom["NEXUS_USERNAME"] = "FallbackUser"
	cfgCustom, err := LoadFromEnv(mapLookup(envCustom))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgCustom.AdminUsername != "OpsLead" || !cfgCustom.VerifyAdminUsername("OpsLead") || cfgCustom.VerifyAdminUsername("opslead") || cfgCustom.VerifyAdminUsername("Nexus") {
		t.Fatalf("unexpected custom ADMIN_USERNAME behavior: %q", cfgCustom.AdminUsername)
	}

	// NEXUS_USERNAME fallback when ADMIN_USERNAME is unset
	envFallback := baseEnv()
	envFallback["NEXUS_USERNAME"] = "FallbackUser"
	cfgFallback, err := LoadFromEnv(mapLookup(envFallback))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfgFallback.AdminUsername != "FallbackUser" || !cfgFallback.VerifyAdminUsername("FallbackUser") || cfgFallback.VerifyAdminUsername("Nexus") {
		t.Fatalf("unexpected NEXUS_USERNAME fallback behavior: %q", cfgFallback.AdminUsername)
	}
}
