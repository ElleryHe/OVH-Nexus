package fleet

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
	"ovh-nexus/server/internal/vault"
)

type sanitizedAccount struct {
	ID             string `json:"id"`
	Alias          string `json:"alias"`
	Endpoint       string `json:"endpoint"`
	Zone           string `json:"zone"`
	AppKey         string `json:"appKey"`
	AppSecret      string `json:"appSecret"`
	ConsumerKey    string `json:"consumerKey"`
	HasAppKey      bool   `json:"hasAppKey"`
	HasAppSecret   bool   `json:"hasAppSecret"`
	HasConsumerKey bool   `json:"hasConsumerKey"`
	IsDefault      bool   `json:"isDefault"`
}

func maskAccount(acc store.OVHAccount, effDefaultID string) sanitizedAccount {
	return sanitizedAccount{
		ID:             acc.ID,
		Alias:          acc.Alias,
		Endpoint:       ovhclient.NormalizeEndpoint(acc.Endpoint),
		Zone:           ovhclient.NormalizeZone(acc.Zone, acc.Endpoint),
		AppKey:         vault.MaskSecret(acc.AppKey),
		AppSecret:      vault.MaskSecretKeep(acc.AppSecret, 0),
		ConsumerKey:    vault.MaskSecret(acc.ConsumerKey),
		HasAppKey:      strings.TrimSpace(acc.AppKey) != "",
		HasAppSecret:   strings.TrimSpace(acc.AppSecret) != "",
		HasConsumerKey: strings.TrimSpace(acc.ConsumerKey) != "",
		IsDefault:      acc.ID != "" && acc.ID == effDefaultID,
	}
}

func effectiveDefaultAccountID(accounts []store.OVHAccount) string {
	for _, acc := range accounts {
		if acc.IsDefault {
			return acc.ID
		}
	}
	if len(accounts) > 0 {
		return accounts[0].ID
	}
	return ""
}

type ovhMeProfile struct {
	CustomerCode string `json:"customerCode"`
	Nichandle    string `json:"nichandle"`
	Email        string `json:"email"`
}

func (s *Service) fetchOVHMe(ctx context.Context, endpoint, appKey, appSecret, consumerKey string) (*ovhMeProfile, error) {
	client, err := s.newCredClientFn(endpoint, appKey, appSecret, consumerKey)
	if err != nil {
		return nil, err
	}
	var me ovhMeProfile
	if err := client.GetWithContext(ctx, "/me", &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// HandleListAccounts handles GET /api/accounts, returning masked credentials only.
func (s *Service) HandleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.store.ListAccounts(r.Context())
	if err != nil {
		s.logError(logger.SourceFleet, "Failed to list OVH accounts", "error", err.Error())
		writeSuccessError(w, http.StatusInternalServerError, "Failed to load OVH accounts")
		return
	}

	effDefault := effectiveDefaultAccountID(accounts)
	sanitized := make([]sanitizedAccount, 0, len(accounts))
	for _, acc := range accounts {
		sanitized = append(sanitized, maskAccount(acc, effDefault))
	}

	var defaultPtr *string
	if effDefault != "" {
		defaultPtr = &effDefault
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accounts":         sanitized,
		"defaultAccountId": defaultPtr,
	})
}

