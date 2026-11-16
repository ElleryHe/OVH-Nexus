package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// WeakSecretKeys contains default or placeholder keys that must be rejected at startup.
var weakAPISecretKeys = map[string]struct{}{
	"":                                    {},
	"123456":                              {},
	"123456789012":                        {},
	"ovh-nexus-default-secret-key":        {},
	"replace-with-a-strong-random-secret": {},
	"votre-cle-secrete-robuste":           {},
	"请替换为您自己的高强度密码":                       {},
	"你得替换为你自己的密钥":                         {},
	"password":                            {},
	"changeme":                            {},
	"secret":                              {},
	"admin123456":                         {},
}

var weakEncryptionKeys = map[string]struct{}{
	"":                                       {},
	"123456":                                 {},
	"1234567890123456":                       {},
	"ovh-nexus-default-secret-key":           {},
	"ovh-nexus-default-encryption-key":       {},
	"replace-with-a-strong-random-secret":    {},
	"replace-with-a-strong-encryption-key":   {},
	"votre-cle-secrete-robuste":              {},
	"请替换为您自己的高强度密码":                          {},
	"请替换为您自己的高强度加密密钥":                        {},
	"password":                               {},
	"changeme":                               {},
	"secret":                                 {},
	"encryption-key-example":                 {},
}

// Config holds validated runtime configuration loaded from environment variables.
type Config struct {
	APISecretKey          string
	DataEncryptionKey     string
	Port                  string
	DataDir               string
	TZ                    string
	Location              *time.Location
	CookieSecure          bool
	DefaultLocale         string
	TrustedProxies        []netip.Prefix
	TrustedProxiesRaw     string
	LogLevel              string
	LogFormat             string
	CORSOrigins           []string
	SessionSecretOverride string
	InternalToken         string

	apiSecretDigest     [32]byte
	internalTokenDigest [32]byte
}

// Load reads configuration from the process environment, validates all invariants,
// and sets time.Local to the configured TZ location.
func Load() (*Config, error) {
	cfg, err := LoadFromEnv(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	if cfg.Location != nil {
		time.Local = cfg.Location
	}
	return cfg, nil
}

// LoadFromEnv loads and validates configuration using a custom environment lookup function.
func LoadFromEnv(lookup func(string) (string, bool)) (*Config, error) {
	get := func(key string) string {
		if lookup == nil {
			return ""
		}
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	apiSecret := get("API_SECRET_KEY")
	if apiSecret == "" {
		return nil, errors.New("API_SECRET_KEY is required (minimum 12 characters)")
	}
	if len(apiSecret) < 12 {
		return nil, fmt.Errorf("API_SECRET_KEY is too short (%d chars); minimum is 12 characters", len(apiSecret))
	}
	if _, weak := weakAPISecretKeys[apiSecret]; weak {
		return nil, errors.New("API_SECRET_KEY uses a weak or placeholder value; configure a strong random secret")
	}
	if _, weakLower := weakAPISecretKeys[strings.ToLower(apiSecret)]; weakLower {
		return nil, errors.New("API_SECRET_KEY uses a weak or placeholder value; configure a strong random secret")
	}

	encKey := get("DATA_ENCRYPTION_KEY")
	if encKey == "" {
		return nil, errors.New("DATA_ENCRYPTION_KEY is required (minimum 16 characters, distinct from API_SECRET_KEY)")
	}
	if len(encKey) < 16 {
		return nil, fmt.Errorf("DATA_ENCRYPTION_KEY is too short (%d chars); minimum is 16 characters", len(encKey))
	}
	if _, weak := weakEncryptionKeys[encKey]; weak {
		return nil, errors.New("DATA_ENCRYPTION_KEY uses a weak or placeholder value; configure a strong random encryption key")
	}
	if _, weakLower := weakEncryptionKeys[strings.ToLower(encKey)]; weakLower {
		return nil, errors.New("DATA_ENCRYPTION_KEY uses a weak or placeholder value; configure a strong random encryption key")
	}

	apiDigest := sha256.Sum256([]byte(apiSecret))
	encDigest := sha256.Sum256([]byte(encKey))
	if subtle.ConstantTimeCompare(apiDigest[:], encDigest[:]) == 1 {
		return nil, errors.New("DATA_ENCRYPTION_KEY must differ from API_SECRET_KEY")
	}

	port := get("PORT")
	if port == "" {
		port = "8080"
	}
	port = strings.TrimPrefix(port, ":")
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid PORT %q: must be an integer between 1 and 65535", port)
	}

	dataDir := get("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}

	tz := get("TZ")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid TZ %q: %w", tz, err)
	}

	cookieSecure := false
	if rawCookie := get("COOKIE_SECURE"); rawCookie != "" {
		parsed, err := parseBoolFlexible(rawCookie)
		if err != nil {
			return nil, fmt.Errorf("invalid COOKIE_SECURE %q: %w", rawCookie, err)
		}
		cookieSecure = parsed
	}

	defaultLocale := normalizeLocale(get("DEFAULT_LOCALE"))

	trustedRaw := get("TRUSTED_PROXIES")
	trustedPrefixes, err := parseTrustedProxies(trustedRaw)
	if err != nil {
		return nil, err
	}

	logLevel := strings.ToUpper(get("LOG_LEVEL"))
	if logLevel == "" {
		logLevel = "INFO"
	}
	switch logLevel {
	case "DEBUG", "INFO", "WARNING", "ERROR":
	case "WARN":
		logLevel = "WARNING"
	default:
		return nil, fmt.Errorf("invalid LOG_LEVEL %q: expected DEBUG, INFO, WARNING, or ERROR", logLevel)
	}

	logFormat := strings.ToLower(get("LOG_FORMAT"))
	if logFormat == "" {
		logFormat = "text"
	}
	switch logFormat {
	case "text", "json":
	default:
		return nil, fmt.Errorf("invalid LOG_FORMAT %q: expected text or json", logFormat)
	}

	corsRaw := get("CORS_ORIGINS")
	if corsRaw == "" {
		corsRaw = "http://localhost:19999,http://127.0.0.1:19999"
	}
	var corsOrigins []string
	for _, part := range strings.Split(corsRaw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			corsOrigins = append(corsOrigins, trimmed)
		}
	}

	internalToken, err := generateRandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("failed to generate internal process token: %w", err)
	}

	return &Config{
		APISecretKey:          apiSecret,
		DataEncryptionKey:     encKey,
		Port:                  strconv.Itoa(portNum),
		DataDir:               dataDir,
		TZ:                    tz,
		Location:              loc,
		CookieSecure:          cookieSecure,
		DefaultLocale:         defaultLocale,
		TrustedProxies:        trustedPrefixes,
		TrustedProxiesRaw:     trustedRaw,
		LogLevel:              logLevel,
		LogFormat:             logFormat,
		CORSOrigins:           corsOrigins,
		SessionSecretOverride: get("SECRET_KEY"),
		InternalToken:         internalToken,
		apiSecretDigest:       apiDigest,
		internalTokenDigest:   sha256.Sum256([]byte(internalToken)),
	}, nil
}

