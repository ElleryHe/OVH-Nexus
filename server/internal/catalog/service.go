package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ovh/go-ovh/ovh"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
)

const (
	CatalogDictTTL = 12 * time.Hour
	ServerListTTL  = 2 * time.Hour
)

var safeFileRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// ServerOption represents a default or selectable hardware addon option for a server plan.
type ServerOption struct {
	Label                 string   `json:"label"`
	Value                 string   `json:"value"`
	Family                string   `json:"family,omitempty"`
	IsDefault             bool     `json:"isDefault"`
	ConfigKey             string   `json:"configKey,omitempty"`
	Options               []string `json:"options,omitempty"`
	EstimatedMonthlyPrice float64  `json:"estimatedMonthlyPrice,omitempty"`
	Price                 float64  `json:"price,omitempty"`
	Currency              string   `json:"currency,omitempty"`
	Memory                string   `json:"memory,omitempty"`
	Storage               string   `json:"storage,omitempty"`
}

// PlanConfigOption aliases ServerOption for configuration-level option metadata.
type PlanConfigOption = ServerOption

// PlanConfigItem represents a unified selectable hardware configuration (Memory + Storage) for a server plan.
type PlanConfigItem struct {
	ConfigKey             string            `json:"configKey"`
	Memory                string            `json:"memory"`
	Storage               string            `json:"storage"`
	SystemStorage         string            `json:"systemStorage,omitempty"`
	GPU                   string            `json:"gpu,omitempty"`
	MemoryLabel           string            `json:"memoryLabel"`
	StorageLabel          string            `json:"storageLabel"`
	Label                 string            `json:"label"`
	Options               []string          `json:"options"`
	IsDefault             bool              `json:"isDefault"`
	EstimatedMonthlyPrice float64           `json:"estimatedMonthlyPrice"`
	Price                 float64           `json:"price"`
	Currency              string            `json:"currency"`
	Datacenters           map[string]string `json:"datacenters"`
	InStockCount          int               `json:"inStockCount"`
}

// PlanConfigsResponse is returned by GET /api/catalog/plans/{planCode}/configs.
type PlanConfigsResponse struct {
	PlanCode         string           `json:"planCode"`
	Name             string           `json:"name"`
	Range            string           `json:"range,omitempty"`
	CPU              string           `json:"cpu,omitempty"`
	ListingState     string           `json:"listingState"`
	Orderable        bool             `json:"orderable"`
	BasePrice        float64          `json:"basePrice"`
	StartingPrice    float64          `json:"startingPrice"`
	Currency         string           `json:"currency"`
	DefaultConfigKey string           `json:"defaultConfigKey"`
	Configs          []PlanConfigItem `json:"configs"`
}

// DatacenterAvailability represents one datacenter's stock status for a server plan.
type DatacenterAvailability struct {
	Datacenter   string `json:"datacenter"`
	DCName       string `json:"dcName"`
	Region       string `json:"region"`
	Availability string `json:"availability"`
	CountryCode  string `json:"countryCode,omitempty"`
	Flag         string `json:"flag,omitempty"`
}

// ServerPlan represents an enriched bare-metal server plan returned by GET /api/servers and GET /api/catalog/plans.
type ServerPlan struct {
	PlanCode         string                   `json:"planCode"`
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	Range            string                   `json:"range,omitempty"`
	CPU              string                   `json:"cpu"`
	Memory           string                   `json:"memory"`
	Storage          string                   `json:"storage"`
	Bandwidth        string                   `json:"bandwidth"`
	VRackBandwidth   string                   `json:"vrackBandwidth"`
	MonthlyPrice     *float64                 `json:"monthlyPrice,omitempty"`
	StartingPrice    float64                  `json:"startingPrice"`
	Price            float64                  `json:"price,omitempty"`
	Currency         string                   `json:"currency,omitempty"`
	DefaultConfigKey string                   `json:"defaultConfigKey"`
	CatalogSource    string                   `json:"catalogSource,omitempty"`
	ListingState     string                   `json:"listingState"`
	ListedAt         string                   `json:"listedAt,omitempty"`
	Orderable        bool                     `json:"orderable"`
	Generation       int                      `json:"generation,omitempty"`
	Datacenters      []DatacenterAvailability `json:"datacenters"`
	DefaultOptions   []ServerOption           `json:"defaultOptions"`
	AvailableOptions []ServerOption           `json:"availableOptions"`
	Configs          []PlanConfigItem         `json:"configs,omitempty"`
}

// CacheInfo describes backend server list cache freshness for ServersPage and CacheManager.
type CacheInfo struct {
	Cached            bool   `json:"cached"`
	UsingExpiredCache bool   `json:"usingExpiredCache"`
	CacheAgeMinutes   int    `json:"cacheAgeMinutes"`
	Timestamp         *int64 `json:"timestamp"`
	CacheAge          *int64 `json:"cacheAge"`
	CacheDuration     int64  `json:"cacheDuration"`
	NextAutoRefresh   *int64 `json:"nextAutoRefresh"`
	AutoRefresh       bool   `json:"autoRefreshEnabled"`
}

// ServerListResponse matches GET /api/servers and POST /api/servers/refresh.
type ServerListResponse struct {
	Servers   []ServerPlan `json:"servers"`
	CacheInfo CacheInfo    `json:"cacheInfo"`
}

// ListServersOptions configures server catalog querying.
type ListServersOptions struct {
	AccountID    string
	Endpoint     string
	Zone         string
	ForceRefresh bool
}

// CatalogPlanSpecs holds default hardware specification codes/labels for a catalog plan.
type CatalogPlanSpecs struct {
	Memory         string `json:"memory,omitempty"`
	Storage        string `json:"storage,omitempty"`
	SystemStorage  string `json:"systemStorage,omitempty"`
	GPU            string `json:"gpu,omitempty"`
	Bandwidth      string `json:"bandwidth,omitempty"`
	VRackBandwidth string `json:"vrackBandwidth,omitempty"`
}

// CatalogPlanEntry holds merged dictionary data for a single server planCode.
type CatalogPlanEntry struct {
	PlanCode            string             `json:"planCode,omitempty"`
	Name                string             `json:"name"`
	Description         string             `json:"description,omitempty"`
	Product             string             `json:"product,omitempty"`
	Range               string             `json:"range,omitempty"`
	CPU                 *CPUInfo           `json:"cpu,omitempty"`
	Price               *float64           `json:"price,omitempty"`
	Currency            string             `json:"currency,omitempty"`
	AddonPrices         map[string]float64 `json:"addonPrices,omitempty"`
	CatalogSource       string             `json:"catalogSource,omitempty"`
	DefaultSpecs        CatalogPlanSpecs   `json:"defaultSpecs,omitempty"`
	DefaultMemory       string             `json:"defaultMemory,omitempty"`
	DefaultStorage      string             `json:"defaultStorage,omitempty"`
	DefaultConfigKey    string             `json:"defaultConfigKey,omitempty"`
	DefaultAddonOptions []string           `json:"defaultAddonOptions,omitempty"`
	DefaultOptions      []ServerOption     `json:"defaultOptions,omitempty"`
	AvailableOptions    []ServerOption     `json:"availableOptions,omitempty"`
}

// CatalogDictionary matches GET /api/catalog/dictionary consumed by src/utils/hardwareFormat.ts.
type CatalogDictionary struct {
	UpdatedAt   int64                       `json:"updatedAt"`
	Currency    string                      `json:"currency,omitempty"`
	Plans       map[string]CatalogPlanEntry `json:"plans"`
	RealPlans   map[string]CatalogPlanEntry `json:"realPlans,omitempty"`
	AddonPrices map[string]float64          `json:"addonPrices,omitempty"`
	Options     map[string]any              `json:"options"`
}

// RawAvailabilityDC represents one datacenter entry in OVH /dedicated/server/datacenter/availabilities.
type RawAvailabilityDC struct {
	Datacenter   string `json:"datacenter"`
	Availability string `json:"availability"`
}

// RawAvailabilityItem represents one hardware configuration entry in OVH /dedicated/server/datacenter/availabilities.
type RawAvailabilityItem struct {
	FQN                   string              `json:"fqn"`
	Memory                string              `json:"memory"`
	PlanCode              string              `json:"planCode"`
	Server                string              `json:"server"`
	Storage               string              `json:"storage"`
	SystemStorage         string              `json:"systemStorage,omitempty"`
	GPU                   string              `json:"gpu,omitempty"`
	Datacenters           []RawAvailabilityDC `json:"datacenters"`
	ConfigKey             string              `json:"configKey,omitempty"`
	Options               []string            `json:"options,omitempty"`
	IsDefault             bool                `json:"isDefault,omitempty"`
	EstimatedMonthlyPrice float64             `json:"estimatedMonthlyPrice,omitempty"`
	Price                 float64             `json:"price,omitempty"`
	Currency              string              `json:"currency,omitempty"`
	ListingState          string              `json:"listingState,omitempty"`
	CatalogSource         string              `json:"catalogSource,omitempty"`
	Orderable             *bool               `json:"orderable,omitempty"`
	Generation            int                 `json:"generation,omitempty"`
	ListedAt              string              `json:"listedAt,omitempty"`
}

// AvailabilityItem represents an enriched availability configuration item returned by GET /api/availability and GET /api/catalog/availability.
type AvailabilityItem struct {
	FQN                   string              `json:"fqn"`
	Memory                string              `json:"memory"`
	PlanCode              string              `json:"planCode"`
	Server                string              `json:"server"`
	Storage               string              `json:"storage"`
	SystemStorage         string              `json:"systemStorage,omitempty"`
	GPU                   string              `json:"gpu,omitempty"`
	Datacenters           []RawAvailabilityDC `json:"datacenters"`
	ConfigKey             string              `json:"configKey"`
	Options               []string            `json:"options"`
	IsDefault             bool                `json:"isDefault"`
	EstimatedMonthlyPrice float64             `json:"estimatedMonthlyPrice"`
	Price                 float64             `json:"price"`
	Currency              string              `json:"currency"`
	ListingState          string              `json:"listingState,omitempty"`
	CatalogSource         string              `json:"catalogSource,omitempty"`
	Orderable             bool                `json:"orderable"`
	Generation            int                 `json:"generation,omitempty"`
	ListedAt              string              `json:"listedAt,omitempty"`
}

// ConfigAvailability holds configuration-level availability and matched catalog addons.
type ConfigAvailability struct {
	FQN                   string            `json:"fqn"`
	ConfigKey             string            `json:"configKey"`
	Memory                string            `json:"memory"`
	Storage               string            `json:"storage"`
	MemoryLabel           string            `json:"memoryLabel,omitempty"`
	StorageLabel          string            `json:"storageLabel,omitempty"`
	Label                 string            `json:"label,omitempty"`
	Datacenters           map[string]string `json:"datacenters"`
	InStockCount          int               `json:"inStockCount"`
	Options               []string          `json:"options"`
	IsDefault             bool              `json:"isDefault"`
	EstimatedMonthlyPrice float64           `json:"estimatedMonthlyPrice"`
	Price                 float64           `json:"price,omitempty"`
	Currency              string            `json:"currency,omitempty"`
	ListingState          string            `json:"listingState,omitempty"`
	Orderable             bool              `json:"orderable"`
	Generation            int               `json:"generation,omitempty"`
}

// ServerPriceValues holds structured tax-inclusive and tax-exclusive monthly pricing.
type ServerPriceValues struct {
	WithTax      *float64 `json:"withTax"`
	WithoutTax   *float64 `json:"withoutTax"`
	Tax          *float64 `json:"tax"`
	CurrencyCode string   `json:"currencyCode"`
}

// ServerPriceItem holds per-item pricing inside a cart quote.
type ServerPriceItem struct {
	ItemID      any               `json:"itemId"`
	PlanCode    string            `json:"planCode"`
	Description string            `json:"description"`
	Prices      ServerPriceValues `json:"prices"`
}

// ServerPricePayload holds cart summary and item breakdown pricing.
type ServerPricePayload struct {
	PricingMode string            `json:"pricingMode"`
	Prices      ServerPriceValues `json:"prices"`
	Items       []ServerPriceItem `json:"items"`
}

// ServerPriceResult matches POST /api/servers/{planCode}/price.
type ServerPriceResult struct {
	Success    bool                `json:"success"`
	Code       string              `json:"code,omitempty"`
	PlanCode   string              `json:"planCode,omitempty"`
	Datacenter string              `json:"datacenter,omitempty"`
	Options    []string            `json:"options,omitempty"`
	Price      *ServerPricePayload `json:"price"`
	Error      string              `json:"error,omitempty"`
}

// VPSDatacenterStatus represents one datacenter inside /vps/order/rule/datacenter.
type VPSDatacenterStatus struct {
	Datacenter         string `json:"datacenter"`
	Code               string `json:"code"`
	Status             string `json:"status"`
	DaysBeforeDelivery int    `json:"daysBeforeDelivery"`
}

// VPSAvailabilityResponse models OVH /vps/order/rule/datacenter JSON output.
type VPSAvailabilityResponse struct {
	Datacenters []VPSDatacenterStatus `json:"datacenters"`
}

// CacheInfoResponse matches GET /api/cache/info consumed by src/components/CacheManager.tsx.
type CacheInfoResponse struct {
	Backend struct {
		HasCachedData bool   `json:"hasCachedData"`
		Timestamp     *int64 `json:"timestamp"`
		CacheAge      *int64 `json:"cacheAge"`
		CacheDuration int64  `json:"cacheDuration"`
		ServerCount   int    `json:"serverCount"`
		CacheValid    bool   `json:"cacheValid"`
	} `json:"backend"`
	Storage struct {
		DataDir  string `json:"dataDir"`
		CacheDir string `json:"cacheDir"`
		LogsDir  string `json:"logsDir"`
		Files    struct {
			Config  bool `json:"config"`
			Servers bool `json:"servers"`
			Logs    bool `json:"logs"`
			Queue   bool `json:"queue"`
			History bool `json:"history"`
		} `json:"files"`
	} `json:"storage"`
}

