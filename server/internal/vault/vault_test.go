package vault

import (
	"strings"
	"testing"
)

func TestVaultEncryptDecryptRoundTrip(t *testing.T) {
	v, err := New("super-strong-master-encryption-key-2026")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	plain := "ovh-secret-凭据-éàü-!@#$%"
	enc1, err := v.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt 1 failed: %v", err)
	}
	enc2, err := v.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}

	if !IsEncrypted(enc1) || !strings.HasPrefix(enc1, EncPrefix) {
		t.Fatalf("expected enc:v1 prefix, got %q", enc1)
	}
	if enc1 == enc2 {
		t.Fatal("expected unique random nonces across encryptions")
	}

	// Idempotent encryption of an already encrypted envelope
	again, err := v.Encrypt(enc1)
	if err != nil {
		t.Fatalf("re-Encrypt failed: %v", err)
	}
	if again != enc1 {
		t.Fatalf("expected re-Encrypt to keep existing envelope unchanged")
	}

	// Empty stays empty
	emptyEnc, err := v.Encrypt("")
	if err != nil || emptyEnc != "" {
		t.Fatalf("expected empty encrypt to return empty, got %q, %v", emptyEnc, err)
	}

	dec, err := v.Decrypt(enc1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if dec != plain {
		t.Fatalf("Decrypt mismatch: got %q, want %q", dec, plain)
	}

	decStrict, err := v.DecryptStrict(enc2)
	if err != nil || decStrict != plain {
		t.Fatalf("DecryptStrict mismatch: got %q, err=%v", decStrict, err)
	}
}

func TestVaultRejectsWrongKeyAndTampering(t *testing.T) {
	v1, err := New("master-encryption-key-alpha-1234")
	if err != nil {
		t.Fatalf("v1 New failed: %v", err)
	}
	v2, err := New("master-encryption-key-bravo-5678")
	if err != nil {
		t.Fatalf("v2 New failed: %v", err)
	}

	enc, err := v1.Encrypt("sensitive-consumer-key")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if _, err := v2.Decrypt(enc); err == nil {
		t.Fatal("expected Decrypt with wrong key to fail")
	}

	// Tamper ciphertext byte
	parts := strings.Split(enc, ":")
	if len(parts) != 4 {
		t.Fatalf("unexpected envelope format: %q", enc)
	}
	tampered := parts[0] + ":" + parts[1] + ":" + parts[2] + ":" + "AAAA" + parts[3][4:]
	if _, err := v1.Decrypt(tampered); err == nil {
		t.Fatal("expected Decrypt on tampered ciphertext to fail")
	}

	if _, err := v1.Decrypt("enc:v1:invalid"); err == nil {
		t.Fatal("expected Decrypt on malformed envelope to fail")
	}

	if _, err := v1.DecryptStrict("plaintext-value"); err == nil {
		t.Fatal("expected DecryptStrict to reject non-envelope plaintext")
	}
}

func TestVaultRejectsWeakKeys(t *testing.T) {
	if _, err := New("short"); err == nil {
		t.Fatal("expected short key error")
	}
	if _, err := New("ovh-nexus-default-encryption-key"); err == nil {
		t.Fatal("expected weak placeholder key error")
	}
}

func TestTokenHashingAndMasking(t *testing.T) {
	tok := "nxs_mcp_example_token_9876543210"
	h := HashTokenSHA256("  " + tok + "\n")
	if len(h) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %d (%q)", len(h), h)
	}
	if !VerifyTokenSHA256(tok, h) {
		t.Fatal("expected VerifyTokenSHA256 to succeed")
	}
	if VerifyTokenSHA256("wrong", h) || VerifyTokenSHA256("", h) || VerifyTokenSHA256(tok, "") {
		t.Fatal("expected VerifyTokenSHA256 to reject mismatch or empty")
	}

	if got := MaskSecret(""); got != "" {
		t.Fatalf("expected empty mask, got %q", got)
	}
	if got := MaskSecret("12345678"); got != "****" {
		t.Fatalf("expected **** for short secret, got %q", got)
	}
	if got := MaskSecret("abcdefghijkl"); got != "abcd****ijkl" {
		t.Fatalf("expected abcd****ijkl, got %q", got)
	}
	if got := MaskSecretKeep("abcdefgh", 0); got != "****" {
		t.Fatalf("expected ****, got %q", got)
	}
	if !IsMaskedPlaceholder("") || !IsMaskedPlaceholder("abcd****ijkl") || !IsMaskedPlaceholder("••••") {
		t.Fatal("expected IsMaskedPlaceholder to detect placeholders")
	}
	if IsMaskedPlaceholder("real-api-key") {
		t.Fatal("expected IsMaskedPlaceholder to return false for real secret")
	}
}
