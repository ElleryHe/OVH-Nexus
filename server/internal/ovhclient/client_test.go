package ovhclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ovh/go-ovh/ovh"

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

	if pool.PublicHTTPClient().Timeout != DefaultPublicHTTPTimeout {
		t.Fatalf("expected %v timeout, got %v", DefaultPublicHTTPTimeout, pool.PublicHTTPClient().Timeout)
	}
}

func TestMaxBytesReaderGzipCRCAndCallWithRetry429(t *testing.T) {
	// 1. maxBytesReader returns ErrResponseTooLarge instead of silent truncation.
	mr := &maxBytesReader{r: bytes.NewReader([]byte("0123456789X")), limit: 10}
	if _, err := io.ReadAll(mr); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge when payload exceeds limit, got %v", err)
	}
	if IsTransientError(ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge NOT to be treated as transient")
	}

	// 2. FetchPublicRawFromURL detects corrupted gzip CRC32 trailer.
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	_, _ = gz.Write([]byte(`{"hello":"world"}`))
	_ = gz.Close()
	corrupt := append([]byte(nil), gzBuf.Bytes()...)
	corrupt[len(corrupt)-5] ^= 0xff // flip CRC32 byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(corrupt)
	}))
	defer ts.Close()
	if _, err := FetchPublicRawFromURL(context.Background(), ts.Client(), ts.URL, "/test"); err == nil {
		t.Fatalf("expected corrupted gzip stream to return an error")
	}

	// 3. CallWithRetry retries HTTP 429 and stops immediately on HTTP 400.
	var attempts atomic.Int32
	ctx := context.Background()
	err := CallWithRetry(ctx, 2, func() error {
		if attempts.Add(1) == 1 {
			return &ovh.APIError{Code: http.StatusTooManyRequests, Message: "rate limited"}
		}
		return nil
	})
	if err != nil || attempts.Load() != 2 {
		t.Fatalf("expected CallWithRetry to succeed on attempt 2 after 429, got err=%v attempts=%d", err, attempts.Load())
	}

	attempts.Store(0)
	err = CallWithRetry(ctx, 3, func() error {
		attempts.Add(1)
		return &ovh.APIError{Code: http.StatusBadRequest, Message: "bad request"}
	})
	if err == nil || attempts.Load() != 1 {
		t.Fatalf("expected CallWithRetry NOT to retry 400, got err=%v attempts=%d", err, attempts.Load())
	}
}