type cachedServersEntry struct {
	UpdatedAt int64        `json:"updatedAt"`
	Servers   []ServerPlan `json:"servers"`
}

// flightGroup deduplicates concurrent upstream requests with the same key.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

func (g *flightGroup) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*flightCall)
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &flightCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	defer func() {
		c.wg.Done()
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
	}()

	c.val, c.err = fn()
	return c.val, c.err
}

// Service manages OVH public catalog fetching, enrichment, disk/memory caching, and pricing.
type Service struct {
	store    *store.Store
	log      *logger.Hub
	ovhPool  *ovhclient.Pool
	dataDir  string
	cacheDir string
	logsDir  string

	mu           sync.RWMutex
	dictCache    map[string]*CatalogDictionary
	serversCache map[string]*cachedServersEntry
	rawCatalogs  map[string]*mergedRawCatalogs
	vpsCache     map[string]*cachedVPSCatalog
	flights      flightGroup

	publicBaseOverride string
	accountClientFn    func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)
	listedAtResolver   func(planCode string) string
}

type mergedRawCatalogs struct {
	UpdatedAt int64
	Eco       map[string]any
	Baremetal map[string]any
	VPS       map[string]any
}

type cachedVPSCatalog struct {
	UpdatedAt int64          `json:"updatedAt"`
	Data      map[string]any `json:"data"`
}

// NewService constructs a catalog Service storing JSON cache artifacts under <dataDir>/cache.
func NewService(st *store.Store, log *logger.Hub, ovhPool *ovhclient.Pool, dataDir string) *Service {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = filepath.Join(os.TempDir(), "ovh-nexus-data")
	}
	cacheDir := filepath.Join(dataDir, "cache")
	logsDir := filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(cacheDir, 0o700)

	return &Service{
		store:        st,
		log:          log,
		ovhPool:      ovhPool,
		dataDir:      dataDir,
		cacheDir:     cacheDir,
		logsDir:      logsDir,
		dictCache:    make(map[string]*CatalogDictionary),
		serversCache: make(map[string]*cachedServersEntry),
		rawCatalogs:  make(map[string]*mergedRawCatalogs),
		vpsCache:     make(map[string]*cachedVPSCatalog),
	}
}