// HandleUpsertAccount handles POST /api/accounts.
func (s *Service) HandleUpsertAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID          string `json:"id"`
		Alias       string `json:"alias"`
		Endpoint    string `json:"endpoint"`
		Zone        string `json:"zone"`
		AppKey      string `json:"appKey"`
		AppSecret   string `json:"appSecret"`
		ConsumerKey string `json:"consumerKey"`
		IsDefault   *bool  `json:"isDefault"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ctx := r.Context()
	accID := strings.TrimSpace(body.ID)
	alias := strings.TrimSpace(body.Alias)

	var existing *store.OVHAccount
	if accID != "" {
		found, err := s.store.GetAccount(ctx, accID)
		if err == nil {
			existing = found
		} else if !errors.Is(err, store.ErrNotFound) {
			writeSuccessError(w, http.StatusInternalServerError, "Failed to inspect existing account")
			return
		}
	}

	resolveSecret := func(incoming, prev string) string {
		if vault.IsMaskedPlaceholder(incoming) {
			return prev
		}
		return strings.TrimSpace(incoming)
	}

	var prevAK, prevAS, prevCK, prevEp, prevZone, prevAlias string
	if existing != nil {
		prevAK = existing.AppKey
		prevAS = existing.AppSecret
		prevCK = existing.ConsumerKey
		prevEp = existing.Endpoint
		prevZone = existing.Zone
		prevAlias = existing.Alias
	}

	credentialsChanged := !vault.IsMaskedPlaceholder(body.AppKey) ||
		!vault.IsMaskedPlaceholder(body.AppSecret) ||
		!vault.IsMaskedPlaceholder(body.ConsumerKey)

	appKey := resolveSecret(body.AppKey, prevAK)
	appSecret := resolveSecret(body.AppSecret, prevAS)
	consumerKey := resolveSecret(body.ConsumerKey, prevCK)

	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" {
		endpoint = prevEp
	}
	endpoint = ovhclient.NormalizeEndpoint(endpoint)

	if accID == "" {
		if appKey == "" || appSecret == "" || consumerKey == "" {
			writeSuccessError(w, http.StatusBadRequest, "Account ID is missing and complete OVH credentials were not provided")
			return
		}
		me, err := s.fetchOVHMe(ctx, endpoint, appKey, appSecret, consumerKey)
		if err != nil {
			msg := "Failed to resolve OVH account profile: " + formatOVHError(err)
			s.logError(logger.SourceFleet, "Failed to resolve OVH account profile", "error", msg)
			writeSuccessError(w, http.StatusBadRequest, msg)
			return
		}
		accID = strings.TrimSpace(me.CustomerCode)
		if accID == "" {
			accID = strings.TrimSpace(me.Nichandle)
		}
		if accID == "" {
			writeSuccessError(w, http.StatusBadRequest, "OVH /me response did not include customerCode or nichandle")
			return
		}
		if found, err := s.store.GetAccount(ctx, accID); err == nil {
			existing = found
			prevAlias = found.Alias
			prevZone = found.Zone
		}
		if alias == "" {
			if strings.TrimSpace(me.Email) != "" {
				alias = strings.TrimSpace(me.Email)
			} else if prevAlias != "" {
				alias = prevAlias
			} else {
				alias = accID
			}
		}
	} else if existing != nil && credentialsChanged {
		if appKey == "" || appSecret == "" || consumerKey == "" {
			writeSuccessError(w, http.StatusBadRequest, "Provide all three OVH credentials when updating account keys")
			return
		}
		me, err := s.fetchOVHMe(ctx, endpoint, appKey, appSecret, consumerKey)
		if err != nil {
			msg := "Failed to verify updated account credentials: " + formatOVHError(err)
			s.logError(logger.SourceFleet, "Failed to verify updated OVH account credentials", "accountId", accID, "error", msg)
			writeSuccessError(w, http.StatusBadRequest, msg)
			return
		}
		cc := strings.TrimSpace(me.CustomerCode)
		nh := strings.TrimSpace(me.Nichandle)
		if (cc != "" || nh != "") && accID != cc && accID != nh {
			writeSuccessError(w, http.StatusBadRequest, "Provided credentials belong to a different OVH account; please add it as a new account")
			return
		}
		if alias == "" {
			if prevAlias != "" {
				alias = prevAlias
			} else if strings.TrimSpace(me.Email) != "" {
				alias = strings.TrimSpace(me.Email)
			} else {
				alias = accID
			}
		}
	} else {
		if existing == nil && (appKey == "" || appSecret == "" || consumerKey == "") {
			writeSuccessError(w, http.StatusBadRequest, "Complete OVH credentials (appKey, appSecret, consumerKey) are required")
			return
		}
		if alias == "" {
			if prevAlias != "" {
				alias = prevAlias
			} else {
				alias = accID
			}
		}
	}

	zone := strings.TrimSpace(body.Zone)
	if zone == "" {
		zone = prevZone
	}
	zone = ovhclient.NormalizeZone(zone, endpoint)

	isDefault := false
	if body.IsDefault != nil {
		isDefault = *body.IsDefault
	} else if existing != nil {
		isDefault = existing.IsDefault
	}

	saved, err := s.store.UpsertAccount(ctx, store.OVHAccount{
		ID:          accID,
		Alias:       alias,
		Endpoint:    endpoint,
		Zone:        zone,
		AppKey:      appKey,
		AppSecret:   appSecret,
		ConsumerKey: consumerKey,
		IsDefault:   isDefault,
	})
	if err != nil {
		s.logError(logger.SourceFleet, "Failed to persist OVH account", "accountId", accID, "error", err.Error())
		writeSuccessError(w, http.StatusInternalServerError, "Failed to save OVH account")
		return
	}

	if s.ovhPool != nil {
		s.ovhPool.Invalidate(saved.ID)
	}
	s.registerSecrets(saved.AppKey, saved.AppSecret, saved.ConsumerKey)
	s.logInfo(logger.SourceFleet, "Saved OVH account profile", "accountId", saved.ID, "endpoint", saved.Endpoint, "zone", saved.Zone)

	allAccounts, _ := s.store.ListAccounts(ctx)
	effDefault := effectiveDefaultAccountID(allAccounts)
	var defaultPtr *string
	if effDefault != "" {
		defaultPtr = &effDefault
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"defaultAccountId": defaultPtr,
		"account":          maskAccount(*saved, effDefault),
	})
}

// HandleDeleteAccount handles DELETE /api/accounts/{id}.
func (s *Service) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	accID := strings.TrimSpace(chi.URLParam(r, "id"))
	if accID == "" {
		writeSuccessError(w, http.StatusBadRequest, "Account ID is required")
		return
	}

	ctx := r.Context()
	if err := s.store.DeleteAccount(ctx, accID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeSuccessError(w, http.StatusNotFound, "Account not found")
			return
		}
		s.logError(logger.SourceFleet, "Failed to delete OVH account", "accountId", accID, "error", err.Error())
		writeSuccessError(w, http.StatusInternalServerError, "Failed to delete OVH account")
		return
	}

	if s.ovhPool != nil {
		s.ovhPool.Invalidate(accID)
	}
	clearedQueue, _ := s.store.ClearQueue(ctx, accID)
	clearedSubs, _ := s.store.ClearStockSubs(ctx, accID)
	if s.onAccountDeleted != nil {
		s.onAccountDeleted(accID)
	}

	s.logInfo(logger.SourceFleet, "Deleted OVH account and associated tasks", "accountId", accID, "clearedQueue", clearedQueue, "clearedSubscriptions", clearedSubs)

	allAccounts, _ := s.store.ListAccounts(ctx)
	effDefault := effectiveDefaultAccountID(allAccounts)
	var defaultPtr *string
	if effDefault != "" {
		defaultPtr = &effDefault
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"defaultAccountId": defaultPtr,
	})
}

// HandleSetDefaultAccount handles PUT /api/accounts/default.
func (s *Service) HandleSetDefaultAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	accID := strings.TrimSpace(body.ID)
	if accID == "" {
		writeSuccessError(w, http.StatusNotFound, "Account not found")
		return
	}
	if err := s.store.SetDefaultAccount(r.Context(), accID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeSuccessError(w, http.StatusNotFound, "Account not found")
			return
		}
		s.logError(logger.SourceFleet, "Failed to set default OVH account", "accountId", accID, "error", err.Error())
		writeSuccessError(w, http.StatusInternalServerError, "Failed to set default account")
		return
	}

	s.logInfo(logger.SourceFleet, "Updated default OVH account", "accountId", accID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"defaultAccountId": accID,
	})
}

// HandleResolveAccountInfo handles POST /api/accounts/resolve-info.
func (s *Service) HandleResolveAccountInfo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AppKey      string `json:"appKey"`
		AppSecret   string `json:"appSecret"`
		ConsumerKey string `json:"consumerKey"`
		Endpoint    string `json:"endpoint"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	appKey := strings.TrimSpace(body.AppKey)
	appSecret := strings.TrimSpace(body.AppSecret)
	consumerKey := strings.TrimSpace(body.ConsumerKey)
	endpoint := ovhclient.NormalizeEndpoint(body.Endpoint)

	if appKey == "" || appSecret == "" || consumerKey == "" {
		writeSuccessError(w, http.StatusBadRequest, "Missing OVH credentials")
		return
	}

	me, err := s.fetchOVHMe(r.Context(), endpoint, appKey, appSecret, consumerKey)
	if err != nil {
		msg := formatOVHError(err)
		s.logError(logger.SourceFleet, "Failed to resolve OVH account metadata", "endpoint", endpoint, "error", msg)
		writeSuccessError(w, http.StatusBadRequest, msg)
		return
	}

	customerCode := strings.TrimSpace(me.CustomerCode)
	if customerCode == "" {
		customerCode = strings.TrimSpace(me.Nichandle)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"customerCode": customerCode,
		"email":        strings.TrimSpace(me.Email),
		"nichandle":    strings.TrimSpace(me.Nichandle),
	})
}

