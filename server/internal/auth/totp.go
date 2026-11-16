package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"ovh-nexus/server/internal/vault"
)

const (
	TOTPDigits          = 6
	TOTPPeriodSeconds   = 30
	TOTPWindowSteps     = 1
	SudoDurationSeconds = 300
	RecoveryCodeCount   = 8
	RecoveryAlphabet    = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	TOTPLockoutSeconds  = 300
	TOTPMaxFailures     = 6
	LoginMaxAttempts    = 5
	LoginWindowSeconds  = 300
)

var (
	sixDigitsRegex   = regexp.MustCompile(`^\d{6}$`)
	reAccountsItem   = regexp.MustCompile(`^/api/accounts/[^/]+$`)
	reQueueItem      = regexp.MustCompile(`^/api/queue/[^/]+$`)
	reServerInstall  = regexp.MustCompile(`^/api/server-control/[^/]+/install$`)
	reServerTerm     = regexp.MustCompile(`^/api/server-control/[^/]+/terminate$`)
	reServerConfirmT = regexp.MustCompile(`^/api/server-control/[^/]+/confirm-termination$`)
	reChangeContact  = regexp.MustCompile(`^/api/server-control/.+/change-contact$`)
	reContactAccept  = regexp.MustCompile(`^/api/ovh/contact-change-requests/\d+/accept$`)
	reContactRefuse  = regexp.MustCompile(`^/api/ovh/contact-change-requests/\d+/refuse$`)
	reHWReplace      = regexp.MustCompile(`^/api/server-control/[^/]+/hardware/replace$`)
	reBackupFTP      = regexp.MustCompile(`^/api/server-control/[^/]+/backup-ftp$`)
	reBackupFTPAccess = regexp.MustCompile(`^/api/server-control/[^/]+/backup-ftp/access.*$`)
	reBackupFTPPwd   = regexp.MustCompile(`^/api/server-control/[^/]+/backup-ftp/password$`)
	reServerReboot   = regexp.MustCompile(`^/api/server-control/[^/]+/reboot$`)
	reVPSReboot      = regexp.MustCompile(`^/api/ovh/vps/.+/reboot$`)
	reBootID         = regexp.MustCompile(`^/api/server-control/[^/]+/boot/\d+$`)
	reBootMode       = regexp.MustCompile(`^/api/server-control/[^/]+/boot-mode$`)
	reConsole        = regexp.MustCompile(`^/api/server-control/[^/]+/console$`)
	reIPMove         = regexp.MustCompile(`^/api/server-control/[^/]+/ip/move$`)
	reVRackDelete    = regexp.MustCompile(`^/api/server-control/[^/]+/vrack/.+$`)
	reOLAAction      = regexp.MustCompile(`^/api/server-control/[^/]+/ola/(aggregation|reset|group|ungroup)$`)
	reVNICAction     = regexp.MustCompile(`^/api/server-control/[^/]+/virtual-network-interface/[^/]+/(enable|disable)$`)
	reVirtualMAC     = regexp.MustCompile(`^/api/server-control/[^/]+/virtual-mac$`)
	reFirewall       = regexp.MustCompile(`^/api/server-control/[^/]+/firewall$`)
	reIPSecRules     = regexp.MustCompile(`^/api/server-control/.+/ip-security/rules$`)
	reIPSecRuleDel   = regexp.MustCompile(`^/api/server-control/.+/ip-security/rules/\d+$`)
	reIPSecMitig     = regexp.MustCompile(`^/api/server-control/.+/ip-security/mitigation$`)
	reSSHKeys        = regexp.MustCompile(`^/api/ovh/account/ssh-keys.*$`)
)

// TOTPSetupInfo holds newly generated pending TOTP enrollment details.
type TOTPSetupInfo struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauthUri"`
	Period     int    `json:"period"`
	Digits     int    `json:"digits"`
}

// TOTPActivationResult holds the response fields produced when 2FA is activated.
type TOTPActivationResult struct {
	Enabled                bool     `json:"enabled"`
	RecoveryCodes          []string `json:"recoveryCodes"`
	RecoveryCodesRemaining int      `json:"recoveryCodesRemaining"`
	EnabledAt              string   `json:"enabledAt"`
}