// SetPublicAPIBaseURL overrides the OVH public /1.0 API base URL (intended for tests).
func (s *Service) SetPublicAPIBaseURL(baseURL string) {
	s.publicBaseOverride = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// SetAccountClientFunc overrides authenticated OVH client lookup (intended for tests).
func (s *Service) SetAccountClientFunc(fn func(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error)) {
	s.accountClientFn = fn
}

// SetListedAtResolver sets a callback to resolve the SKU listedAt timestamp for a planCode.
func (s *Service) SetListedAtResolver(fn func(planCode string) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listedAtResolver = fn
}

func (s *Service) resolveListedAt(planCode string) string {
	s.mu.RLock()
	fn := s.listedAtResolver
	s.mu.RUnlock()
	if fn == nil {
		return ""
	}
	return fn(planCode)
}

func (s *Service) resolveEndpointAndZone(ctx context.Context, accountID, endpoint, zone string) (string, string) {
	ep := strings.TrimSpace(endpoint)
	zn := strings.TrimSpace(zone)
	if (ep == "" || zn == "") && s.store != nil {
		if acc, err := s.store.GetActiveAccount(ctx, accountID); err == nil && acc != nil {
			if ep == "" {
				ep = acc.Endpoint
			}
			if zn == "" {
				zn = acc.Zone
			}
		}
	}
	ep = ovhclient.NormalizeEndpoint(ep)
	zn = ovhclient.NormalizeZone(zn, ep)
	return ep, zn
}

func normalizeSubsidiary(endpoint, zone string) string {
	sub := strings.ToUpper(strings.TrimSpace(zone))
	if sub == "" {
		sub = ovhclient.DefaultZoneForEndpoint(endpoint)
	}
	if endpoint == ovhclient.EndpointUS {
		return "US"
	}
	if endpoint == ovhclient.EndpointCA {
		switch sub {
		case "CA", "QC", "WE", "WS", "SG", "AU", "IN":
		default:
			return "CA"
		}
	}
	return sub
}

func (s *Service) fetchPublicJSON(ctx context.Context, endpoint, apiPath string, out any) error {
	if s.publicBaseOverride != "" {
		if !strings.HasPrefix(apiPath, "/") {
			apiPath = "/" + apiPath
		}
		target := s.publicBaseOverride + apiPath
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		client := &http.Client{Timeout: ovhclient.DefaultHTTPTimeout}
		if s.ovhPool != nil {
			client = s.ovhPool.PublicHTTPClient()
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("public GET %s returned HTTP %d: %s", apiPath, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return json.Unmarshal(body, out)
	}
	if s.ovhPool == nil {
		return errors.New("catalog: OVH client pool is not configured")
	}
	return s.ovhPool.GetPublicJSON(ctx, endpoint, apiPath, out)
}

func (s *Service) fetchPublicCatalog(ctx context.Context, endpoint, zone, catName string) map[string]any {
	sub := normalizeSubsidiary(endpoint, zone)
	apiPath := fmt.Sprintf("/order/catalog/public/%s?ovhSubsidiary=%s", catName, url.QueryEscape(sub))
	var payload map[string]any
	if err := s.fetchPublicJSON(ctx, endpoint, apiPath, &payload); err != nil {
		if s.log != nil {
			s.log.Debug(logger.SourceCatalog, "Public catalog fetch skipped or failed", "catalog", catName, "endpoint", endpoint, "subsidiary", sub, "error", err.Error())
		}
		return nil
	}
	return payload
}

// GetRawAvailabilities fetches raw datacenter availabilities from the OVH public API.
func (s *Service) GetRawAvailabilities(ctx context.Context, endpoint, planCode string) ([]RawAvailabilityItem, error) {
	ep, _ := s.resolveEndpointAndZone(ctx, "", endpoint, "")
	apiPath := "/dedicated/server/datacenter/availabilities"
	if strings.TrimSpace(planCode) != "" {
		apiPath += "?planCode=" + url.QueryEscape(strings.TrimSpace(planCode))
	}
	key := "avail:" + ep + ":" + strings.TrimSpace(planCode)
	val, err := s.flights.do(key, func() (any, error) {
		var items []RawAvailabilityItem
		if err := s.fetchPublicJSON(ctx, ep, apiPath, &items); err != nil {
			return nil, err
		}
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return val.([]RawAvailabilityItem), nil
}

// GetDictionary returns the merged Eco + Baremetal + VPS catalog dictionary, cached in memory & disk (12h TTL).
func (s *Service) GetDictionary(ctx context.Context, endpoint, zone string, forceRefresh bool) (*CatalogDictionary, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	cacheKey := ep + ":" + zn
	diskPath := s.dictCachePath(ep, zn)
	now := time.Now().Unix()

	if !forceRefresh {
		s.mu.RLock()
		mem := s.dictCache[cacheKey]
		s.mu.RUnlock()
		if mem != nil && (now-mem.UpdatedAt) < int64(CatalogDictTTL.Seconds()) {
			return mem, nil
		}

		if diskDict, ok := readJSONFile[CatalogDictionary](diskPath); ok {
			if (now - diskDict.UpdatedAt) < int64(CatalogDictTTL.Seconds()) {
				s.mu.Lock()
				s.dictCache[cacheKey] = &diskDict
				s.mu.Unlock()
				return &diskDict, nil
			}
		}
	}

	flightKey := fmt.Sprintf("dict:%s:%s:%v", ep, zn, forceRefresh)
	val, err := s.flights.do(flightKey, func() (any, error) {
		raw := s.fetchMergedRawCatalogs(ctx, ep, zn)
		if raw.Eco != nil || raw.Baremetal != nil || raw.VPS != nil {
			merged := BuildDictionaryFromCatalogs(raw.Eco, raw.Baremetal, raw.VPS)
			_ = writeJSONFile(diskPath, merged)
			s.mu.Lock()
			s.dictCache[cacheKey] = merged
			s.rawCatalogs[cacheKey] = raw
			s.mu.Unlock()
			if s.log != nil {
				s.log.Info(logger.SourceCatalog, "Refreshed OVH catalog dictionary", "endpoint", ep, "zone", zn, "plans", len(merged.Plans), "realPlans", len(merged.RealPlans))
			}
			return merged, nil
		}

		// Fallback to stale disk cache if upstream failed.
		if stale, ok := readJSONFile[CatalogDictionary](diskPath); ok {
			s.mu.Lock()
			s.dictCache[cacheKey] = &stale
			s.mu.Unlock()
			return &stale, nil
		}
		empty := &CatalogDictionary{
			UpdatedAt:   now,
			Currency:    "EUR",
			Plans:       make(map[string]CatalogPlanEntry),
			RealPlans:   make(map[string]CatalogPlanEntry),
			AddonPrices: make(map[string]float64),
			Options:     make(map[string]any),
		}
		return empty, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*CatalogDictionary), nil
}

// GetBaremetalDictionary fetches ONLY the eco and baremetalServers public catalogs (skipping vps),
// used by bare-metal SKU monitoring so VPS catalog latency/plans never affect bare-metal SKU checks.
func (s *Service) GetBaremetalDictionary(ctx context.Context, endpoint, zone string, forceRefresh bool) (*CatalogDictionary, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	cacheKey := ep + ":" + zn
	diskPath := s.dictCachePath(ep, zn)
	now := time.Now().Unix()

	if !forceRefresh {
		s.mu.RLock()
		mem := s.dictCache[cacheKey]
		s.mu.RUnlock()
		if mem != nil && len(mem.RealPlans) > 0 && (now-mem.UpdatedAt) < int64(CatalogDictTTL.Seconds()) {
			return mem, nil
		}
		if diskDict, ok := readJSONFile[CatalogDictionary](diskPath); ok && len(diskDict.RealPlans) > 0 {
			if (now - diskDict.UpdatedAt) < int64(CatalogDictTTL.Seconds()) {
				s.mu.Lock()
				s.dictCache[cacheKey] = &diskDict
				s.mu.Unlock()
				return &diskDict, nil
			}
		}
	}

	flightKey := fmt.Sprintf("bmdict:%s:%s:%v", ep, zn, forceRefresh)
	val, err := s.flights.do(flightKey, func() (any, error) {
		var wg sync.WaitGroup
		var eco, bm map[string]any
		wg.Add(2)
		go func() {
			defer wg.Done()
			eco = s.fetchPublicCatalog(ctx, ep, zn, "eco")
		}()
		go func() {
			defer wg.Done()
			bm = s.fetchPublicCatalog(ctx, ep, zn, "baremetalServers")
		}()
		wg.Wait()

		if eco != nil || bm != nil {
			merged := BuildDictionaryFromCatalogs(eco, bm, nil)
			_ = writeJSONFile(diskPath, merged)
			s.mu.Lock()
			s.dictCache[cacheKey] = merged
			s.mu.Unlock()
			if s.log != nil {
				s.log.Info(logger.SourceCatalog, "Refreshed OVH baremetal catalog dictionary", "endpoint", ep, "zone", zn, "realPlans", len(merged.RealPlans))
			}
			return merged, nil
		}

		if stale, ok := readJSONFile[CatalogDictionary](diskPath); ok {
			s.mu.Lock()
			s.dictCache[cacheKey] = &stale
			s.mu.Unlock()
			return &stale, nil
		}
		empty := &CatalogDictionary{
			UpdatedAt:   now,
			Currency:    "EUR",
			Plans:       make(map[string]CatalogPlanEntry),
			RealPlans:   make(map[string]CatalogPlanEntry),
			AddonPrices: make(map[string]float64),
			Options:     make(map[string]any),
		}
		return empty, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*CatalogDictionary), nil
}

func (s *Service) fetchMergedRawCatalogs(ctx context.Context, endpoint, zone string) *mergedRawCatalogs {
	var wg sync.WaitGroup
	var eco, bm, vps map[string]any

	wg.Add(3)
	go func() {
		defer wg.Done()
		eco = s.fetchPublicCatalog(ctx, endpoint, zone, "eco")
	}()
	go func() {
		defer wg.Done()
		bm = s.fetchPublicCatalog(ctx, endpoint, zone, "baremetalServers")
	}()
	go func() {
		defer wg.Done()
		vps = s.fetchPublicCatalog(ctx, endpoint, zone, "vps")
		if vps != nil {
			_ = writeJSONFile(s.vpsCachePath(endpoint, zone), cachedVPSCatalog{
				UpdatedAt: time.Now().Unix(),
				Data:      vps,
			})
			s.mu.Lock()
			s.vpsCache[endpoint+":"+zone] = &cachedVPSCatalog{UpdatedAt: time.Now().Unix(), Data: vps}
			s.mu.Unlock()
		}
	}()
	wg.Wait()

	return &mergedRawCatalogs{
		UpdatedAt: time.Now().Unix(),
		Eco:       eco,
		Baremetal: bm,
		VPS:       vps,
	}
}

// GetVPSCatalog returns the public VPS catalog JSON (cached 12h on disk and in memory).
func (s *Service) GetVPSCatalog(ctx context.Context, endpoint, zone string, forceRefresh bool) (map[string]any, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	cacheKey := ep + ":" + zn
	diskPath := s.vpsCachePath(ep, zn)
	now := time.Now().Unix()

	if !forceRefresh {
		s.mu.RLock()
		mem := s.vpsCache[cacheKey]
		s.mu.RUnlock()
		if mem != nil && (now-mem.UpdatedAt) < int64(CatalogDictTTL.Seconds()) && mem.Data != nil {
			return mem.Data, nil
		}
		if disk, ok := readJSONFile[cachedVPSCatalog](diskPath); ok && disk.Data != nil {
			if (now - disk.UpdatedAt) < int64(CatalogDictTTL.Seconds()) {
				s.mu.Lock()
				s.vpsCache[cacheKey] = &disk
				s.mu.Unlock()
				return disk.Data, nil
			}
		}
	}

	val, err := s.flights.do(fmt.Sprintf("vps:%s:%v", cacheKey, forceRefresh), func() (any, error) {
		data := s.fetchPublicCatalog(ctx, ep, zn, "vps")
		if data == nil {
			if stale, ok := readJSONFile[cachedVPSCatalog](diskPath); ok && stale.Data != nil {
				return stale.Data, nil
			}
			return nil, errors.New("failed to fetch OVH VPS catalog")
		}
		entry := &cachedVPSCatalog{UpdatedAt: time.Now().Unix(), Data: data}
		_ = writeJSONFile(diskPath, entry)
		s.mu.Lock()
		s.vpsCache[cacheKey] = entry
		s.mu.Unlock()
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(map[string]any), nil
}

// BuildDictionaryFromCatalogs merges raw eco, baremetalServers, and vps catalogs into a CatalogDictionary.
func BuildDictionaryFromCatalogs(eco, baremetal, vps map[string]any) *CatalogDictionary {
	productsByName := make(map[string]map[string]any)
	addonsPriceByCode := make(map[string]struct {
		product string
		price   *float64
	})
	plansMap := make(map[string]CatalogPlanEntry)
	realPlansMap := make(map[string]CatalogPlanEntry)
	globalAddonPrices := make(map[string]float64)
	optionsMap := make(map[string]any)
	defaultCurrency := "EUR"

	type catalogSourcePair struct {
		source string
		data   map[string]any
	}
	sources := []catalogSourcePair{
		{source: "eco", data: eco},
		{source: "baremetalServers", data: baremetal},
		{source: "vps", data: vps},
	}

	for _, src := range sources {
		cat := src.data
		if cat == nil {
			continue
		}
		if loc, ok := cat["locale"].(map[string]any); ok {
			if cc, ok := loc["currencyCode"].(string); ok && strings.TrimSpace(cc) != "" {
				defaultCurrency = strings.ToUpper(strings.TrimSpace(cc))
			}
		}
		for _, rawAddon := range sliceOfMaps(cat["addons"]) {
			ac := strings.TrimSpace(strFromMap(rawAddon, "planCode"))
			if ac == "" {
				continue
			}
			prodCode := strings.TrimSpace(strFromMap(rawAddon, "product"))
			pr := extractMonthlyPrice(rawAddon)
			addonsPriceByCode[ac] = struct {
				product string
				price   *float64
			}{product: prodCode, price: pr}
			if pr != nil && src.source != "vps" {
				globalAddonPrices[ac] = *pr
				if prodCode != "" {
					if cur, exists := globalAddonPrices[prodCode]; !exists || (cur == 0 && *pr > 0) {
						globalAddonPrices[prodCode] = *pr
					}
				}
			}
		}
		for _, prod := range sliceOfMaps(cat["products"]) {
			pname, _ := prod["name"].(string)
			if pname == "" {
				continue
			}
			productsByName[pname] = prod
			blobs, _ := prod["blobs"].(map[string]any)
			tech, _ := blobs["technical"].(map[string]any)
			if memMap, ok := tech["memory"].(map[string]any); ok {
				optionsMap[pname] = map[string]any{
					"kind":   "memory",
					"parsed": ParseMemoryCode(pname, memMap),
				}
			} else if _, hasStorage := tech["storage"]; hasStorage ||
				strings.HasPrefix(pname, "softraid-") ||
				strings.HasPrefix(pname, "hardraid-") ||
				strings.HasPrefix(pname, "hybridsoftraid-") ||
				strings.HasPrefix(pname, "noraid-") {
				optionsMap[pname] = map[string]any{
					"kind":   "storage",
					"parsed": ParseStorageCode(pname, strings.HasSuffix(pname, "-system")),
				}
			} else if _, hasGPU := tech["gpu"]; hasGPU || strings.HasPrefix(pname, "gpu-") {
				optionsMap[pname] = map[string]any{
					"kind":   "gpu",
					"parsed": ParseGPUCode(pname),
				}
			}
		}
	}

	for _, src := range sources {
		cat := src.data
		if cat == nil {
			continue
		}
		catCurrency := defaultCurrency
		if loc, ok := cat["locale"].(map[string]any); ok {
			if cc, ok := loc["currencyCode"].(string); ok && strings.TrimSpace(cc) != "" {
				catCurrency = strings.ToUpper(strings.TrimSpace(cc))
			}
		}
		for _, plan := range sliceOfMaps(cat["plans"]) {
			planCode := strings.TrimSpace(strFromMap(plan, "planCode"))
			if planCode == "" {
				continue
			}
			productKey := strings.TrimSpace(strFromMap(plan, "product"))
			invoiceName := strings.TrimSpace(strFromMap(plan, "invoiceName"))
			desc := strings.TrimSpace(strFromMap(plan, "description"))
			if desc == "" {
				desc = invoiceName
			}
			commName := nestedString(plan, "blobs", "commercial", "name")

			parts := splitPipeNonEmpty(invoiceName)
			displayName := commName
			if displayName == "" && len(parts) > 0 {
				displayName = parts[0]
			}
			if displayName == "" {
				displayName = planCode
			}
			invoiceCPU := ""
			if len(parts) > 1 {
				invoiceCPU = parts[1]
			}

			prodEntry := productsByName[productKey]
			if prodEntry == nil {
				prodEntry = productsByName[planCode]
			}
			var srvTech map[string]any
			if prodEntry != nil {
				srvTech = nestedMap(prodEntry, "blobs", "technical", "server")
			}
			cpuRaw, _ := srvTech["cpu"].(map[string]any)
			rawRange, _ := srvTech["range"].(string)
			if src.source == "vps" && rawRange == "" {
				rawRange = "vps"
			}

			var cpuInfo *CPUInfo
			if model, _ := cpuRaw["model"].(string); model != "" || invoiceCPU != "" {
				brand, _ := cpuRaw["brand"].(string)
				if model == "" {
					model = invoiceCPU
				}
				cpuInfo = &CPUInfo{
					Brand:     brand,
					Model:     model,
					Cores:     intFromAny(cpuRaw["cores"]),
					Threads:   intFromAny(cpuRaw["threads"]),
					Frequency: floatFromAny(cpuRaw["frequency"]),
					Boost:     floatFromAny(cpuRaw["boost"]),
				}
			}

			normRange := ClassifyProductRange(displayName, rawRange, planCode)
			baseCode, regionInfo := ParsePlanRegion(planCode)
			noVerCode := StripPlanVersion(planCode)
			basePrice := extractMonthlyPrice(plan)
			basePriceVal := 0.0
			if basePrice != nil {
				basePriceVal = *basePrice
			}
			if basePrice != nil && src.source != "vps" {
				globalAddonPrices[planCode] = *basePrice
			}

			addonPrices := make(map[string]float64)
			var defMemCode, defStoCode, defSysStoCode, defGPUCode, defBWStr, defVRackStr string
			var defaultAddonOpts []string
			defaultOpts := make([]ServerOption, 0)
			availOpts := make([]ServerOption, 0)
			hasMemory := false
			hasStorage := false

			for _, fam := range sliceOfMaps(plan["addonFamilies"]) {
				famName := strings.ToLower(strings.TrimSpace(strFromMap(fam, "name")))
				defAddon := strings.TrimSpace(strFromMap(fam, "default"))
				addons := sliceOfStrings(fam["addons"])

				isMemFam := strings.Contains(famName, "memory") || strings.Contains(famName, "ram")
				isStoFam := (strings.Contains(famName, "storage") || strings.Contains(famName, "disk") || strings.Contains(famName, "drive")) && !strings.Contains(famName, "system")
				isSysStoFam := strings.Contains(famName, "system-storage") || strings.Contains(famName, "system_storage")
				isGPUFam := strings.Contains(famName, "gpu")
				if isMemFam {
					hasMemory = true
				}
				if isStoFam || isSysStoFam {
					hasStorage = true
				}

				for _, rawAC := range addons {
					rawAC = strings.TrimSpace(rawAC)
					if rawAC == "" || isLicenseOrOSOption(rawAC) {
						continue
					}
					ap, hasAP := addonsPriceByCode[rawAC]
					addonDelta := 0.0
					if hasAP && ap.price != nil {
						addonDelta = *ap.price
						if *ap.price > 0 {
							addonPrices[rawAC] = *ap.price
							if ap.product != "" {
								addonPrices[ap.product] = *ap.price
							}
						}
					}
					prodKey := ap.product
					if prodKey == "" {
						prodKey = stripPlanAddonSuffix(rawAC, planCode, baseCode, noVerCode, productKey)
					}
					isDef := rawAC == defAddon
					estOptPrice := roundCents(basePriceVal + addonDelta)
					opt := ServerOption{
						Label:                 rawAC,
						Value:                 rawAC,
						Family:                famName,
						IsDefault:             isDef,
						ConfigKey:             prodKey,
						Options:               []string{rawAC},
						EstimatedMonthlyPrice: estOptPrice,
						Price:                 addonDelta,
						Currency:              catCurrency,
					}
					if isMemFam {
						opt.Memory = prodKey
					} else if isStoFam {
						opt.Storage = prodKey
					}
					availOpts = append(availOpts, opt)
					if isDef {
						defaultOpts = append(defaultOpts, opt)
					}
				}

				if defAddon != "" {
					ap := addonsPriceByCode[defAddon]
					prodKey := ap.product
					if prodKey == "" {
						prodKey = stripPlanAddonSuffix(defAddon, planCode, baseCode, noVerCode, productKey)
					}
					switch {
					case isMemFam && defMemCode == "":
						defMemCode = prodKey
						if !slices.Contains(defaultAddonOpts, defAddon) {
							defaultAddonOpts = append(defaultAddonOpts, defAddon)
						}
					case isStoFam && defStoCode == "":
						defStoCode = prodKey
						if !slices.Contains(defaultAddonOpts, defAddon) {
							defaultAddonOpts = append(defaultAddonOpts, defAddon)
						}
					case isSysStoFam && defSysStoCode == "":
						defSysStoCode = prodKey
					case isGPUFam && defGPUCode == "":
						defGPUCode = prodKey
					case strings.Contains(famName, "vrack") && defVRackStr == "":
						defVRackStr = FormatVRackCode(defAddon)
					case (strings.Contains(famName, "bandwidth") || strings.Contains(famName, "traffic") || strings.Contains(famName, "network")) && defBWStr == "":
						if strings.Contains(strings.ToLower(defAddon), "vrack") {
							defVRackStr = FormatVRackCode(defAddon)
						} else {
							defBWStr = FormatBandwidthCode(defAddon)
						}
					}
				}
			}

			prodName := productKey
			if prodName == "" {
				prodName = baseCode
			}
			defConfigKey := BuildConfigKey(defMemCode, defStoCode)

			entry := CatalogPlanEntry{
				PlanCode:      planCode,
				Name:          displayName,
				Description:   desc,
				Product:       prodName,
				Range:         normRange,
				CPU:           cpuInfo,
				Price:         basePrice,
				Currency:      catCurrency,
				AddonPrices:   addonPrices,
				CatalogSource: src.source,
				DefaultSpecs: CatalogPlanSpecs{
					Memory:         defMemCode,
					Storage:        defStoCode,
					SystemStorage:  defSysStoCode,
					GPU:            defGPUCode,
					Bandwidth:      defBWStr,
					VRackBandwidth: defVRackStr,
				},
				DefaultMemory:       defMemCode,
				DefaultStorage:      defStoCode,
				DefaultConfigKey:    defConfigKey,
				DefaultAddonOptions: defaultAddonOpts,
				DefaultOptions:      defaultOpts,
				AvailableOptions:    availOpts,
			}

			if (src.source == "eco" || src.source == "baremetalServers") && !IsNonServerAddonCode(planCode) {
				if (hasMemory && hasStorage) || (!hasMemory && !hasStorage) {
					realPlansMap[planCode] = entry
				}
			}

			plansMap[planCode] = entry
			isRegional := regionInfo != nil && regionInfo.Code != ""
			if baseCode != "" {
				if _, exists := plansMap[baseCode]; !exists || !isRegional {
					plansMap[baseCode] = entry
				}
			}
			if noVerCode != "" {
				if _, exists := plansMap[noVerCode]; !exists || !isRegional {
					plansMap[noVerCode] = entry
				}
			}
			if productKey != "" {
				if _, exists := plansMap[productKey]; !exists || !isRegional {
					plansMap[productKey] = entry
				}
			}
		}
	}

	return &CatalogDictionary{
		UpdatedAt:   time.Now().Unix(),
		Currency:    defaultCurrency,
		Plans:       plansMap,
		RealPlans:   realPlansMap,
		AddonPrices: globalAddonPrices,
		Options:     optionsMap,
	}
}

func stripPlanAddonSuffix(addonCode string, suffixes ...string) string {
	trimmed := strings.TrimSpace(addonCode)
	low := strings.ToLower(trimmed)
	for _, s := range suffixes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		suf := "-" + strings.ToLower(s)
		if strings.HasSuffix(low, suf) && len(trimmed) > len(suf) {
			return trimmed[:len(trimmed)-len(suf)]
		}
	}
	return trimmed
}

func lookupPlanCaseInsensitive(m map[string]CatalogPlanEntry, key string) (CatalogPlanEntry, bool) {
	if len(m) == 0 || strings.TrimSpace(key) == "" {
		return CatalogPlanEntry{}, false
	}
	if entry, ok := m[key]; ok {
		return entry, true
	}
	low := strings.ToLower(strings.TrimSpace(key))
	if entry, ok := m[low]; ok {
		return entry, true
	}
	for k, entry := range m {
		if strings.EqualFold(k, low) {
			return entry, true
		}
	}
	return CatalogPlanEntry{}, false
}

// EstimateConfigMonthlyPrice computes the estimated monthly ex-VAT price for a (memory, storage) configuration.
func EstimateConfigMonthlyPrice(
	basePrice float64,
	memory, storage string,
	planEntry *CatalogPlanEntry,
	dict *CatalogDictionary,
) float64 {
	if basePrice <= 0 {
		return 0
	}
	lookupAddon := func(code, defaultCode string) float64 {
		c := strings.TrimSpace(code)
		if c == "" || strings.EqualFold(c, "N/A") || strings.EqualFold(c, "none") {
			return 0
		}
		if defaultCode != "" && (strings.EqualFold(c, defaultCode) || StandardizeConfig(c) == StandardizeConfig(defaultCode)) {
			if planEntry != nil && len(planEntry.AddonPrices) > 0 {
				if v, ok := planEntry.AddonPrices[c]; ok {
					return v
				}
			}
			return 0
		}
		if planEntry != nil && len(planEntry.AddonPrices) > 0 {
			if v, ok := planEntry.AddonPrices[c]; ok {
				return v
			}
			low := strings.ToLower(c)
			for k, v := range planEntry.AddonPrices {
				if strings.EqualFold(k, low) || strings.HasPrefix(strings.ToLower(k), low+"-") {
					return v
				}
			}
		}
		if dict != nil && len(dict.AddonPrices) > 0 {
			if v, ok := dict.AddonPrices[c]; ok {
				return v
			}
			low := strings.ToLower(c)
			for k, v := range dict.AddonPrices {
				if strings.EqualFold(k, low) || strings.HasPrefix(strings.ToLower(k), low+"-") {
					return v
				}
			}
		}
		return 0
	}

	var defMem, defSto string
	if planEntry != nil {
		defMem = planEntry.DefaultMemory
		defSto = planEntry.DefaultStorage
	}
	memDelta := lookupAddon(memory, defMem)
	stoDelta := lookupAddon(storage, defSto)
	return roundCents(basePrice + memDelta + stoDelta)
}

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}

// ResolvePlan resolves friendly name, CPU, range, price, and catalog source for a planCode.
func (s *Service) ResolvePlan(ctx context.Context, planCode, serverCode, endpoint, zone string) PlanMeta {
	dict, _ := s.GetDictionary(ctx, endpoint, zone, false)
	return ResolvePlanWithDict(planCode, serverCode, dict)
}

// ResolvePlanWithDict resolves plan metadata using a pre-fetched CatalogDictionary.
func ResolvePlanWithDict(planCode, serverCode string, dict *CatalogDictionary) PlanMeta {
	baseCode, regionInfo := ParsePlanRegion(planCode)
	noVer := StripPlanVersion(planCode)
	cleanBase := reTrailingVer.ReplaceAllString(strings.ToLower(noVer), "$1")

	if dict != nil {
		var match *CatalogPlanEntry
		for _, candidate := range []string{planCode, baseCode} {
			if entry, ok := lookupPlanCaseInsensitive(dict.RealPlans, candidate); ok {
				copyEntry := entry
				match = &copyEntry
				break
			}
		}
		if match == nil && len(dict.Plans) > 0 {
			for _, candidate := range []string{planCode, serverCode, baseCode, noVer, cleanBase} {
				if entry, ok := lookupPlanCaseInsensitive(dict.Plans, candidate); ok {
					copyEntry := entry
					match = &copyEntry
					break
				}
			}
		}
		if match != nil {
			name := match.Name
			if name == "" {
				name = planCode
			}
			rng := match.Range
			if rng == "" {
				rng = ClassifyProductRange(name, "", planCode)
			}
			curr := match.Currency
			if curr == "" {
				curr = dict.Currency
			}
			if curr == "" {
				curr = "EUR"
			}
			return PlanMeta{
				PlanCode:      planCode,
				BaseCode:      nonEmpty(baseCode, planCode),
				Name:          name,
				Range:         rng,
				Generation:    InferPlanGeneration(planCode),
				CPU:           match.CPU,
				Region:        regionInfo,
				Price:         match.Price,
				Currency:      curr,
				AddonPrices:   match.AddonPrices,
				CatalogSource: match.CatalogSource,
			}
		}
	}
	return InferPlanFromCode(planCode)
}

// IsPlanListedInDict reports whether planCode is present as a real bare-metal plan in dict.RealPlans
// (matching only exact planCode or regional baseCode, never product or stripped-version aliases).
func IsPlanListedInDict(planCode string, dict *CatalogDictionary) bool {
	if dict == nil {
		return false
	}
	pc := strings.TrimSpace(planCode)
	if pc == "" {
		return false
	}
	baseCode, _ := ParsePlanRegion(pc)
	if len(dict.RealPlans) > 0 {
		if _, ok := lookupPlanCaseInsensitive(dict.RealPlans, pc); ok {
			return true
		}
		if baseCode != "" {
			if _, ok := lookupPlanCaseInsensitive(dict.RealPlans, baseCode); ok {
				return true
			}
		}
		return false
	}
	if len(dict.Plans) == 0 {
		return false
	}
	if entry, ok := lookupPlanCaseInsensitive(dict.Plans, pc); ok && !strings.EqualFold(entry.CatalogSource, "vps") {
		return true
	}
	return false
}

// GetPlans aliases ListServers for callers referencing GetPlans.
func (s *Service) GetPlans(ctx context.Context, opts ListServersOptions) (*ServerListResponse, error) {
	return s.ListServers(ctx, opts)
}

// ListServers fetches/returns the enriched bare-metal server list with TTL + disk caching and singleflight dedup.
func (s *Service) ListServers(ctx context.Context, opts ListServersOptions) (*ServerListResponse, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, opts.AccountID, opts.Endpoint, opts.Zone)
	cacheKey := ep + ":" + zn
	diskPath := s.serversCachePath(ep, zn)
	now := time.Now().Unix()
	ttlSec := int64(ServerListTTL.Seconds())

	if !opts.ForceRefresh {
		s.mu.RLock()
		mem := s.serversCache[cacheKey]
		s.mu.RUnlock()
		if mem != nil && len(mem.Servers) > 0 && (now-mem.UpdatedAt) < ttlSec {
			return buildServerListResp(mem.Servers, mem.UpdatedAt, true, false, ttlSec, now), nil
		}
		if disk, ok := readJSONFile[cachedServersEntry](diskPath); ok && len(disk.Servers) > 0 {
			if (now - disk.UpdatedAt) < ttlSec {
				s.mu.Lock()
				s.serversCache[cacheKey] = &disk
				s.mu.Unlock()
				return buildServerListResp(disk.Servers, disk.UpdatedAt, true, false, ttlSec, now), nil
			}
		}
	}

	flightKey := fmt.Sprintf("servers:%s:%v", cacheKey, opts.ForceRefresh)
	val, err := s.flights.do(flightKey, func() (any, error) {
		servers, buildErr := s.buildServerPlans(ctx, ep, zn)
		if buildErr == nil && len(servers) > 0 {
			entry := &cachedServersEntry{
				UpdatedAt: time.Now().Unix(),
				Servers:   servers,
			}
			_ = writeJSONFile(diskPath, entry)
			s.mu.Lock()
			s.serversCache[cacheKey] = entry
			s.mu.Unlock()
			if s.log != nil {
				s.log.Info(logger.SourceCatalog, "Loaded bare-metal server catalog", "endpoint", ep, "zone", zn, "servers", len(servers))
			}
			return buildServerListResp(entry.Servers, entry.UpdatedAt, false, false, ttlSec, time.Now().Unix()), nil
		}

		// Fallback to stale memory or disk cache on upstream outage.
		s.mu.RLock()
		staleMem := s.serversCache[cacheKey]
		s.mu.RUnlock()
		if staleMem != nil && len(staleMem.Servers) > 0 {
			if s.log != nil {
				s.log.Warn(logger.SourceCatalog, "Serving stale in-memory server catalog after upstream failure", "endpoint", ep, "zone", zn)
			}
			return buildServerListResp(staleMem.Servers, staleMem.UpdatedAt, false, true, ttlSec, time.Now().Unix()), nil
		}
		if staleDisk, ok := readJSONFile[cachedServersEntry](diskPath); ok && len(staleDisk.Servers) > 0 {
			s.mu.Lock()
			s.serversCache[cacheKey] = &staleDisk
			s.mu.Unlock()
			if s.log != nil {
				s.log.Warn(logger.SourceCatalog, "Serving stale disk server catalog after upstream failure", "endpoint", ep, "zone", zn)
			}
			return buildServerListResp(staleDisk.Servers, staleDisk.UpdatedAt, false, true, ttlSec, time.Now().Unix()), nil
		}
		if buildErr != nil {
			return nil, buildErr
		}
		return nil, errors.New("no server plans returned from OVH public catalog")
	})
	if err != nil {
		return nil, err
	}
	return val.(*ServerListResponse), nil
}

// RefreshCatalog forces a fresh fetch of both the catalog dictionary and enriched server list.
func (s *Service) RefreshCatalog(ctx context.Context, endpoint, zone string) (*ServerListResponse, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	return s.ListServers(ctx, ListServersOptions{
		Endpoint:     ep,
		Zone:         zn,
		ForceRefresh: true,
	})
}

func (s *Service) buildServerPlans(ctx context.Context, endpoint, zone string) ([]ServerPlan, error) {
	var wg sync.WaitGroup
	var eco, bm, vps map[string]any
	var availItems []RawAvailabilityItem
	var availErr error

	wg.Add(4)
	go func() {
		defer wg.Done()
		eco = s.fetchPublicCatalog(ctx, endpoint, zone, "eco")
	}()
	go func() {
		defer wg.Done()
		bm = s.fetchPublicCatalog(ctx, endpoint, zone, "baremetalServers")
	}()
	go func() {
		defer wg.Done()
		vps = s.fetchPublicCatalog(ctx, endpoint, zone, "vps")
		if vps != nil {
			entry := &cachedVPSCatalog{UpdatedAt: time.Now().Unix(), Data: vps}
			_ = writeJSONFile(s.vpsCachePath(endpoint, zone), entry)
			s.mu.Lock()
			s.vpsCache[endpoint+":"+zone] = entry
			s.mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		availItems, availErr = s.GetRawAvailabilities(ctx, endpoint, "")
	}()
	wg.Wait()

	if eco == nil && bm == nil {
		if availErr != nil {
			return nil, fmt.Errorf("failed to fetch OVH public catalogs and availabilities: %w", availErr)
		}
		return nil, errors.New("failed to fetch OVH public catalogs")
	}

	dict := BuildDictionaryFromCatalogs(eco, bm, vps)
	_ = writeJSONFile(s.dictCachePath(endpoint, zone), dict)
	s.mu.Lock()
	s.dictCache[endpoint+":"+zone] = dict
	s.mu.Unlock()

	// Index datacenter availabilities and raw items by planCode.
	availByPlan := make(map[string]map[string]string)
	itemsByPlan := make(map[string][]RawAvailabilityItem)
	firstAvailByPlan := make(map[string]RawAvailabilityItem)
	for _, item := range availItems {
		pc := strings.TrimSpace(item.PlanCode)
		if pc == "" {
			continue
		}
		itemsByPlan[strings.ToLower(pc)] = append(itemsByPlan[strings.ToLower(pc)], item)
		if _, exists := firstAvailByPlan[pc]; !exists {
			firstAvailByPlan[pc] = item
		}
		dcMap, ok := availByPlan[pc]
		if !ok {
			dcMap = make(map[string]string)
			availByPlan[pc] = dcMap
		}
		for _, dc := range item.Datacenters {
			displayDC := NormalizeDisplayDatacenter(dc.Datacenter)
			if displayDC == "" {
				continue
			}
			cur := dcMap[displayDC]
			dcMap[displayDC] = pickBetterAvailability(cur, dc.Availability)
		}
	}

	seen := make(map[string]struct{})
	seenLow := make(map[string]struct{})
	out := make([]ServerPlan, 0, 128)

	type catPair struct {
		source string
		data   map[string]any
	}
	for _, cat := range []catPair{{source: "eco", data: eco}, {source: "baremetalServers", data: bm}} {
		if cat.data == nil {
			continue
		}
		for _, plan := range sliceOfMaps(cat.data["plans"]) {
			pc := strings.TrimSpace(strFromMap(plan, "planCode"))
			if pc == "" {
				continue
			}
			if _, isReal := lookupPlanCaseInsensitive(dict.RealPlans, pc); !isReal {
				continue
			}
			if _, dup := seen[pc]; dup {
				continue
			}
			seen[pc] = struct{}{}
			seenLow[strings.ToLower(pc)] = struct{}{}

			meta := ResolvePlanWithDict(pc, "", dict)
			planEntry, _ := lookupPlanCaseInsensitive(dict.RealPlans, pc)
			sp := buildSingleServerPlan(plan, &planEntry, meta, dict, cat.source, availByPlan[pc], itemsByPlan[strings.ToLower(pc)], s.resolveListedAt(pc))
			out = append(out, sp)
		}
	}

	// Ensure any plan in dict.RealPlans not yet in out is synthesized as "announced".
	for pc, entry := range dict.RealPlans {
		if _, ok := seenLow[strings.ToLower(pc)]; ok {
			continue
		}
		seen[pc] = struct{}{}
		seenLow[strings.ToLower(pc)] = struct{}{}
		meta := ResolvePlanWithDict(pc, "", dict)
		entryCopy := entry
		sp := buildSynthesizedAnnouncedPlan(pc, &entryCopy, meta, dict, s.resolveListedAt(pc))
		out = append(out, sp)
	}

	var presalePlans []ServerPlan
	for pc, dcAvail := range availByPlan {
		if _, ok := seen[pc]; ok {
			continue
		}
		if _, ok := seenLow[strings.ToLower(pc)]; ok {
			continue
		}
		if IsNonServerAddonCode(pc) || IsPlanListedInDict(pc, dict) {
			continue
		}
		firstItem := firstAvailByPlan[pc]
		meta := ResolvePlanWithDict(pc, firstItem.Server, dict)
		if strings.EqualFold(meta.Range, "VPS") {
			continue
		}

		memStr := "N/A"
		if firstItem.Memory != "" {
			parsedMem := ParseMemoryCode(firstItem.Memory, nil)
			if parsedMem.SizeGB > 0 {
				memStr = FormatMemory(parsedMem, "en")
			} else {
				memStr = firstItem.Memory
			}
		}
		stoStr := "N/A"
		if firstItem.Storage != "" {
			parsedSto := ParseStorageCode(firstItem.Storage, false)
			if !parsedSto.None && len(parsedSto.Groups) > 0 {
				stoStr = FormatStorage(parsedSto, "en")
			} else {
				stoStr = firstItem.Storage
			}
		}

		dcs := buildSortedDatacenterAvailabilities(dcAvail)
		defCfgKey := BuildConfigKey(firstItem.Memory, firstItem.Storage)
		cfgs := buildPlanConfigsList(pc, nil, dict, itemsByPlan[strings.ToLower(pc)], nonEmpty(meta.Currency, dict.Currency))
		presalePlans = append(presalePlans, ServerPlan{
			PlanCode:         pc,
			Name:             nonEmpty(meta.Name, pc),
			Description:      "Presale / Unlisted Server Model",
			Range:            meta.Range,
			CPU:              nonEmpty(FormatCPU(meta.CPU, "en"), "TBD"),
			Memory:           memStr,
			Storage:          stoStr,
			Bandwidth:        "N/A",
			VRackBandwidth:   "N/A",
			MonthlyPrice:     nil,
			StartingPrice:    0,
			Currency:         nonEmpty(meta.Currency, dict.Currency),
			DefaultConfigKey: defCfgKey,
			CatalogSource:    "presale",
			ListingState:     "presale",
			ListedAt:         s.resolveListedAt(pc),
			Orderable:        false,
			Generation:       InferPlanGeneration(pc),
			Datacenters:      dcs,
			DefaultOptions:   []ServerOption{},
			AvailableOptions: []ServerOption{},
			Configs:          cfgs,
		})
	}
	slices.SortFunc(presalePlans, func(a, b ServerPlan) int {
		return strings.Compare(a.PlanCode, b.PlanCode)
	})
	out = append(out, presalePlans...)

	return out, nil
}

func buildSortedDatacenterAvailabilities(dcAvail map[string]string) []DatacenterAvailability {
	dcs := make([]DatacenterAvailability, 0, len(dcAvail))
	for dc, avail := range dcAvail {
		name, region, cc := DatacenterMeta(dc)
		if avail == "" {
			avail = "unknown"
		}
		dcs = append(dcs, DatacenterAvailability{
			Datacenter:   dc,
			DCName:       name,
			Region:       region,
			Availability: avail,
			CountryCode:  cc,
			Flag:         DatacenterFlag(dc),
		})
	}
	slices.SortFunc(dcs, func(a, b DatacenterAvailability) int {
		return strings.Compare(a.Datacenter, b.Datacenter)
	})
	return dcs
}

func buildSingleServerPlan(
	plan map[string]any,
	planEntry *CatalogPlanEntry,
	meta PlanMeta,
	dict *CatalogDictionary,
	catalogSource string,
	dcAvail map[string]string,
	planAvailItems []RawAvailabilityItem,
	listedAt string,
) ServerPlan {
	planCode, _ := plan["planCode"].(string)
	invoiceName, _ := plan["invoiceName"].(string)
	desc, _ := plan["description"].(string)
	if desc == "" {
		desc = invoiceName
	}

	cpuStr := FormatCPU(meta.CPU, "en")
	if cpuStr == "" {
		parts := splitPipeNonEmpty(invoiceName)
		if len(parts) > 1 {
			cpuStr = parts[1]
		}
	}
	if cpuStr == "" {
		cpuStr = "N/A"
	}

	memStr := "N/A"
	stoStr := "N/A"
	bwStr := "N/A"
	vrackStr := "N/A"
	defaultOpts := make([]ServerOption, 0)
	availOpts := make([]ServerOption, 0)
	defConfigKey := "default"

	if planEntry != nil {
		defaultOpts = append(defaultOpts, planEntry.DefaultOptions...)
		availOpts = append(availOpts, planEntry.AvailableOptions...)
		if planEntry.DefaultConfigKey != "" {
			defConfigKey = planEntry.DefaultConfigKey
		}
		if planEntry.DefaultMemory != "" {
			parsed := ParseMemoryCode(planEntry.DefaultMemory, nil)
			if parsed.SizeGB > 0 {
				memStr = FormatMemory(parsed, "en")
			} else {
				memStr = planEntry.DefaultMemory
			}
		}
		if planEntry.DefaultStorage != "" {
			parsed := ParseStorageCode(planEntry.DefaultStorage, false)
			if !parsed.None && len(parsed.Groups) > 0 {
				stoStr = FormatStorage(parsedStoOrCode(planEntry.DefaultStorage), "en")
			} else {
				stoStr = planEntry.DefaultStorage
			}
		}
		if planEntry.DefaultSpecs.Bandwidth != "" {
			bwStr = planEntry.DefaultSpecs.Bandwidth
		}
		if planEntry.DefaultSpecs.VRackBandwidth != "" {
			vrackStr = planEntry.DefaultSpecs.VRackBandwidth
		}
	}

	dcs := buildSortedDatacenterAvailabilities(dcAvail)
	displayName := meta.Name
	if displayName == "" {
		displayName = nonEmpty(invoiceName, planCode)
	}

	startingPrice := 0.0
	if meta.Price != nil {
		startingPrice = *meta.Price
	}
	listingState := "listed"
	orderable := true
	if len(planAvailItems) == 0 && len(dcAvail) == 0 {
		listingState = "announced"
		orderable = false
	}

	currency := nonEmpty(meta.Currency, "EUR")
	cfgs := buildPlanConfigsList(planCode, planEntry, dict, planAvailItems, currency)
	if defConfigKey == "default" && len(cfgs) > 0 {
		defConfigKey = cfgs[0].ConfigKey
	}

	return ServerPlan{
		PlanCode:         planCode,
		Name:             displayName,
		Description:      desc,
		Range:            meta.Range,
		CPU:              cpuStr,
		Memory:           memStr,
		Storage:          stoStr,
		Bandwidth:        bwStr,
		VRackBandwidth:   vrackStr,
		MonthlyPrice:     meta.Price,
		StartingPrice:    startingPrice,
		Price:            startingPrice,
		Currency:         currency,
		DefaultConfigKey: defConfigKey,
		CatalogSource:    catalogSource,
		ListingState:     listingState,
		ListedAt:         listedAt,
		Orderable:        orderable,
		Generation:       InferPlanGeneration(planCode),
		Datacenters:      dcs,
		DefaultOptions:   defaultOpts,
		AvailableOptions: availOpts,
		Configs:          cfgs,
	}
}

func parsedStoOrCode(code string) ParsedStorage {
	return ParseStorageCode(code, false)
}

func buildSynthesizedAnnouncedPlan(
	planCode string,
	entry *CatalogPlanEntry,
	meta PlanMeta,
	dict *CatalogDictionary,
	listedAt string,
) ServerPlan {
	cpuStr := FormatCPU(meta.CPU, "en")
	if cpuStr == "" {
		cpuStr = "N/A"
	}
	memStr := "N/A"
	stoStr := "N/A"
	bwStr := "N/A"
	vrackStr := "N/A"
	defCfgKey := "default"
	var defaultOpts, availOpts []ServerOption
	if entry != nil {
		defaultOpts = append([]ServerOption(nil), entry.DefaultOptions...)
		availOpts = append([]ServerOption(nil), entry.AvailableOptions...)
		if entry.DefaultConfigKey != "" {
			defCfgKey = entry.DefaultConfigKey
		}
		if entry.DefaultMemory != "" {
			parsed := ParseMemoryCode(entry.DefaultMemory, nil)
			if parsed.SizeGB > 0 {
				memStr = FormatMemory(parsed, "en")
			} else {
				memStr = entry.DefaultMemory
			}
		}
		if entry.DefaultStorage != "" {
			parsed := ParseStorageCode(entry.DefaultStorage, false)
			if !parsed.None && len(parsed.Groups) > 0 {
				stoStr = FormatStorage(parsed, "en")
			} else {
				stoStr = entry.DefaultStorage
			}
		}
		if entry.DefaultSpecs.Bandwidth != "" {
			bwStr = entry.DefaultSpecs.Bandwidth
		}
		if entry.DefaultSpecs.VRackBandwidth != "" {
			vrackStr = entry.DefaultSpecs.VRackBandwidth
		}
	}
	startingPrice := 0.0
	if meta.Price != nil {
		startingPrice = *meta.Price
	}
	currency := nonEmpty(meta.Currency, "EUR")
	cfgs := buildPlanConfigsList(planCode, entry, dict, nil, currency)
	return ServerPlan{
		PlanCode:         planCode,
		Name:             nonEmpty(meta.Name, planCode),
		Description:      nonEmpty(entry.Description, meta.Name),
		Range:            meta.Range,
		CPU:              cpuStr,
		Memory:           memStr,
		Storage:          stoStr,
		Bandwidth:        bwStr,
		VRackBandwidth:   vrackStr,
		MonthlyPrice:     meta.Price,
		StartingPrice:    startingPrice,
		Price:            startingPrice,
		Currency:         currency,
		DefaultConfigKey: defCfgKey,
		CatalogSource:    nonEmpty(entry.CatalogSource, "eco"),
		ListingState:     "announced",
		ListedAt:         listedAt,
		Orderable:        false,
		Generation:       InferPlanGeneration(planCode),
		Datacenters:      []DatacenterAvailability{},
		DefaultOptions:    nonNilOptions(defaultOpts),
		AvailableOptions: nonNilOptions(availOpts),
		Configs:          cfgs,
	}
}

func nonNilOptions(in []ServerOption) []ServerOption {
	if in == nil {
		return []ServerOption{}
	}
	return in
}

// GetCatalogAvailabilities returns enriched availability rows for GET /api/availability and GET /api/catalog/availability,
// including configKey, options, isDefault, estimatedMonthlyPrice, listingState ("listed", "announced", "presale"),
// and synthesized "announced" entries for real catalog plans with 0 availability rows.
func (s *Service) GetCatalogAvailabilities(ctx context.Context, endpoint, zone, planCode string) ([]AvailabilityItem, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	filterPC := strings.TrimSpace(planCode)
	rawItems, err := s.GetRawAvailabilities(ctx, ep, filterPC)
	if err != nil {
		return nil, err
	}
	dict, _ := s.GetDictionary(ctx, ep, zn, false)
	hasRealPlans := dict != nil && len(dict.RealPlans) > 0

	seenExact := make(map[string]bool)
	seenBase := make(map[string]bool)
	firstSeenCfgForPlan := make(map[string]bool)
	out := make([]AvailabilityItem, 0, len(rawItems)+16)

	for _, item := range rawItems {
		pc := strings.TrimSpace(item.PlanCode)
		if pc == "" {
			continue
		}
		if filterPC != "" && !strings.EqualFold(pc, filterPC) {
			continue
		}
		seenExact[strings.ToLower(pc)] = true
		if base, _ := ParsePlanRegion(pc); base != "" {
			seenBase[strings.ToLower(base)] = true
		}

		var planEntry *CatalogPlanEntry
		if dict != nil {
			if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, pc); ok {
				copyE := e
				planEntry = &copyE
			} else if base, _ := ParsePlanRegion(pc); base != "" {
				if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, base); ok {
					copyE := e
					planEntry = &copyE
				}
			}
			if planEntry == nil {
				if e, ok := lookupPlanCaseInsensitive(dict.Plans, pc); ok {
					copyE := e
					planEntry = &copyE
				}
			}
		}

		isListed := !hasRealPlans || IsPlanListedInDict(pc, dict)
		listingState := "listed"
		orderable := true
		catSource := ""
		if planEntry != nil {
			catSource = planEntry.CatalogSource
		}
		if !isListed {
			listingState = "presale"
			orderable = false
			catSource = "presale"
		}

		cfgKey := BuildConfigKey(item.Memory, item.Storage)
		isDef := isDefaultPlanConfig(planEntry, item.Memory, item.Storage, cfgKey)
		if !isDef && (planEntry == nil || planEntry.DefaultConfigKey == "" || planEntry.DefaultConfigKey == "default") {
			if !firstSeenCfgForPlan[strings.ToLower(pc)] {
				isDef = true
			}
		}
		firstSeenCfgForPlan[strings.ToLower(pc)] = true

		opts := matchOptionsFromPlanEntry(planEntry, item.Memory, item.Storage)
		if isDef && len(opts) == 0 && planEntry != nil && len(planEntry.DefaultAddonOptions) > 0 {
			opts = append([]string(nil), planEntry.DefaultAddonOptions...)
		}
		if opts == nil {
			opts = []string{}
		}

		basePrice := 0.0
		currency := "EUR"
		if dict != nil && dict.Currency != "" {
			currency = dict.Currency
		}
		if planEntry != nil {
			if planEntry.Price != nil {
				basePrice = *planEntry.Price
			}
			if planEntry.Currency != "" {
				currency = planEntry.Currency
			}
		}
		estPrice := EstimateConfigMonthlyPrice(basePrice, item.Memory, item.Storage, planEntry, dict)
		dcs := item.Datacenters
		if dcs == nil {
			dcs = []RawAvailabilityDC{}
		}

		out = append(out, AvailabilityItem{
			FQN:                   item.FQN,
			Memory:                item.Memory,
			PlanCode:              pc,
			Server:                item.Server,
			Storage:               item.Storage,
			SystemStorage:         item.SystemStorage,
			GPU:                   item.GPU,
			Datacenters:           dcs,
			ConfigKey:             cfgKey,
			Options:               opts,
			IsDefault:             isDef,
			EstimatedMonthlyPrice: estPrice,
			Price:                 estPrice,
			Currency:              currency,
			ListingState:          listingState,
			CatalogSource:         catSource,
			Orderable:             orderable,
			Generation:            InferPlanGeneration(pc),
			ListedAt:              s.resolveListedAt(pc),
		})
	}

	if hasRealPlans {
		var announcedCodes []string
		for pc := range dict.RealPlans {
			if filterPC != "" && !strings.EqualFold(pc, filterPC) {
				continue
			}
			lowPC := strings.ToLower(pc)
			base, _ := ParsePlanRegion(pc)
			lowBase := strings.ToLower(base)
			if seenExact[lowPC] || (lowBase != "" && seenBase[lowBase]) {
				continue
			}
			announcedCodes = append(announcedCodes, pc)
		}
		slices.Sort(announcedCodes)
		for _, pc := range announcedCodes {
			entry := dict.RealPlans[pc]
			entryCopy := entry
			defMem := entry.DefaultMemory
			if defMem == "" {
				defMem = entry.DefaultSpecs.Memory
			}
			defSto := entry.DefaultStorage
			if defSto == "" {
				defSto = entry.DefaultSpecs.Storage
			}
			cfgKey := nonEmpty(entry.DefaultConfigKey, BuildConfigKey(defMem, defSto))
			basePrice := 0.0
			if entry.Price != nil {
				basePrice = *entry.Price
			}
			estPrice := EstimateConfigMonthlyPrice(basePrice, defMem, defSto, &entryCopy, dict)
			currency := nonEmpty(entry.Currency, nonEmpty(dict.Currency, "EUR"))
			opts := append([]string(nil), entry.DefaultAddonOptions...)
			if opts == nil {
				opts = []string{}
			}
			fqn := pc
			if defMem != "" || defSto != "" {
				fqn = fmt.Sprintf("%s.%s.%s", pc, defMem, defSto)
			}
			out = append(out, AvailabilityItem{
				FQN:                   fqn,
				Memory:                defMem,
				PlanCode:              pc,
				Server:                nonEmpty(entry.Product, pc),
				Storage:               defSto,
				SystemStorage:         entry.DefaultSpecs.SystemStorage,
				GPU:                   entry.DefaultSpecs.GPU,
				Datacenters:           []RawAvailabilityDC{},
				ConfigKey:             cfgKey,
				Options:               opts,
				IsDefault:             true,
				EstimatedMonthlyPrice: estPrice,
				Price:                 estPrice,
				Currency:              currency,
				ListingState:          "announced",
				CatalogSource:         entry.CatalogSource,
				Orderable:             false,
				Generation:            InferPlanGeneration(pc),
				ListedAt:              s.resolveListedAt(pc),
			})
		}
	}

	return out, nil
}

// GetPlanConfigs returns the merged configuration list and pricing for GET /api/catalog/plans/{planCode}/configs.
func (s *Service) GetPlanConfigs(ctx context.Context, endpoint, zone, planCode string) (*PlanConfigsResponse, error) {
	pc := strings.TrimSpace(planCode)
	if pc == "" {
		return nil, errors.New("planCode is required")
	}
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	dict, _ := s.GetDictionary(ctx, ep, zn, false)
	rawItems, _ := s.GetRawAvailabilities(ctx, ep, pc)

	var filteredAvail []RawAvailabilityItem
	for _, it := range rawItems {
		if strings.EqualFold(strings.TrimSpace(it.PlanCode), pc) {
			filteredAvail = append(filteredAvail, it)
		}
	}

	var planEntry *CatalogPlanEntry
	inRealPlans := false
	if dict != nil {
		if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, pc); ok {
			copyE := e
			planEntry = &copyE
			inRealPlans = true
		} else if base, _ := ParsePlanRegion(pc); base != "" {
			if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, base); ok {
				copyE := e
				planEntry = &copyE
				inRealPlans = true
			}
		}
		if planEntry == nil {
			if e, ok := lookupPlanCaseInsensitive(dict.Plans, pc); ok && !strings.EqualFold(e.CatalogSource, "vps") {
				copyE := e
				planEntry = &copyE
				if len(dict.RealPlans) == 0 {
					inRealPlans = true
				}
			}
		}
	}

	var firstServer string
	if len(filteredAvail) > 0 {
		firstServer = filteredAvail[0].Server
	}
	meta := ResolvePlanWithDict(pc, firstServer, dict)

	listingState := "presale"
	orderable := false
	if inRealPlans {
		if len(filteredAvail) > 0 {
			listingState = "listed"
			orderable = true
		} else {
			listingState = "announced"
			orderable = false
		}
	}

	basePrice := 0.0
	if meta.Price != nil {
		basePrice = *meta.Price
	}
	currency := nonEmpty(meta.Currency, "EUR")
	if dict != nil && dict.Currency != "" && meta.Currency == "" {
		currency = dict.Currency
	}

	configs := buildPlanConfigsList(pc, planEntry, dict, filteredAvail, currency)
	defCfgKey := "default"
	if planEntry != nil && planEntry.DefaultConfigKey != "" {
		defCfgKey = planEntry.DefaultConfigKey
	}
	for _, c := range configs {
		if c.IsDefault {
			defCfgKey = c.ConfigKey
			break
		}
	}
	startingPrice := basePrice
	if startingPrice == 0 && len(configs) > 0 {
		startingPrice = configs[0].EstimatedMonthlyPrice
	}

	return &PlanConfigsResponse{
		PlanCode:         pc,
		Name:             nonEmpty(meta.Name, pc),
		Range:            meta.Range,
		CPU:              FormatCPU(meta.CPU, "en"),
		ListingState:     listingState,
		Orderable:        orderable,
		BasePrice:        basePrice,
		StartingPrice:    startingPrice,
		Currency:         currency,
		DefaultConfigKey: defCfgKey,
		Configs:          configs,
	}, nil
}

func buildPlanConfigsList(
	planCode string,
	planEntry *CatalogPlanEntry,
	dict *CatalogDictionary,
	availItems []RawAvailabilityItem,
	currency string,
) []PlanConfigItem {
	basePrice := 0.0
	if planEntry != nil && planEntry.Price != nil {
		basePrice = *planEntry.Price
	}
	if currency == "" {
		currency = "EUR"
	}

	byKey := make(map[string]*PlanConfigItem)
	order := make([]string, 0, len(availItems)+1)
	sawDefault := false

	for _, it := range availItems {
		if planCode != "" && it.PlanCode != "" && !strings.EqualFold(it.PlanCode, planCode) {
			continue
		}
		mem := strings.TrimSpace(it.Memory)
		sto := strings.TrimSpace(it.Storage)
		cfgKey := BuildConfigKey(mem, sto)
		isDef := isDefaultPlanConfig(planEntry, mem, sto, cfgKey)
		if isDef {
			sawDefault = true
		}

		existing, ok := byKey[cfgKey]
		if !ok {
			opts := matchOptionsFromPlanEntry(planEntry, mem, sto)
			if isDef && len(opts) == 0 && planEntry != nil && len(planEntry.DefaultAddonOptions) > 0 {
				opts = append([]string(nil), planEntry.DefaultAddonOptions...)
			}
			if opts == nil {
				opts = []string{}
			}
			memLabel := FormatMemory(ParseMemoryCode(mem, nil), "en")
			stoLabel := FormatStorage(ParseStorageCode(sto, false), "en")
			lbl := joinNonEmpty(" + ", memLabel, stoLabel)
			if lbl == "" {
				lbl = cfgKey
			}
			estPrice := EstimateConfigMonthlyPrice(basePrice, mem, sto, planEntry, dict)
			existing = &PlanConfigItem{
				ConfigKey:             cfgKey,
				Memory:                mem,
				Storage:               sto,
				SystemStorage:         it.SystemStorage,
				GPU:                   it.GPU,
				MemoryLabel:           memLabel,
				StorageLabel:          stoLabel,
				Label:                 lbl,
				Options:               opts,
				IsDefault:             isDef,
				EstimatedMonthlyPrice: estPrice,
				Price:                 estPrice,
				Currency:              currency,
				Datacenters:           make(map[string]string),
			}
			byKey[cfgKey] = existing
			order = append(order, cfgKey)
		} else if isDef {
			existing.IsDefault = true
		}

		for _, dc := range it.Datacenters {
			normDC := NormalizeDisplayDatacenter(dc.Datacenter)
			if normDC == "" {
				continue
			}
			av := strings.TrimSpace(dc.Availability)
			if av == "" {
				av = "unknown"
			}
			existing.Datacenters[normDC] = pickBetterAvailability(existing.Datacenters[normDC], av)
		}
	}

	// Merge the plan's default configuration if it wasn't present in availItems.
	if planEntry != nil && !sawDefault {
		defMem := nonEmpty(planEntry.DefaultMemory, planEntry.DefaultSpecs.Memory)
		defSto := nonEmpty(planEntry.DefaultStorage, planEntry.DefaultSpecs.Storage)
		if defMem != "" || defSto != "" || basePrice > 0 {
			defKey := nonEmpty(planEntry.DefaultConfigKey, BuildConfigKey(defMem, defSto))
			if existing, ok := byKey[defKey]; ok {
				existing.IsDefault = true
				sawDefault = true
			} else {
				memLabel := FormatMemory(ParseMemoryCode(defMem, nil), "en")
				stoLabel := FormatStorage(ParseStorageCode(defSto, false), "en")
				lbl := joinNonEmpty(" + ", memLabel, stoLabel)
				if lbl == "" {
					lbl = "Default Configuration"
				}
				opts := append([]string(nil), planEntry.DefaultAddonOptions...)
				if len(opts) == 0 {
					opts = matchOptionsFromPlanEntry(planEntry, defMem, defSto)
				}
				if opts == nil {
					opts = []string{}
				}
				estPrice := EstimateConfigMonthlyPrice(basePrice, defMem, defSto, planEntry, dict)
				byKey[defKey] = &PlanConfigItem{
					ConfigKey:             defKey,
					Memory:                defMem,
					Storage:               defSto,
					SystemStorage:         planEntry.DefaultSpecs.SystemStorage,
					GPU:                   planEntry.DefaultSpecs.GPU,
					MemoryLabel:           memLabel,
					StorageLabel:          stoLabel,
					Label:                 lbl,
					Options:               opts,
					IsDefault:             true,
					EstimatedMonthlyPrice: estPrice,
					Price:                 estPrice,
					Currency:              currency,
					Datacenters:           make(map[string]string),
				}
				order = append([]string{defKey}, order...)
				sawDefault = true
			}
		}
	}

	out := make([]PlanConfigItem, 0, len(order))
	for _, k := range order {
		item := byKey[k]
		inStock := 0
		for _, av := range item.Datacenters {
			if av != "" && !strings.EqualFold(av, "unavailable") && !strings.EqualFold(av, "unknown") {
				inStock++
			}
		}
		item.InStockCount = inStock
		out = append(out, *item)
	}
	if !sawDefault && len(out) > 0 {
		out[0].IsDefault = true
	}

	slices.SortStableFunc(out, func(a, b PlanConfigItem) int {
		if a.IsDefault != b.IsDefault {
			if a.IsDefault {
				return -1
			}
			return 1
		}
		if a.EstimatedMonthlyPrice != b.EstimatedMonthlyPrice {
			if a.EstimatedMonthlyPrice < b.EstimatedMonthlyPrice {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ConfigKey, b.ConfigKey)
	})
	return out
}

func isDefaultPlanConfig(planEntry *CatalogPlanEntry, memory, storage, cfgKey string) bool {
	if planEntry == nil {
		return false
	}
	defKey := planEntry.DefaultConfigKey
	if defKey != "" && defKey != "default" && strings.EqualFold(cfgKey, defKey) {
		return true
	}
	defMem := nonEmpty(planEntry.DefaultMemory, planEntry.DefaultSpecs.Memory)
	defSto := nonEmpty(planEntry.DefaultStorage, planEntry.DefaultSpecs.Storage)
	if defMem == "" && defSto == "" {
		return false
	}
	memMatch := defMem == "" || strings.EqualFold(memory, defMem) || StandardizeConfig(memory) == StandardizeConfig(defMem)
	stoMatch := defSto == "" || strings.EqualFold(storage, defSto) || StandardizeConfig(storage) == StandardizeConfig(defSto)
	return memMatch && stoMatch
}

func matchOptionsFromPlanEntry(planEntry *CatalogPlanEntry, memoryCode, storageCode string) []string {
	if planEntry == nil || len(planEntry.AvailableOptions) == 0 {
		return nil
	}
	var opts []string
	findBestOption := func(code string, isMem bool) string {
		c := strings.TrimSpace(code)
		if c == "" || strings.EqualFold(c, "N/A") || strings.EqualFold(c, "none") {
			return ""
		}
		low := strings.ToLower(c)
		// 1. Exact ConfigKey or Value match
		for _, opt := range planEntry.AvailableOptions {
			fam := strings.ToLower(opt.Family)
			if isMem && !strings.Contains(fam, "memory") && !strings.Contains(fam, "ram") && !strings.HasPrefix(strings.ToLower(opt.Value), "ram-") {
				continue
			}
			if !isMem && !strings.Contains(fam, "storage") && !strings.Contains(fam, "disk") && !strings.Contains(fam, "drive") {
				continue
			}
			if strings.EqualFold(opt.ConfigKey, low) || strings.EqualFold(opt.Value, low) {
				return opt.Value
			}
		}
		// 2. Prefix match (e.g. "ram-32g-ecc-2933" -> "ram-32g-ecc-2933-24ska01")
		for _, opt := range planEntry.AvailableOptions {
			if strings.HasPrefix(strings.ToLower(opt.Value), low+"-") {
				return opt.Value
			}
		}
		// 3. Standardized match
		std := StandardizeConfig(c)
		if std == "" {
			return ""
		}
		for _, opt := range planEntry.AvailableOptions {
			fam := strings.ToLower(opt.Family)
			if isMem && !strings.Contains(fam, "memory") && !strings.Contains(fam, "ram") && !strings.HasPrefix(strings.ToLower(opt.Value), "ram-") {
				continue
			}
			if !isMem && !strings.Contains(fam, "storage") && !strings.Contains(fam, "disk") && !strings.Contains(fam, "drive") {
				continue
			}
			if StandardizeConfig(opt.Value) == std {
				return opt.Value
			}
		}
		return ""
	}

	if memOpt := findBestOption(memoryCode, true); memOpt != "" {
		opts = append(opts, memOpt)
	}
	if stoOpt := findBestOption(storageCode, false); stoOpt != "" {
		if !slices.Contains(opts, stoOpt) {
			opts = append(opts, stoOpt)
		}
	}
	return opts
}

func joinNonEmpty(sep string, parts ...string) string {
	var filtered []string
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" && t != "-" && !strings.EqualFold(t, "N/A") {
			filtered = append(filtered, t)
		}
	}
	return strings.Join(filtered, sep)
}

// GetAvailability returns per-datacenter availability for planCode (optionally filtered by options).
func (s *Service) GetAvailability(ctx context.Context, endpoint, planCode string, options []string) (map[string]string, error) {
	items, err := s.GetRawAvailabilities(ctx, endpoint, planCode)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return map[string]string{}, nil
	}

	var matched *RawAvailabilityItem
	if len(options) > 0 {
		var memOpt, stoOpt string
		for _, opt := range options {
			low := strings.ToLower(strings.TrimSpace(opt))
			if strings.Contains(low, "ram-") || strings.Contains(low, "memory") {
				memOpt = low
			} else if strings.Contains(low, "softraid-") || strings.Contains(low, "hardraid-") ||
				strings.Contains(low, "hybrid") || strings.Contains(low, "noraid-") ||
				strings.Contains(low, "nvme") || strings.Contains(low, "raid") {
				stoOpt = low
			}
		}

		for i := range items {
			it := &items[i]
			memMatch := true
			if memOpt != "" {
				if it.Memory == "" {
					memMatch = false
				} else {
					memMatch = memoryPrefixKey(memOpt) == memoryPrefixKey(strings.ToLower(it.Memory))
				}
			}
			stoMatch := true
			if stoOpt != "" {
				if it.Storage == "" {
					stoMatch = false
				} else {
					lowSto := strings.ToLower(it.Storage)
					stoMatch = strings.HasPrefix(stoOpt, lowSto) || StandardizeConfig(stoOpt) == StandardizeConfig(lowSto)
				}
			}
			if memMatch && stoMatch {
				matched = it
				break
			}
		}
		if matched == nil {
			return map[string]string{}, nil
		}
	} else {
		matched = &items[0]
	}

	res := make(map[string]string, len(matched.Datacenters))
	for _, dc := range matched.Datacenters {
		code := strings.ToLower(strings.TrimSpace(dc.Datacenter))
		if code == "" {
			continue
		}
		av := strings.TrimSpace(dc.Availability)
		if av == "" {
			av = "unknown"
		}
		res[code] = pickBetterAvailability(res[code], av)
		if norm := NormalizeDisplayDatacenter(code); norm != "" && norm != code {
			res[norm] = pickBetterAvailability(res[norm], av)
		}
	}
	return res, nil
}

// GetConfigLevelAvailabilities returns configuration-level availabilities mapped to catalog addon planCodes.
func (s *Service) GetConfigLevelAvailabilities(ctx context.Context, endpoint, zone, planCode string) (map[string]*ConfigAvailability, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, zone)
	items, err := s.GetRawAvailabilities(ctx, ep, planCode)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return map[string]*ConfigAvailability{}, nil
	}

	dict, _ := s.GetDictionary(ctx, ep, zn, false)
	var planEntry *CatalogPlanEntry
	if dict != nil {
		if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, planCode); ok {
			copyE := e
			planEntry = &copyE
		} else if base, _ := ParsePlanRegion(planCode); base != "" {
			if e, ok := lookupPlanCaseInsensitive(dict.RealPlans, base); ok {
				copyE := e
				planEntry = &copyE
			}
		}
	}

	var catalogPlan map[string]any
	if planEntry == nil {
		for _, catName := range []string{"eco", "baremetalServers"} {
			cat := s.fetchPublicCatalog(ctx, ep, zn, catName)
			if cat == nil {
				continue
			}
			for _, p := range sliceOfMaps(cat["plans"]) {
				if strings.EqualFold(strFromMap(p, "planCode"), planCode) {
					catalogPlan = p
					break
				}
			}
			if catalogPlan != nil {
				break
			}
		}
	}

	listingState := "presale"
	orderable := false
	if planEntry != nil || catalogPlan != nil {
		listingState = "listed"
		orderable = true
	}
	gen := InferPlanGeneration(planCode)
	basePrice := 0.0
	currency := "EUR"
	if dict != nil && dict.Currency != "" {
		currency = dict.Currency
	}
	if planEntry != nil {
		if planEntry.Price != nil {
			basePrice = *planEntry.Price
		}
		if planEntry.Currency != "" {
			currency = planEntry.Currency
		}
	}

	out := make(map[string]*ConfigAvailability, len(items))
	for _, item := range items {
		if planCode != "" && item.PlanCode != "" && !strings.EqualFold(item.PlanCode, planCode) {
			continue
		}
		dcs := make(map[string]string, len(item.Datacenters))
		inStock := 0
		for _, dc := range item.Datacenters {
			code := strings.ToLower(strings.TrimSpace(dc.Datacenter))
			if code != "" {
				av := strings.TrimSpace(dc.Availability)
				if av == "" {
					av = "unknown"
				}
				dcs[code] = pickBetterAvailability(dcs[code], av)
				norm := NormalizeDisplayDatacenter(code)
				if norm != "" && norm != code {
					dcs[norm] = pickBetterAvailability(dcs[norm], av)
				}
			}
		}
		for _, av := range dcs {
			if av != "" && !strings.EqualFold(av, "unavailable") && !strings.EqualFold(av, "unknown") {
				inStock++
			}
		}

		matchedOptions := matchOptionsFromPlanEntry(planEntry, item.Memory, item.Storage)
		if len(matchedOptions) == 0 && catalogPlan != nil {
			matchedOptions = matchAddonOptionsForConfig(catalogPlan, item.Memory, item.Storage)
		}
		cfgKey := BuildConfigKey(item.Memory, item.Storage)
		isDef := isDefaultPlanConfig(planEntry, item.Memory, item.Storage, cfgKey)
		if isDef && len(matchedOptions) == 0 && planEntry != nil && len(planEntry.DefaultAddonOptions) > 0 {
			matchedOptions = append([]string(nil), planEntry.DefaultAddonOptions...)
		}
		if matchedOptions == nil {
			matchedOptions = []string{}
		}
		estPrice := EstimateConfigMonthlyPrice(basePrice, item.Memory, item.Storage, planEntry, dict)
		memLabel := FormatMemory(ParseMemoryCode(item.Memory, nil), "en")
		stoLabel := FormatStorage(ParseStorageCode(item.Storage, false), "en")

		key := item.FQN
		if key == "" {
			key = fmt.Sprintf("%s.%s.%s", nonEmpty(item.PlanCode, planCode), item.Memory, item.Storage)
		}
		out[key] = &ConfigAvailability{
			FQN:                   key,
			ConfigKey:             cfgKey,
			Memory:                nonEmpty(item.Memory, "N/A"),
			Storage:               nonEmpty(item.Storage, "N/A"),
			MemoryLabel:           memLabel,
			StorageLabel:          stoLabel,
			Label:                 nonEmpty(joinNonEmpty(" + ", memLabel, stoLabel), cfgKey),
			Datacenters:           dcs,
			InStockCount:          inStock,
			Options:               matchedOptions,
			IsDefault:             isDef,
			EstimatedMonthlyPrice: estPrice,
			Price:                 estPrice,
			Currency:              currency,
			ListingState:          listingState,
			Orderable:             orderable,
			Generation:            gen,
		}
	}
	return out, nil
}