// HandleVerifyAuth handles GET|POST /api/verify-auth.
func (s *Service) HandleVerifyAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AppKey      string `json:"appKey"`
		AppSecret   string `json:"appSecret"`
		ConsumerKey string `json:"consumerKey"`
		Endpoint    string `json:"endpoint"`
		AccountID   string `json:"accountId"`
	}
	if r.Method == http.MethodPost {
		_ = decodeJSONBody(r, &body)
	}

	ctx := r.Context()
	var client OVHClient
	var err error

	ak := strings.TrimSpace(body.AppKey)
	as := strings.TrimSpace(body.AppSecret)
	ck := strings.TrimSpace(body.ConsumerKey)

	if ak != "" && as != "" && ck != "" &&
		!vault.IsMaskedPlaceholder(ak) && !vault.IsMaskedPlaceholder(as) && !vault.IsMaskedPlaceholder(ck) {
		ep := strings.TrimSpace(body.Endpoint)
		if ep == "" {
			if acc, accErr := s.store.GetActiveAccount(ctx, s.resolveAccountHint(r, body.AccountID)); accErr == nil && acc != nil {
				ep = acc.Endpoint
			}
		}
		client, err = s.newCredClientFn(ovhclient.NormalizeEndpoint(ep), ak, as, ck)
	} else {
		accHint := s.resolveAccountHint(r, body.AccountID)
		client, _, err = s.clientForAccountFn(ctx, accHint)
	}

	if err != nil || client == nil {
		writeJSON(w, http.StatusOK, map[string]any{"valid": false})
		return
	}

	var me map[string]any
	if err := client.GetWithContext(ctx, "/me", &me); err != nil {
		s.logWarn(logger.SourceFleet, "OVH credential verification failed", "error", formatOVHError(err))
		writeJSON(w, http.StatusOK, map[string]any{"valid": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true})
}