// GenerateTOTPSecret generates a 160-bit Base32 secret (32 chars without padding).
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("totp: failed to generate secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// BuildOTPAuthURI builds an otpauth://totp/... URI compatible with authenticator apps.
func BuildOTPAuthURI(secret, accountName, issuer string) string {
	if strings.TrimSpace(accountName) == "" {
		accountName = "Admin"
	}
	if strings.TrimSpace(issuer) == "" {
		issuer = "OVH Nexus"
	}
	label := issuer + ":" + accountName
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", TOTPDigits))
	params.Set("period", fmt.Sprintf("%d", TOTPPeriodSeconds))
	return "otpauth://totp/" + url.PathEscape(label) + "?" + params.Encode()
}

func decodeBase32Secret(secret string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.NewReplacer(" ", "", "\t", "", "-", "").Replace(secret))
	if cleaned == "" {
		return nil, errors.New("empty TOTP secret")
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(cleaned, "="))
}

// ComputeTOTPAtStep computes the 6-digit RFC 6238 TOTP code for a given 30-second step.
func ComputeTOTPAtStep(secret string, step int64) (string, error) {
	key, err := decodeBase32Secret(secret)
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	digest := mac.Sum(nil)

	offset := digest[len(digest)-1] & 0x0f
	binCode := (uint32(digest[offset]&0x7f) << 24) |
		(uint32(digest[offset+1]) << 16) |
		(uint32(digest[offset+2]) << 8) |
		uint32(digest[offset+3])

	mod := uint32(1)
	for range TOTPDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", TOTPDigits, binCode%mod), nil
}

// ComputeCurrentTOTP computes the 6-digit code and time step for t.
func ComputeCurrentTOTP(secret string, t time.Time) (string, int64, error) {
	step := t.Unix() / TOTPPeriodSeconds
	code, err := ComputeTOTPAtStep(secret, step)
	return code, step, err
}

func normalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "\t", "", "-", "").Replace(code))
}

// GenerateRecoveryCodes generates count XXXX-XXXX recovery codes and their SHA-256 hex digests.
func GenerateRecoveryCodes(count int) ([]string, []string, error) {
	if count <= 0 {
		count = RecoveryCodeCount
	}
	plain := make([]string, 0, count)
	hashes := make([]string, 0, count)
	alphaLen := big.NewInt(int64(len(RecoveryAlphabet)))

	for range count {
		var buf [8]byte
		for j := range 8 {
			n, err := rand.Int(rand.Reader, alphaLen)
			if err != nil {
				return nil, nil, fmt.Errorf("totp: failed to generate recovery code: %w", err)
			}
			buf[j] = RecoveryAlphabet[n.Int64()]
		}
		formatted := string(buf[:4]) + "-" + string(buf[4:])
		plain = append(plain, formatted)
		hashes = append(hashes, vault.HashTokenSHA256(normalizeRecoveryCode(formatted)))
	}
	return plain, hashes, nil
}

