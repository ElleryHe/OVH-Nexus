package ovhclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/store"
)

const (
	EndpointEU = "ovh-eu"
	EndpointUS = "ovh-us"
	EndpointCA = "ovh-ca"

	DefaultHTTPTimeout       = 30 * time.Second
	DefaultPublicHTTPTimeout = 60 * time.Second

	// MaxPublicResponseBytes bounds decompressed public catalog responses (128 MiB).
	MaxPublicResponseBytes = 128 << 20
	// MaxPublicWireBytes bounds compressed wire payloads kept in memory prior to digest check (32 MiB).
	MaxPublicWireBytes = 32 << 20
)

var (
	// ErrCredentialsMissing indicates no configured OVH API account or incomplete credentials.
	ErrCredentialsMissing = errors.New("OVH API credentials are not configured")
	// ErrInvalidEndpoint indicates an unsupported OVH endpoint name.
	ErrInvalidEndpoint = errors.New("unsupported OVH endpoint")
)

var endpointBaseURLs = map[string]string{
	EndpointEU: "https://eu.api.ovh.com",
	EndpointUS: "https://api.us.ovhcloud.com",
	EndpointCA: "https://ca.api.ovh.com",
}

var supportedZones = []string{
	"IE", "FR", "GB", "DE", "ES", "PT", "IT", "PL", "FI", "LT", "CZ", "NL", "CA", "US",
}

// NormalizeEndpoint validates and normalizes an OVH endpoint identifier, defaulting to ovh-eu.
func NormalizeEndpoint(endpoint string) string {
	switch strings.ToLower(strings.TrimSpace(endpoint)) {
	case "", EndpointEU:
		return EndpointEU
	case EndpointUS:
		return EndpointUS
	case EndpointCA:
		return EndpointCA
	default:
		return EndpointEU
	}
}

// IsValidEndpoint reports whether endpoint is one of ovh-eu, ovh-us, or ovh-ca.
func IsValidEndpoint(endpoint string) bool {
	_, ok := endpointBaseURLs[strings.ToLower(strings.TrimSpace(endpoint))]
	return ok
}

// EndpointBaseURL returns the HTTPS origin (without /1.0) for an OVH endpoint.
func EndpointBaseURL(endpoint string) string {
	norm := NormalizeEndpoint(endpoint)
	if u, ok := endpointBaseURLs[norm]; ok {
		return u
	}
	return endpointBaseURLs[EndpointEU]
}

// EndpointAPIBaseURL returns the full /1.0 API base URL for unauthenticated public calls.
func EndpointAPIBaseURL(endpoint string) string {
	return EndpointBaseURL(endpoint) + "/1.0"
}

// DefaultZoneForEndpoint returns the default subsidiary zone for a given OVH endpoint.
func DefaultZoneForEndpoint(endpoint string) string {
	switch NormalizeEndpoint(endpoint) {
	case EndpointUS:
		return "US"
	case EndpointCA:
		return "CA"
	default:
		return "IE"
	}
}

// NormalizeZone uppercases zone or falls back to the endpoint's default subsidiary.
func NormalizeZone(zone, endpoint string) string {
	z := strings.ToUpper(strings.TrimSpace(zone))
	if z == "" {
		return DefaultZoneForEndpoint(endpoint)
	}
	return z
}

// DefaultIAM returns the canonical OVH Nexus IAM identifier for a subsidiary zone.
func DefaultIAM(zone string) string {
	z := strings.ToLower(strings.TrimSpace(zone))
	if z == "" {
		z = "ie"
	}
	return "ovh-nexus-" + z
}

// SupportedZones returns the supported OVH subsidiary codes.
func SupportedZones() []string {
	return append([]string(nil), supportedZones...)
}

type cachedClient struct {
	fingerprint string
	client      *ovh.Client
}

// Pool manages multi-account *ovh.Client instances and a shared public HTTP client.
type Pool struct {
	store      *store.Store
	log        *logger.Hub
	publicHTTP *http.Client

	mu      sync.RWMutex
	clients map[string]cachedClient
}

// NewPool constructs a multi-account OVH client pool backed by SQLite account credentials.
func NewPool(st *store.Store, log *logger.Hub) *Pool {
	return &Pool{
		store: st,
		log:   log,
		publicHTTP: &http.Client{
			Timeout: DefaultPublicHTTPTimeout,
		},
		clients: make(map[string]cachedClient),
	}
}