// HandleDebugAccount handles GET /api/debug/account returning safe diagnostics only.
func (s *Service) HandleDebugAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hdrID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))

	var hdrPtr *string
	if hdrID != "" {
		hdrPtr = &hdrID
	}

	acc, err := s.store.GetActiveAccount(ctx, hdrID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeSuccessError(w, http.StatusInternalServerError, "Failed to inspect account state")
		return
	}

	if acc == nil {
		ep, z, _ := s.loadDefaultEndpointAndZone(ctx)
		writeJSON(w, http.StatusOK, map[string]any{
			"headerAccountId":    hdrPtr,
			"requestedAccountId": nil,
			"exists":             false,
			"alias":              nil,
			"endpoint":           ep,
			"zone":               z,
			"hasAppKey":          false,
			"hasAppSecret":       false,
			"hasConsumerKey":     false,
		})
		return
	}

	reqID := acc.ID
	alias := acc.Alias
	writeJSON(w, http.StatusOK, map[string]any{
		"headerAccountId":    hdrPtr,
		"requestedAccountId": &reqID,
		"exists":             true,
		"alias":              &alias,
		"endpoint":           ovhclient.NormalizeEndpoint(acc.Endpoint),
		"zone":               ovhclient.NormalizeZone(acc.Zone, acc.Endpoint),
		"hasAppKey":          strings.TrimSpace(acc.AppKey) != "",
		"hasAppSecret":       strings.TrimSpace(acc.AppSecret) != "",
		"hasConsumerKey":     strings.TrimSpace(acc.ConsumerKey) != "",
	})
}

