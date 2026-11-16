package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

const (
	// EncPrefix identifies authenticated AES-256-GCM ciphertexts produced by this vault.
	EncPrefix  = "enc:v1:"
	HKDFSalt   = "ovh-nexus-salt-v1"
	HKDFInfo   = "ovh-nexus-data-v1"
	AADContext = "ovh-nexus:at-rest:v1"
)

var weakMasterKeys = map[string]struct{}{
	"":                                     {},
	"123456":                               {},
	"1234567890123456":                     {},
	"ovh-nexus-default-secret-key":         {},
	"ovh-nexus-default-encryption-key":     {},
	"replace-with-a-strong-random-secret":  {},
	"replace-with-a-strong-encryption-key": {},
	"votre-cle-secrete-robuste":            {},
	"请替换为您自己的高强度密码":                        {},
	"请替换为您自己的高强度加密密钥":                      {},
	"password":                             {},
	"changeme":                             {},
	"secret":                               {},
}

// Vault provides HKDF-SHA256 + AES-256-GCM at-rest encryption without retaining
// the raw master encryption key in memory.
type Vault struct {
	aead cipher.AEAD
}

// New derives a 256-bit AES-GCM key via HKDF-SHA256 and wipes temporary key buffers.
func New(rawMasterKey string) (*Vault, error) {
	trimmed := strings.TrimSpace(rawMasterKey)
	if len(trimmed) < 16 {
		return nil, errors.New("vault initialization failed: DATA_ENCRYPTION_KEY must be at least 16 characters")
	}
	if _, weak := weakMasterKeys[trimmed]; weak {
		return nil, errors.New("vault initialization failed: DATA_ENCRYPTION_KEY uses a weak placeholder value")
	}
	if _, weakLower := weakMasterKeys[strings.ToLower(trimmed)]; weakLower {
		return nil, errors.New("vault initialization failed: DATA_ENCRYPTION_KEY uses a weak placeholder value")
	}

	ikm := []byte(trimmed)
	defer clear(ikm)

	derived := make([]byte, 32)
	defer clear(derived)

	reader := hkdf.New(sha256.New, ikm, []byte(HKDFSalt), []byte(HKDFInfo))
	if _, err := io.ReadFull(reader, derived); err != nil {
		return nil, fmt.Errorf("vault HKDF key derivation failed: %w", err)
	}

	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, fmt.Errorf("vault AES cipher initialization failed: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault GCM initialization failed: %w", err)
	}

	return &Vault{aead: aead}, nil
}

// IsEncrypted reports whether value is formatted as an enc:v1 envelope.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, EncPrefix)
}

// Encrypt encrypts plaintext into `enc:v1:<base64url_nonce>:<base64url_ciphertext>`.
// Empty values remain empty; values already bearing an enc:v1 envelope are returned unchanged.
func (v *Vault) Encrypt(plaintext string) (string, error) {
	if v == nil || v.aead == nil {
		return "", errors.New("vault is not initialized")
	}
	if plaintext == "" {
		return "", nil
	}
	if IsEncrypted(plaintext) {
		return plaintext, nil
	}

	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate AES-GCM nonce: %w", err)
	}

	ciphertext := v.aead.Seal(nil, nonce, []byte(plaintext), []byte(AADContext))
	return EncPrefix +
		base64.RawURLEncoding.EncodeToString(nonce) + ":" +
		base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an `enc:v1:<base64url_nonce>:<base64url_ciphertext>` envelope.
// Empty strings return empty. Non-envelope strings pass through as legacy plaintext;
// use DecryptStrict if legacy plaintext must be rejected.
func (v *Vault) Decrypt(value string) (string, error) {
	pt, _, err := v.DecryptWithStatus(value)
	return pt, err
}

// DecryptStrict requires an `enc:v1:...` envelope for any non-empty value.
func (v *Vault) DecryptStrict(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !IsEncrypted(value) {
		return "", errors.New("invalid encrypted envelope: missing enc:v1 prefix")
	}
	pt, _, err := v.DecryptWithStatus(value)
	return pt, err
}

// DecryptWithStatus returns (plaintext, wasLegacyPlaintext, err).
func (v *Vault) DecryptWithStatus(value string) (string, bool, error) {
	if v == nil || v.aead == nil {
		return "", false, errors.New("vault is not initialized")
	}
	if value == "" {
		return "", false, nil
	}
	if !IsEncrypted(value) {
		return value, true, nil
	}

	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != "enc" || parts[1] != "v1" {
		return "", false, errors.New("invalid encrypted envelope format")
	}

	nonce, err := decodeBase64URLFlexible(parts[2])
	if err != nil {
		return "", false, fmt.Errorf("invalid encrypted envelope nonce: %w", err)
	}
	if len(nonce) != v.aead.NonceSize() {
		return "", false, fmt.Errorf("invalid encrypted envelope nonce size: got %d, want %d", len(nonce), v.aead.NonceSize())
	}

	ciphertext, err := decodeBase64URLFlexible(parts[3])
	if err != nil {
		return "", false, fmt.Errorf("invalid encrypted envelope ciphertext: %w", err)
	}

	plaintext, err := v.aead.Open(nil, nonce, ciphertext, []byte(AADContext))
	if err != nil {
		return "", false, errors.New("decryption failed: DATA_ENCRYPTION_KEY mismatch or corrupted ciphertext")
	}

	return string(plaintext), false, nil
}

func decodeBase64URLFlexible(s string) ([]byte, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(s), "=")
	return base64.RawURLEncoding.DecodeString(trimmed)
}

// HashTokenSHA256 computes a lowercase hex SHA-256 digest of a trimmed token or recovery code.
func HashTokenSHA256(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

// VerifyTokenSHA256 compares candidate's SHA-256 digest against expectedHex in constant time.
func VerifyTokenSHA256(candidate, expectedHex string) bool {
	candHash := HashTokenSHA256(candidate)
	expected := strings.ToLower(strings.TrimSpace(expectedHex))
	if candHash == "" || len(candHash) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candHash), []byte(expected)) == 1
}

// MaskSecret redacts a secret for display, preserving at most the first 4 and last 4 characters.
func MaskSecret(secret string) string {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 8 {
		return "****"
	}
	return string(runes[:4]) + "****" + string(runes[len(runes)-4:])
}

// MaskSecretKeep redacts a secret keeping only the trailing `keep` characters (e.g. keep=0 -> "****").
func MaskSecretKeep(secret string, keep int) string {
	if secret == "" {
		return ""
	}
	if keep <= 0 {
		return "****"
	}
	runes := []rune(secret)
	if len(runes) <= keep {
		return "****"
	}
	return "****" + string(runes[len(runes)-keep:])
}

// IsMaskedPlaceholder reports whether a value represents an unchanged masked credential placeholder.
func IsMaskedPlaceholder(val string) bool {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return true
	}
	return strings.Contains(trimmed, "****") || strings.Contains(trimmed, "•")
}