func matchAddonOptionsForConfig(catalogPlan map[string]any, memoryCode, storageCode string) []string {
	if catalogPlan == nil {
		return nil
	}
	memStd := ""
	if memoryCode != "" && memoryCode != "N/A" {
		memStd = StandardizeConfig(memoryCode)
	}
	stoStd := ""
	if storageCode != "" && storageCode != "N/A" {
		stoStd = StandardizeConfig(storageCode)
	}
	if memStd == "" && stoStd == "" {
		return nil
	}

	var opts []string
	for _, fam := range sliceOfMaps(catalogPlan["addonFamilies"]) {
		famName := strings.ToLower(strFromMap(fam, "name"))
		addons := sliceOfStrings(fam["addons"])
		if (famName == "memory" || famName == "ram") && memStd != "" {
			for _, a := range addons {
				if StandardizeConfig(a) == memStd && !slices.Contains(opts, a) {
					opts = append(opts, a)
				}
			}
		}
		if (famName == "storage" || famName == "disk") && stoStd != "" {
			for _, a := range addons {
				if StandardizeConfig(a) == stoStd && !slices.Contains(opts, a) {
					opts = append(opts, a)
				}
			}
		}
	}
	return opts
}

// GetServerPrice quotes the monthly configuration price via a temporary OVH cart and deletes the cart afterwards.
func (s *Service) GetServerPrice(ctx context.Context, accountID, planCode, datacenter string, options []string) (*ServerPriceResult, error) {
	client, acc, err := s.getAccountClient(ctx, accountID)
	if err != nil {
		return &ServerPriceResult{
			Success: false,
			Code:    "OVH_CREDENTIALS_MISSING",
			Error:   "OVH API credentials are not configured",
			Price:   nil,
		}, err
	}

	ep := ovhclient.EndpointEU
	zone := "IE"
	if acc != nil {
		ep = ovhclient.NormalizeEndpoint(acc.Endpoint)
		zone = ovhclient.NormalizeZone(acc.Zone, ep)
	}
	meta := s.ResolvePlan(ctx, planCode, "", ep, zone)

	apiDC := NormalizeAPIDatacenter(datacenter)
	var cartResp struct {
		CartID string `json:"cartId"`
	}
	if err := client.PostWithContext(ctx, "/order/cart", map[string]any{
		"ovhSubsidiary": normalizeSubsidiary(ep, zone),
	}, &cartResp); err != nil {
		return &ServerPriceResult{Success: false, Error: err.Error()}, err
	}
	cartID := cartResp.CartID
	defer func() {
		if cartID != "" {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.DeleteWithContext(cleanupCtx, fmt.Sprintf("/order/cart/%s", url.PathEscape(cartID)), nil)
		}
	}()

	_ = client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/assign", url.PathEscape(cartID)), nil, nil)

	catalogPath, itemID, addErr := AddCartServerItem(ctx, client, cartID, planCode, meta.CatalogSource, 1)
	if addErr != nil {
		return &ServerPriceResult{Success: false, Error: addErr.Error()}, addErr
	}

	configs := map[string]string{
		"dedicated_datacenter": apiDC,
		"dedicated_os":         "none_64.en",
	}
	if reg := InferCartRegion(apiDC); reg != "" {
		configs["region"] = reg
	}
	for k, v := range configs {
		_ = client.PostWithContext(ctx,
			fmt.Sprintf("/order/cart/%s/item/%d/configuration", url.PathEscape(cartID), itemID),
			map[string]any{"label": k, "value": v},
			nil,
		)
	}

	if len(options) > 0 {
		AddCartOptions(ctx, client, cartID, itemID, catalogPath, planCode, options, s.log)
	}

	var cartSummary map[string]any
	if err := client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s/summary", url.PathEscape(cartID)), &cartSummary); err != nil {
		return &ServerPriceResult{Success: false, Error: err.Error()}, err
	}

	var cartInfo map[string]any
	_ = client.GetWithContext(ctx, fmt.Sprintf("/order/cart/%s", url.PathEscape(cartID)), &cartInfo)

	pricePayload := ExtractCartPricePayload(cartSummary, cartInfo)
	return &ServerPriceResult{
		Success:    true,
		PlanCode:   planCode,
		Datacenter: datacenter,
		Options:    options,
		Price:      pricePayload,
	}, nil
}