// HandleGetSettings handles GET /api/settings.
func (s *Service) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hdrID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	acc, err := s.store.GetActiveAccount(ctx, hdrID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeSuccessError(w, http.StatusInternalServerError, "Failed to load settings")
		return
	}

	ep, z, iam := s.loadDefaultEndpointAndZone(ctx)
	var ak, as, ck string
	if acc != nil {
		ep = ovhclient.NormalizeEndpoint(acc.Endpoint)
		z = ovhclient.NormalizeZone(acc.Zone, ep)
		ak = acc.AppKey
		as = acc.AppSecret
		ck = acc.ConsumerKey
		if iam == "" || strings.HasPrefix(iam, "ovh-nexus-") {
			iam = ovhclient.DefaultIAM(z)
		}
	}
	if iam == "" {
		iam = ovhclient.DefaultIAM(z)
	}

	defaultLocale := "en"
	if s.cfg != nil && strings.TrimSpace(s.cfg.DefaultLocale) != "" {
		defaultLocale = s.cfg.DefaultLocale
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"appKey":         vault.MaskSecret(ak),
		"appSecret":      vault.MaskSecretKeep(as, 0),
		"consumerKey":    vault.MaskSecret(ck),
		"endpoint":       ep,
		"tgToken":        "",
		"tgChatId":       "",
		"iam":            iam,
		"zone":           z,
		"hasAppKey":      strings.TrimSpace(ak) != "",
		"hasAppSecret":   strings.TrimSpace(as) != "",
		"hasConsumerKey": strings.TrimSpace(ck) != "",
		"hasTgToken":     false,
		"defaultLocale":  defaultLocale,
	})
}

