package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/ovhclient"
)

// MountRoutes registers catalog, availability, pricing, dictionary, and cache routes on an /api chi.Router.
func (s *Service) MountRoutes(r chi.Router) {
	r.Get("/servers", s.HandleListServers)
	r.Post("/servers/refresh", s.HandleRefreshServers)
	r.Post("/servers/{planCode}/price", s.HandleGetServerPrice)

	r.Get("/availability", s.HandleGetGlobalAvailability)
	r.Get("/availability/{planCode}", s.HandleGetPlanAvailability)
	r.Post("/availability/{planCode}", s.HandleGetPlanAvailability)

	r.Get("/catalog/dictionary", s.HandleGetDictionary)
	r.Get("/cache/info", s.HandleGetCacheInfo)
	r.Post("/cache/clear", s.HandleClearCache)
}

// HandleListServers handles GET /api/servers.
func (s *Service) HandleListServers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	force := parseBoolQuery(q.Get("force")) || parseBoolQuery(q.Get("refresh")) || parseBoolQuery(q.Get("forceRefresh"))
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))

	resp, err := s.ListServers(r.Context(), ListServersOptions{
		AccountID:    accountID,
		Endpoint:     q.Get("endpoint"),
		Zone:         q.Get("zone"),
		ForceRefresh: force,
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "No data available",
			"message": fmt.Sprintf("Failed to load OVH server catalog: %v", err),
		})
		return
	}
	if resp.CacheInfo.UsingExpiredCache {
		w.Header().Set("X-Cache-Warning", fmt.Sprintf("Using expired cache (%d minutes old)", resp.CacheInfo.CacheAgeMinutes))
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleRefreshServers handles POST /api/servers/refresh.
func (s *Service) HandleRefreshServers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	ep, zn := s.resolveEndpointAndZone(r.Context(), accountID, q.Get("endpoint"), q.Get("zone"))

	resp, err := s.RefreshCatalog(r.Context(), ep, zn)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "Refresh failed",
			"message": fmt.Sprintf("Failed to refresh OVH server catalog: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleGetServerPrice handles POST /api/servers/{planCode}/price.
func (s *Service) HandleGetServerPrice(w http.ResponseWriter, r *http.Request) {
	planCode := strings.TrimSpace(chi.URLParam(r, "planCode"))
	var body struct {
		Datacenter string   `json:"datacenter"`
		Options    []string `json:"options"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body)
	if strings.TrimSpace(body.Datacenter) == "" {
		body.Datacenter = "gra"
	}
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))

	res, err := s.GetServerPrice(r.Context(), accountID, planCode, body.Datacenter, body.Options)
	if err != nil {
		if errors.Is(err, ovhclient.ErrCredentialsMissing) {
			writeJSON(w, http.StatusBadRequest, res)
			return
		}
		writeJSON(w, http.StatusInternalServerError, res)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// HandleGetGlobalAvailability handles GET /api/availability.
func (s *Service) HandleGetGlobalAvailability(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	ep, _ := s.resolveEndpointAndZone(r.Context(), accountID, q.Get("endpoint"), q.Get("zone"))
	items, err := s.GetRawAvailabilities(r.Context(), ep, q.Get("planCode"))
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "Availability query failed",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// HandleGetPlanAvailability handles GET|POST /api/availability/{planCode}.
func (s *Service) HandleGetPlanAvailability(w http.ResponseWriter, r *http.Request) {
	planCode := strings.TrimSpace(chi.URLParam(r, "planCode"))
	var options []string
	if r.Method == http.MethodPost {
		var raw map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&raw)
		if raw != nil {
			switch optVal := raw["options"].(type) {
			case []any:
				for _, item := range optVal {
					if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
						options = append(options, strings.TrimSpace(str))
					}
				}
			case string:
				for _, part := range strings.Split(optVal, ",") {
					if trimmed := strings.TrimSpace(part); trimmed != "" {
						options = append(options, trimmed)
					}
				}
			}
		}
	} else {
		if rawOpts := strings.TrimSpace(r.URL.Query().Get("options")); rawOpts != "" {
			for _, part := range strings.Split(rawOpts, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					options = append(options, trimmed)
				}
			}
		}
	}

	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	ep, _ := s.resolveEndpointAndZone(r.Context(), accountID, r.URL.Query().Get("endpoint"), r.URL.Query().Get("zone"))
	avail, err := s.GetAvailability(r.Context(), ep, planCode, options)
	if err != nil || len(avail) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, avail)
}

// HandleGetDictionary handles GET /api/catalog/dictionary.
func (s *Service) HandleGetDictionary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	ep, zn := s.resolveEndpointAndZone(r.Context(), accountID, q.Get("endpoint"), q.Get("zone"))
	force := parseBoolQuery(q.Get("refresh")) || parseBoolQuery(q.Get("force"))

	dict, err := s.GetDictionary(r.Context(), ep, zn, force)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "Catalog dictionary unavailable",
			"message": err.Error(),
		})
		return
	}

	etag := fmt.Sprintf(`W/"%d-%d"`, dict.UpdatedAt, len(dict.Plans))
	if !force && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	writeJSON(w, http.StatusOK, dict)
}

// HandleGetCacheInfo handles GET /api/cache/info.
func (s *Service) HandleGetCacheInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.GetCacheInfo(r.Context()))
}

// HandleClearCache handles POST /api/cache/clear.
func (s *Service) HandleClearCache(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body)
	if strings.TrimSpace(body.Type) == "" {
		body.Type = "all"
	}

	cleared, err := s.ClearCache(r.Context(), body.Type)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"cleared": cleared,
		"message": fmt.Sprintf("Cleared catalog cache (%s)", strings.Join(cleared, ", ")),
	})
}

func parseBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
