package ovhclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

func TestEndpointZoneAndIAMHelpers(t *testing.T) {
	if got := NormalizeEndpoint("OVH-US"); got != EndpointUS {
		t.Fatalf("expected ovh-us, got %q", got)
	}
	if got := NormalizeEndpoint("unknown"); got != EndpointEU {
		t.Fatalf("expected ovh-eu fallback, got %q", got)
	}
	if got := DefaultZoneForEndpoint(EndpointCA); got != "CA" {
		t.Fatalf("expected CA, got %q", got)
	}
	if got := DefaultIAM("IE"); got != "ovh-nexus-ie" {
		t.Fatalf("unexpected IAM: %q", got)
	}
	if got := EndpointBaseURL(EndpointUS); got != "https://api.us.ovhcloud.com" {
		t.Fatalf("unexpected US URL: %q", got)
	}
}

func TestPoolClientResolutionAndPublicHTTP(t *testing.T) {
	v, err := vault.New("ovhclient-test-encryption-key-01")
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "data"), v)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	pool := NewPool(st, nil)
	ctx := context.Background()

	if _, _, err := pool.ClientForAccount(ctx, ""); err == nil {
		t.Fatal("expected ErrCredentialsMissing when no accounts exist")
	}

	_, err = st.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acct-eu",
		Alias:       "EU",
		Endpoint:    "ovh-eu",
		Zone:        "IE",
		AppKey:      "ak-eu",
		AppSecret:   "as-eu",
		ConsumerKey: "ck-eu",
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("UpsertAccount eu: %v", err)
	}
	_, err = st.UpsertAccount(ctx, store.OVHAccount{
		ID:          "acct-ca",
		Alias:       "CA",
		Endpoint:    "ovh-ca",
		Zone:        "CA",
		AppKey:      "ak-ca",
		AppSecret:   "as-ca",
		ConsumerKey: "ck-ca",
	})
	if err != nil {
		t.Fatalf("UpsertAccount ca: %v", err)
	}

	c1, acc1, err := pool.ClientForAccount(ctx, "")
	if err != nil || c1 == nil || acc1.ID != "acct-eu" {
		t.Fatalf("expected default acct-eu, got acc=%+v err=%v", acc1, err)
	}
	c1Again, _, err := pool.ClientForAccount(ctx, "")
	if err != nil || c1 != c1Again {
		t.Fatal("expected cached *ovh.Client pointer reuse when credentials unchanged")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	req.Header.Set("X-OVH-Account", "acct-ca")
	c2, acc2, err := pool.ClientFromRequest(req)
	if err != nil || c2 == nil || acc2.ID != "acct-ca" || c2 == c1 {
		t.Fatalf("expected header acct-ca client, got acc=%+v err=%v", acc2, err)
	}

	if pool.PublicHTTPClient().Timeout != DefaultHTTPTimeout {
		t.Fatalf("expected 30s timeout, got %v", pool.PublicHTTPClient().Timeout)
	}
}