// ClassifyRouteTier classifies an incoming HTTP request into "strict", "sudo", or ""
// according to the high-risk security gatekeeper policy.
func ClassifyRouteTier(method, path string, body map[string]any) string {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "GET"
	}
	if m == "OPTIONS" {
		return ""
	}
	p := strings.TrimRight(strings.TrimSpace(path), "/")
	if p == "" {
		p = "/"
	}

	// 1. 2FA Self-Management (strict)
	if m == "POST" && (p == "/api/auth/totp/disable" || p == "/api/auth/totp/recovery-codes/regenerate") {
		return "strict"
	}

	// 2. OVH API Accounts & Credentials (strict)
	if m == "POST" && p == "/api/accounts" {
		return "strict"
	}
	if m == "DELETE" && reAccountsItem.MatchString(p) {
		return "strict"
	}
	if m == "POST" && p == "/api/settings" {
		if body != nil {
			if asBool(body["clearCredentials"]) {
				return "strict"
			}
			for _, field := range []string{"appKey", "appSecret", "consumerKey"} {
				if raw, exists := body[field]; exists && raw != nil {
					if str, ok := raw.(string); ok {
						if !vault.IsMaskedPlaceholder(str) {
							return "strict"
						}
					} else {
						return "strict"
					}
				}
			}
		}
		return ""
	}

	// 3. Irreversible Bare-Metal Server Lifecycle & Ownership (strict)
	if m == "POST" && (reServerInstall.MatchString(p) ||
		reServerTerm.MatchString(p) ||
		reServerConfirmT.MatchString(p) ||
		reChangeContact.MatchString(p) ||
		reContactAccept.MatchString(p) ||
		reHWReplace.MatchString(p)) {
		return "strict"
	}
	if m == "DELETE" && reBackupFTP.MatchString(p) {
		return "strict"
	}

	// 4. Reversible Server Power, Boot, Console, Network & Firewall (sudo)
	if m == "POST" && (reServerReboot.MatchString(p) ||
		reVPSReboot.MatchString(p) ||
		reIPMove.MatchString(p) ||
		reOLAAction.MatchString(p) ||
		reVNICAction.MatchString(p) ||
		reVirtualMAC.MatchString(p) ||
		reIPSecRules.MatchString(p) ||
		reIPSecMitig.MatchString(p) ||
		reBackupFTP.MatchString(p) ||
		reBackupFTPPwd.MatchString(p) ||
		reContactRefuse.MatchString(p)) {
		return "sudo"
	}
	if m == "PUT" && (reBootID.MatchString(p) || reBootMode.MatchString(p) || reFirewall.MatchString(p)) {
		return "sudo"
	}
	if m == "GET" && reConsole.MatchString(p) {
		return "sudo"
	}
	if m == "DELETE" && (reVRackDelete.MatchString(p) || reIPSecRuleDel.MatchString(p)) {
		return "sudo"
	}
	if (m == "POST" || m == "DELETE") && reBackupFTPAccess.MatchString(p) {
		return "sudo"
	}
	if (m == "POST" || m == "PUT" || m == "DELETE") && reSSHKeys.MatchString(p) {
		return "sudo"
	}

	// 5. MCP Gateway Governance (sudo)
	if (m == "PUT" || m == "POST") && (p == "/api/mcp/config" || p == "/api/mcp/settings") {
		return "sudo"
	}
	if m == "POST" && (p == "/api/mcp/token/rotate" || p == "/api/mcp/token/revoke") {
		return "sudo"
	}

	// 6. Auto-Pay & Waive EU Retraction Right in Queue / Quick-Order (sudo)
	if (m == "POST" && p == "/api/queue") ||
		(m == "PUT" && reQueueItem.MatchString(p) && p != "/api/queue/clear") ||
		(m == "POST" && p == "/api/orders/quick-order") {
		if body != nil {
			if asBool(body["auto_pay"]) || asBool(body["autoPay"]) ||
				asBool(body["waive_retractation_period"]) || asBool(body["waiveRetractationPeriod"]) {
				return "sudo"
			}
		}
	}

	return ""
}

func asBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		low := strings.ToLower(strings.TrimSpace(b))
		return low == "true" || low == "1" || low == "yes"
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

// slidingRateLimiter tracks per-key attempt timestamps within a rolling window.
type slidingRateLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	buckets     map[string][]time.Time
	nowFn       func() time.Time
}

func newSlidingRateLimiter(maxAttempts int, window time.Duration, nowFn func() time.Time) *slidingRateLimiter {
	return &slidingRateLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		buckets:     make(map[string][]time.Time),
		nowFn:       nowFn,
	}
}

// Check returns (allowed, retryAfterSeconds).
func (rl *slidingRateLimiter) Check(key string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFn()
	rl.pruneLocked(now)

	b := rl.buckets[key]
	if len(b) >= rl.maxAttempts {
		rem := int(rl.window.Seconds() - now.Sub(b[0]).Seconds())
		if rem < 1 {
			rem = 1
		}
		return false, rem
	}
	return true, 0
}

func (rl *slidingRateLimiter) RecordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFn()
	rl.pruneLocked(now)
	rl.buckets[key] = append(rl.buckets[key], now)
}

func (rl *slidingRateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, key)
}

func (rl *slidingRateLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-rl.window)
	for k, list := range rl.buckets {
		idx := 0
		for idx < len(list) && !list[idx].After(cutoff) {
			idx++
		}
		if idx >= len(list) {
			delete(rl.buckets, k)
		} else if idx > 0 {
			rl.buckets[k] = append([]time.Time(nil), list[idx:]...)
		}
	}
}

func constantTimeEqualString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