// HandleSaveSettings handles POST /api/settings safely without legacy webhook hooks.
func (s *Service) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClearCredentials bool    `json:"clearCredentials"`
		AppKey           *string `json:"appKey"`
		AppSecret        *string `json:"appSecret"`
		ConsumerKey      *string `json:"consumerKey"`
		Endpoint         *string `json:"endpoint"`
		Zone             *string `json:"zone"`
		IAM              *string `json:"iam"`
		AccountID        string  `json:"accountId"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeStatusError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ctx := r.Context()
	accHint := s.resolveAccountHint(r, body.AccountID)
	acc, err := s.store.GetActiveAccount(ctx, accHint)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeStatusError(w, http.StatusInternalServerError, "Failed to load active account")
		return
	}

	curEp, curZone, curIAM := s.loadDefaultEndpointAndZone(ctx)
	if acc != nil {
		curEp = ovhclient.NormalizeEndpoint(acc.Endpoint)
		curZone = ovhclient.NormalizeZone(acc.Zone, curEp)
	}

	nextEp := curEp
	if body.Endpoint != nil && strings.TrimSpace(*body.Endpoint) != "" {
		nextEp = ovhclient.NormalizeEndpoint(*body.Endpoint)
	}
	nextZone := curZone
	if body.Zone != nil && strings.TrimSpace(*body.Zone) != "" {
		nextZone = ovhclient.NormalizeZone(*body.Zone, nextEp)
	} else if body.Endpoint != nil && acc == nil {
		nextZone = ovhclient.NormalizeZone("", nextEp)
	}

	nextIAM := curIAM
	if body.IAM != nil {
		trimmedIAM := strings.TrimSpace(*body.IAM)
		if trimmedIAM == "" {
			nextIAM = ovhclient.DefaultIAM(nextZone)
		} else {
			nextIAM = trimmedIAM
		}
	} else if body.Zone != nil || nextIAM == "" {
		nextIAM = ovhclient.DefaultIAM(nextZone)
	}

	if body.ClearCredentials {
		if acc != nil {
			acc.AppKey = ""
			acc.AppSecret = ""
			acc.ConsumerKey = ""
			acc.Endpoint = nextEp
			acc.Zone = nextZone
			if _, upErr := s.store.UpsertAccount(ctx, *acc); upErr != nil {
				writeStatusError(w, http.StatusInternalServerError, "Failed to clear account credentials")
				return
			}
			if s.ovhPool != nil {
				s.ovhPool.Invalidate(acc.ID)
			}
			s.logWarn(logger.SourceFleet, "Cleared OVH API credentials via settings", "accountId", acc.ID)
		}
	} else {
		updateCred := func(incoming *string, existing string) (string, bool) {
			if incoming == nil || vault.IsMaskedPlaceholder(*incoming) {
				return existing, false
			}
			return strings.TrimSpace(*incoming), true
		}

		var ak, as, ck string
		var changed bool
		if acc != nil {
			ak, _ = updateCred(body.AppKey, acc.AppKey)
			as, _ = updateCred(body.AppSecret, acc.AppSecret)
			ck, _ = updateCred(body.ConsumerKey, acc.ConsumerKey)
			changed = (ak != acc.AppKey) || (as != acc.AppSecret) || (ck != acc.ConsumerKey) || (nextEp != acc.Endpoint) || (nextZone != acc.Zone)
			if changed {
				acc.AppKey = ak
				acc.AppSecret = as
				acc.ConsumerKey = ck
				acc.Endpoint = nextEp
				acc.Zone = nextZone
				if _, upErr := s.store.UpsertAccount(ctx, *acc); upErr != nil {
					writeStatusError(w, http.StatusInternalServerError, "Failed to update account settings")
					return
				}
				if s.ovhPool != nil {
					s.ovhPool.Invalidate(acc.ID)
				}
				s.registerSecrets(ak, as, ck)
			}
		} else {
			ak, c1 := updateCred(body.AppKey, "")
			as, c2 := updateCred(body.AppSecret, "")
			ck, c3 := updateCred(body.ConsumerKey, "")
			if (c1 || c2 || c3) && ak != "" && as != "" && ck != "" {
				targetID := "default"
				alias := "Default"
				if me, meErr := s.fetchOVHMe(ctx, nextEp, ak, as, ck); meErr == nil && me != nil {
					if cc := strings.TrimSpace(me.CustomerCode); cc != "" {
						targetID = cc
					} else if nh := strings.TrimSpace(me.Nichandle); nh != "" {
						targetID = nh
					}
					if em := strings.TrimSpace(me.Email); em != "" {
						alias = em
					}
				}
				created, upErr := s.store.UpsertAccount(ctx, store.OVHAccount{
					ID:          targetID,
					Alias:       alias,
					Endpoint:    nextEp,
					Zone:        nextZone,
					AppKey:      ak,
					AppSecret:   as,
					ConsumerKey: ck,
					IsDefault:   true,
				})
				if upErr != nil {
					writeStatusError(w, http.StatusInternalServerError, "Failed to create account from settings")
					return
				}
				if s.ovhPool != nil {
					s.ovhPool.Invalidate(created.ID)
				}
				s.registerSecrets(ak, as, ck)
			}
		}
	}

	_ = s.store.SetSetting(ctx, "settings_endpoint", nextEp)
	_ = s.store.SetSetting(ctx, "settings_zone", nextZone)
	_ = s.store.SetSetting(ctx, "settings_iam", nextIAM)
	s.logInfo(logger.SourceFleet, "Updated system settings", "endpoint", nextEp, "zone", nextZone, "iam", nextIAM)

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
	})
}

// HandleEndpointConfig handles GET /api/endpoint-config.
func (s *Service) HandleEndpointConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hdrID := strings.TrimSpace(r.Header.Get("X-OVH-Account"))
	ep, z, _ := s.loadDefaultEndpointAndZone(ctx)
	if acc, err := s.store.GetActiveAccount(ctx, hdrID); err == nil && acc != nil {
		ep = ovhclient.NormalizeEndpoint(acc.Endpoint)
		z = ovhclient.NormalizeZone(acc.Zone, ep)
	}
	defaultLocale := "en"
	if s.cfg != nil && strings.TrimSpace(s.cfg.DefaultLocale) != "" {
		defaultLocale = s.cfg.DefaultLocale
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"endpoint":      ep,
		"zone":          z,
		"defaultLocale": defaultLocale,
	})
}

func (s *Service) resolveAccountHint(r *http.Request, bodyAccountID string) string {
	if r != nil {
		if hdr := strings.TrimSpace(r.Header.Get("X-OVH-Account")); hdr != "" {
			return hdr
		}
	}
	return strings.TrimSpace(bodyAccountID)
}

func (s *Service) loadDefaultEndpointAndZone(ctx context.Context) (string, string, string) {
	ep := ovhclient.EndpointEU
	if raw, ok, err := s.store.GetSetting(ctx, "settings_endpoint"); err == nil && ok && strings.TrimSpace(raw) != "" {
		ep = ovhclient.NormalizeEndpoint(raw)
	}
	z := ovhclient.DefaultZoneForEndpoint(ep)
	if raw, ok, err := s.store.GetSetting(ctx, "settings_zone"); err == nil && ok && strings.TrimSpace(raw) != "" {
		z = ovhclient.NormalizeZone(raw, ep)
	}
	iam := ovhclient.DefaultIAM(z)
	if raw, ok, err := s.store.GetSetting(ctx, "settings_iam"); err == nil && ok && strings.TrimSpace(raw) != "" {
		iam = strings.TrimSpace(raw)
	}
	return ep, z, iam
}