// AddCartServerItem adds a base server item to an OVH cart using either /eco or /baremetalServers with fallback.
func AddCartServerItem(
	ctx context.Context,
	client *ovh.Client,
	cartID string,
	planCode string,
	preferredSource string,
	quantity int,
) (string, int64, error) {
	if quantity <= 0 {
		quantity = 1
	}
	paths := []string{"baremetalServers", "eco"}
	if preferredSource == "eco" || strings.Contains(strings.ToLower(planCode), "sk") ||
		strings.Contains(strings.ToLower(planCode), "sys") || strings.Contains(strings.ToLower(planCode), "rise") {
		paths = []string{"eco", "baremetalServers"}
	}

	payload := map[string]any{
		"planCode":    planCode,
		"pricingMode": "default",
		"duration":    "P1M",
		"quantity":    quantity,
	}

	var lastErr error
	for _, catPath := range paths {
		var res struct {
			ItemID int64 `json:"itemId"`
		}
		err := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/%s", url.PathEscape(cartID), catPath), payload, &res)
		if err == nil {
			return catPath, res.ItemID, nil
		}
		lastErr = err
		var apiErr *ovh.APIError
		if errors.As(err, &apiErr) && apiErr.Code != http.StatusNotFound && apiErr.Code != http.StatusBadRequest {
			break
		}
	}
	return "", 0, lastErr
}