// PublicHTTPClient returns a 30s-timeout HTTP client for unauthenticated public OVH catalog/availability APIs.
func (p *Pool) PublicHTTPClient() *http.Client {
	if p != nil && p.publicHTTP != nil {
		return p.publicHTTP
	}
	return &http.Client{Timeout: DefaultHTTPTimeout}
}

// NewClientWithCredentials builds an isolated *ovh.Client for verifying or resolving credentials.
func NewClientWithCredentials(endpoint, appKey, appSecret, consumerKey string) (*ovh.Client, error) {
	ep := strings.ToLower(strings.TrimSpace(endpoint))
	if ep == "" {
		ep = EndpointEU
	}
	if !IsValidEndpoint(ep) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidEndpoint, endpoint)
	}
	if strings.TrimSpace(appKey) == "" || strings.TrimSpace(appSecret) == "" || strings.TrimSpace(consumerKey) == "" {
		return nil, ErrCredentialsMissing
	}

	client, err := ovh.NewClient(ep, strings.TrimSpace(appKey), strings.TrimSpace(appSecret), strings.TrimSpace(consumerKey))
	if err != nil {
		return nil, fmt.Errorf("ovhclient: failed to create client for %s: %w", ep, err)
	}
	client.Client = &http.Client{Timeout: DefaultHTTPTimeout}
	return client, nil
}

// ClientForAccount resolves the account matching accountID (or default account when empty)
// and returns a cached *ovh.Client together with the resolved account metadata.
func (p *Pool) ClientForAccount(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error) {
	if p == nil || p.store == nil {
		return nil, nil, ErrCredentialsMissing
	}

	acc, err := p.store.GetActiveAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrCredentialsMissing
		}
		return nil, nil, fmt.Errorf("ovhclient: load active account: %w", err)
	}
	if strings.TrimSpace(acc.AppKey) == "" || strings.TrimSpace(acc.AppSecret) == "" || strings.TrimSpace(acc.ConsumerKey) == "" {
		return nil, acc, ErrCredentialsMissing
	}

	ep := NormalizeEndpoint(acc.Endpoint)
	fp := credentialFingerprint(ep, acc.AppKey, acc.AppSecret, acc.ConsumerKey)

	p.mu.RLock()
	cached, ok := p.clients[acc.ID]
	p.mu.RUnlock()
	if ok && cached.fingerprint == fp && cached.client != nil {
		return cached.client, acc, nil
	}

	client, err := NewClientWithCredentials(ep, acc.AppKey, acc.AppSecret, acc.ConsumerKey)
	if err != nil {
		return nil, acc, err
	}

	p.mu.Lock()
	p.clients[acc.ID] = cachedClient{fingerprint: fp, client: client}
	p.mu.Unlock()

	if p.log != nil {
		p.log.Debug(logger.SourceOVH, "Initialized OVH API client", "accountId", acc.ID, "endpoint", ep)
	}
	return client, acc, nil
}

// ClientFromRequest resolves the active OVH client using the X-OVH-Account header or default account.
func (p *Pool) ClientFromRequest(r *http.Request) (*ovh.Client, *store.OVHAccount, error) {
	var headerID string
	if r != nil {
		headerID = strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	}
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	return p.ClientForAccount(ctx, headerID)
}

// Invalidate drops any cached *ovh.Client for accountID (call after credential updates or deletions).
func (p *Pool) Invalidate(accountID string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.TrimSpace(accountID) == "" {
		clear(p.clients)
		return
	}
	delete(p.clients, strings.TrimSpace(accountID))
}

// PublicRawResponse holds a compact wire payload (typically ~0.7-1.2 MB gzipped)
// together with the deterministic SHA-256 hex digest of its uncompressed body.
// Callers compare SHA256 before calling DecodeJSON to skip JSON parsing entirely on unchanged catalogs.
type PublicRawResponse struct {
	RawBytes []byte
	IsGzip   bool
	SHA256   string
}