// WipeEncryptionKey clears the raw DATA_ENCRYPTION_KEY string from Config after
// the vault has derived its encryption key.
func (c *Config) WipeEncryptionKey() {
	if c != nil {
		c.DataEncryptionKey = ""
	}
}

// VerifyAPISecretKey checks a candidate password or API key in constant time.
func (c *Config) VerifyAPISecretKey(candidate string) bool {
	if c == nil || candidate == "" {
		return false
	}
	candDigest := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(candDigest[:], c.apiSecretDigest[:]) == 1
}

// VerifyInternalToken validates the per-process internal service token in constant time.
func (c *Config) VerifyInternalToken(candidate string) bool {
	if c == nil || candidate == "" {
		return false
	}
	candDigest := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(candDigest[:], c.internalTokenDigest[:]) == 1
}

// IsTrustedProxy reports whether addr falls inside any configured TRUSTED_PROXIES CIDR/IP.
func (c *Config) IsTrustedProxy(addr netip.Addr) bool {
	if c == nil || !addr.IsValid() {
		return false
	}
	unmapped := addr.Unmap()
	for _, prefix := range c.TrustedProxies {
		if prefix.Contains(unmapped) {
			return true
		}
	}
	return false
}

func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	if raw == "" {
		return nil, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			pfx, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, fmt.Errorf("invalid TRUSTED_PROXIES CIDR %q: %w", item, err)
			}
			out = append(out, pfx.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXIES IP %q: %w", item, err)
		}
		unmapped := addr.Unmap()
		out = append(out, netip.PrefixFrom(unmapped, unmapped.BitLen()))
	}
	return out, nil
}

func parseBoolFlexible(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean (true/false/1/0), got %q", s)
	}
}

func normalizeLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "en", "en-us", "en-gb":
		return "en"
	case "fr", "fr-fr":
		return "fr"
	case "zh", "zh-cn", "zh_cn", "zh-hans":
		return "zh-CN"
	default:
		return strings.TrimSpace(raw)
	}
}

func generateRandomToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