// AddCartOptions attaches requested hardware addons to a cart item, filtering out non-hardware options.
func AddCartOptions(
	ctx context.Context,
	client *ovh.Client,
	cartID string,
	itemID int64,
	catalogPath string,
	planCode string,
	options []string,
	log *logger.Hub,
) int {
	filtered := make([]string, 0, len(options))
	for _, opt := range options {
		trimmed := strings.TrimSpace(opt)
		if trimmed == "" || isLicenseOrOSOption(trimmed) {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	if len(filtered) == 0 {
		return 0
	}

	var availOpts []map[string]any
	optsURL := fmt.Sprintf("/order/cart/%s/%s/options?planCode=%s", url.PathEscape(cartID), catalogPath, url.QueryEscape(planCode))
	if err := client.GetWithContext(ctx, optsURL, &availOpts); err != nil {
		if log != nil {
			log.Warn(logger.SourceCatalog, "Failed to list cart addon options", "planCode", planCode, "error", err.Error())
		}
		return 0
	}

	added := 0
	for _, wanted := range filtered {
		for _, avail := range availOpts {
			pc := strFromMap(avail, "planCode")
			if pc != wanted {
				continue
			}
			dur := nonEmpty(strFromMap(avail, "duration"), "P1M")
			mode := nonEmpty(strFromMap(avail, "pricingMode"), "default")
			optPayload := map[string]any{
				"itemId":      itemID,
				"planCode":    wanted,
				"duration":    dur,
				"pricingMode": mode,
				"quantity":    1,
			}
			if err := client.PostWithContext(ctx, fmt.Sprintf("/order/cart/%s/%s/options", url.PathEscape(cartID), catalogPath), optPayload, nil); err == nil {
				added++
			}
			break
		}
	}
	return added
}

// ExtractCartPricePayload parses OVH /order/cart/{id}/summary and /order/cart/{id} into ServerPricePayload.
func ExtractCartPricePayload(summary, info map[string]any) *ServerPricePayload {
	out := &ServerPricePayload{
		PricingMode: "default",
		Prices: ServerPriceValues{
			CurrencyCode: "EUR",
		},
		Items: []ServerPriceItem{},
	}

	if summary != nil {
		if pricesMap, ok := summary["prices"].(map[string]any); ok {
			out.Prices = parsePriceValues(pricesMap)
		}
	}
	if info != nil {
		for _, it := range sliceOfMaps(info["items"]) {
			pm, _ := it["prices"].(map[string]any)
			out.Items = append(out.Items, ServerPriceItem{
				ItemID:      it["itemId"],
				PlanCode:    strFromMap(it, "planCode"),
				Description: strFromMap(it, "description"),
				Prices:      parsePriceValues(pm),
			})
		}
	}
	return out
}

func parsePriceValues(m map[string]any) ServerPriceValues {
	vals := ServerPriceValues{CurrencyCode: "EUR"}
	if m == nil {
		return vals
	}
	var currency string
	vals.WithTax, currency = extractPriceValAndCurrency(m["withTax"])
	vals.WithoutTax, _ = extractPriceValAndCurrency(m["withoutTax"])
	vals.Tax, _ = extractPriceValAndCurrency(m["tax"])
	if currency == "" {
		currency = strFromMap(m, "currencyCode")
	}
	if currency != "" {
		vals.CurrencyCode = currency
	}
	return vals
}

func extractPriceValAndCurrency(raw any) (*float64, string) {
	if raw == nil {
		return nil, ""
	}
	switch v := raw.(type) {
	case map[string]any:
		cc := strFromMap(v, "currencyCode")
		if val, ok := v["value"]; ok && val != nil {
			f := floatFromAny(val)
			return &f, cc
		}
		return nil, cc
	case float64:
		f := v
		return &f, ""
	case int:
		f := float64(v)
		return &f, ""
	default:
		return nil, ""
	}
}

// GetVPSDatacenterAvailability fetches public VPS datacenter availability for planCode and subsidiary.
func (s *Service) GetVPSDatacenterAvailability(ctx context.Context, endpoint, planCode, subsidiary string) (*VPSAvailabilityResponse, error) {
	ep, zn := s.resolveEndpointAndZone(ctx, "", endpoint, subsidiary)
	sub := normalizeSubsidiary(ep, zn)
	apiPath := fmt.Sprintf("/vps/order/rule/datacenter?ovhSubsidiary=%s&planCode=%s", url.QueryEscape(sub), url.QueryEscape(strings.TrimSpace(planCode)))
	var resp VPSAvailabilityResponse
	if err := s.fetchPublicJSON(ctx, ep, apiPath, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetCachedServerCounts returns the total and currently available server plan counts from memory cache.
func (s *Service) GetCachedServerCounts() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var latest *cachedServersEntry
	for _, entry := range s.serversCache {
		if entry != nil && (latest == nil || entry.UpdatedAt > latest.UpdatedAt) {
			latest = entry
		}
	}
	if latest == nil {
		return 0, 0
	}
	avail := 0
	for _, srv := range latest.Servers {
		for _, dc := range srv.Datacenters {
			if dc.Availability != "" && dc.Availability != "unavailable" && dc.Availability != "unknown" {
				avail++
				break
			}
		}
	}
	return len(latest.Servers), avail
}

// GetCacheInfo returns backend cache status and storage metadata for CacheManager.
func (s *Service) GetCacheInfo(_ context.Context) *CacheInfoResponse {
	s.mu.RLock()
	var latest *cachedServersEntry
	for _, entry := range s.serversCache {
		if entry != nil && (latest == nil || entry.UpdatedAt > latest.UpdatedAt) {
			latest = entry
		}
	}
	s.mu.RUnlock()

	resp := &CacheInfoResponse{}
	resp.Backend.CacheDuration = int64(ServerListTTL.Seconds())
	if latest != nil && len(latest.Servers) > 0 {
		now := time.Now().Unix()
		ts := latest.UpdatedAt
		age := max(0, now-ts)
		resp.Backend.HasCachedData = true
		resp.Backend.Timestamp = &ts
		resp.Backend.CacheAge = &age
		resp.Backend.ServerCount = len(latest.Servers)
		resp.Backend.CacheValid = age < resp.Backend.CacheDuration
	}

	resp.Storage.DataDir = s.dataDir
	resp.Storage.CacheDir = s.cacheDir
	resp.Storage.LogsDir = s.logsDir

	dbExists := fileExists(filepath.Join(s.dataDir, "nexus.db"))
	hasCacheFiles := dirHasJSONFiles(s.cacheDir)
	resp.Storage.Files.Config = dbExists
	resp.Storage.Files.Servers = hasCacheFiles
	resp.Storage.Files.Logs = dbExists
	resp.Storage.Files.Queue = dbExists
	resp.Storage.Files.History = dbExists
	return resp
}

// ClearCache clears in-memory and/or on-disk catalog cache files.
func (s *Service) ClearCache(_ context.Context, cacheType string) ([]string, error) {
	mode := strings.ToLower(strings.TrimSpace(cacheType))
	if mode == "" {
		mode = "all"
	}

	var cleared []string
	if mode == "all" || mode == "memory" {
		s.mu.Lock()
		clear(s.dictCache)
		clear(s.serversCache)
		clear(s.rawCatalogs)
		clear(s.vpsCache)
		s.mu.Unlock()
		cleared = append(cleared, "memory")
		if s.log != nil {
			s.log.Info(logger.SourceCatalog, "Cleared in-memory catalog caches")
		}
	}

	if mode == "all" || mode == "files" {
		entries, err := os.ReadDir(s.cacheDir)
		if err == nil {
			for _, ent := range entries {
				target := filepath.Join(s.cacheDir, ent.Name())
				if ent.IsDir() {
					if rmErr := os.RemoveAll(target); rmErr != nil {
						return cleared, rmErr
					}
					cleared = append(cleared, ent.Name())
				} else if strings.HasSuffix(ent.Name(), ".json") {
					if rmErr := os.Remove(target); rmErr != nil {
						return cleared, rmErr
					}
					cleared = append(cleared, ent.Name())
				}
			}
		}
		if s.log != nil {
			s.log.Info(logger.SourceCatalog, "Cleared disk catalog cache files", "count", len(cleared))
		}
	}

	return cleared, nil
}

func (s *Service) getAccountClient(ctx context.Context, accountID string) (*ovh.Client, *store.OVHAccount, error) {
	if s.accountClientFn != nil {
		return s.accountClientFn(ctx, accountID)
	}
	if s.ovhPool == nil {
		return nil, nil, ovhclient.ErrCredentialsMissing
	}
	return s.ovhPool.ClientForAccount(ctx, accountID)
}

func (s *Service) dictCachePath(endpoint, zone string) string {
	ep := safeFileRegex.ReplaceAllString(strings.ToLower(endpoint), "")
	zn := safeFileRegex.ReplaceAllString(strings.ToUpper(zone), "")
	return filepath.Join(s.cacheDir, fmt.Sprintf("catalog_dict_v2_%s_%s.json", ep, zn))
}

func (s *Service) serversCachePath(endpoint, zone string) string {
	ep := safeFileRegex.ReplaceAllString(strings.ToLower(endpoint), "")
	zn := safeFileRegex.ReplaceAllString(strings.ToUpper(zone), "")
	return filepath.Join(s.cacheDir, fmt.Sprintf("servers_%s_%s.json", ep, zn))
}

func (s *Service) vpsCachePath(endpoint, zone string) string {
	ep := safeFileRegex.ReplaceAllString(strings.ToLower(endpoint), "")
	zn := safeFileRegex.ReplaceAllString(strings.ToUpper(zone), "")
	return filepath.Join(s.cacheDir, fmt.Sprintf("vps_catalog_%s_%s.json", ep, zn))
}

func buildServerListResp(servers []ServerPlan, ts int64, valid, usingExpired bool, duration, now int64) *ServerListResponse {
	age := max(0, now-ts)
	ageMin := int(age / 60)
	tsCopy := ts
	ageCopy := age
	nextRefresh := ts + duration
	return &ServerListResponse{
		Servers: servers,
		CacheInfo: CacheInfo{
			Cached:            valid,
			UsingExpiredCache: usingExpired,
			CacheAgeMinutes:   ageMin,
			Timestamp:         &tsCopy,
			CacheAge:          &ageCopy,
			CacheDuration:     duration,
			NextAutoRefresh:   &nextRefresh,
			AutoRefresh:       true,
		},
	}
}

func pickBetterAvailability(a, b string) string {
	rank := func(s string) int {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "", "unknown":
			return 0
		case "unavailable":
			return 1
		case "480h":
			return 2
		case "72h", "240h":
			return 3
		case "1h-low":
			return 4
		case "1h-high", "available":
			return 5
		default:
			return 3
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func extractMonthlyPrice(item map[string]any) *float64 {
	pricings := sliceOfMaps(item["pricings"])
	for _, pr := range pricings {
		if strFromMap(pr, "mode") == "default" && slices.Contains(sliceOfStrings(pr["capacities"]), "renew") {
			if rawVal, ok := pr["price"]; ok && rawVal != nil {
				v := math.Round(floatFromAny(rawVal)/1e6) / 100.0
				return &v
			}
		}
	}
	for _, pr := range pricings {
		if slices.Contains(sliceOfStrings(pr["capacities"]), "renew") {
			if rawVal, ok := pr["price"]; ok && rawVal != nil {
				v := math.Round(floatFromAny(rawVal)/1e6) / 100.0
				return &v
			}
		}
	}
	return nil
}

func memoryPrefixKey(mem string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mem)), "-")
	if len(parts) >= 2 {
		return parts[0] + "-" + parts[1]
	}
	return strings.ToLower(strings.TrimSpace(mem))
}

func readJSONFile[T any](path string) (T, bool) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, false
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, false
	}
	return out, true
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func dirHasJSONFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if !ent.IsDir() && strings.HasSuffix(ent.Name(), ".json") {
			return true
		}
	}
	return false
}

func sliceOfMaps(raw any) []map[string]any {
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func sliceOfStrings(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func strFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func nestedMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		if cur == nil {
			return nil
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func nestedString(m map[string]any, keys ...string) string {
	if len(keys) == 0 {
		return ""
	}
	parent := nestedMap(m, keys[:len(keys)-1]...)
	return strFromMap(parent, keys[len(keys)-1])
}

func splitPipeNonEmpty(s string) []string {
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func floatFromAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