// DecodeJSON streams the response directly into out without allocating an intermediate uncompressed []byte copy.
func (r *PublicRawResponse) DecodeJSON(out any) error {
	if r == nil || out == nil {
		return nil
	}
	var src io.Reader = bytes.NewReader(r.RawBytes)
	if r.IsGzip {
		gz, err := gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("ovhclient: open gzip stream: %w", err)
		}
		defer gz.Close()
		src = gz
	}
	dec := json.NewDecoder(io.LimitReader(src, MaxPublicResponseBytes))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("ovhclient: decode public JSON: %w", err)
	}
	return nil
}

// FetchPublicRawFromURL fetches targetURL requesting explicit gzip compression, buffers the compact wire payload,
// and computes the canonical SHA-256 digest of the uncompressed stream with a fixed 32 KiB buffer.
func FetchPublicRawFromURL(ctx context.Context, client *http.Client, targetURL, displayPath string) (*PublicRawResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ovhclient: build public request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "ovh-nexus/1.4.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ovhclient: public GET %s: %w", displayPath, err)
	}
	defer resp.Body.Close()

	isGzip := strings.Contains(strings.ToLower(resp.Header.Get("Content-Encoding")), "gzip")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errReader io.Reader = resp.Body
		if isGzip {
			if gz, gzErr := gzip.NewReader(resp.Body); gzErr == nil {
				defer gz.Close()
				errReader = gz
			}
		}
		errBody, _ := io.ReadAll(io.LimitReader(errReader, 4096))
		return nil, fmt.Errorf("ovhclient: public GET %s returned HTTP %d: %s", displayPath, resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	limit := int64(MaxPublicResponseBytes)
	if isGzip {
		limit = int64(MaxPublicWireBytes)
	}
	rawBytes, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("ovhclient: read public response %s: %w", displayPath, err)
	}

	h := sha256.New()
	if isGzip {
		gz, err := gzip.NewReader(bytes.NewReader(rawBytes))
		if err != nil {
			return nil, fmt.Errorf("ovhclient: decompress public response %s: %w", displayPath, err)
		}
		buf := make([]byte, 32*1024)
		_, copyErr := io.CopyBuffer(h, io.LimitReader(gz, MaxPublicResponseBytes), buf)
		_ = gz.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("ovhclient: hash public response %s: %w", displayPath, copyErr)
		}
	} else {
		_, _ = h.Write(rawBytes)
	}

	return &PublicRawResponse{
		RawBytes: rawBytes,
		IsGzip:   isGzip,
		SHA256:   hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// GetPublicRaw performs an unauthenticated GET against the public OVH /1.0 API, returning compact wire bytes and SHA-256.
func (p *Pool) GetPublicRaw(ctx context.Context, endpoint, apiPath string) (*PublicRawResponse, error) {
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	targetURL := EndpointAPIBaseURL(endpoint) + apiPath
	return FetchPublicRawFromURL(ctx, p.PublicHTTPClient(), targetURL, apiPath)
}

// GetPublicJSON performs an unauthenticated GET request against the public OVH /1.0 API using streaming JSON decoding.
func (p *Pool) GetPublicJSON(ctx context.Context, endpoint, apiPath string, out any) error {
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	targetURL := EndpointAPIBaseURL(endpoint) + apiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("ovhclient: build public request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ovh-nexus/1.4.0")

	resp, err := p.PublicHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("ovhclient: public GET %s: %w", apiPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ovhclient: public GET %s returned HTTP %d: %s", apiPath, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, MaxPublicResponseBytes)).Decode(out); err != nil {
			return fmt.Errorf("ovhclient: decode public JSON %s: %w", apiPath, err)
		}
	}
	return nil
}

// ExtractQueryID extracts the OVH-Query-ID from an *ovh.APIError when present.
func ExtractQueryID(err error) string {
	var apiErr *ovh.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return strings.TrimSpace(apiErr.QueryID)
	}
	return ""
}

// IsTransientError reports whether err is a transient network/429/5xx OVH error suitable for retry.
func IsTransientError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *ovh.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// CallWithRetry executes fn up to maxAttempts with exponential backoff on transient failures.
func CallWithRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	backoff := 1500 * time.Millisecond
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxAttempts || !IsTransientError(err) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
	return lastErr
}

func credentialFingerprint(endpoint, appKey, appSecret, consumerKey string) string {
	sum := sha256.Sum256([]byte(endpoint + "\x00" + appKey + "\x00" + appSecret + "\x00" + consumerKey))
	return hex.EncodeToString(sum[:])
}
