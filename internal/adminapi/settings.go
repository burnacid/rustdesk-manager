package adminapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/settings"
)

// GET /admin/api/settings
func (a *API) getSettings(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"require_registered": a.settings.Bool(settings.RequireRegistered),
		"accept_recordings":  a.settings.Bool(settings.AcceptRecordings),
		"public_url":         a.cfg.PublicURL,
		"oidc_redirect_url":  a.sso.RedirectURL(),
	})
}

// PATCH /admin/api/settings {require_registered?, accept_recordings?}
func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequireRegistered *bool `json:"require_registered"`
		AcceptRecordings  *bool `json:"accept_recordings"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.RequireRegistered != nil {
		_ = a.settings.SetBool(settings.RequireRegistered, *req.RequireRegistered)
	}
	if req.AcceptRecordings != nil {
		_ = a.settings.SetBool(settings.AcceptRecordings, *req.AcceptRecordings)
	}
	a.getSettings(w, r)
}

// OIDC providers. The client secret is write-only.

type oidcProviderJSON struct {
	ID         uint      `json:"id"`
	Name       string    `json:"name"`
	Issuer     string    `json:"issuer"`
	ClientID   string    `json:"client_id"`
	HasSecret  bool      `json:"has_secret"`
	Scopes     string    `json:"scopes"`
	AutoCreate bool      `json:"auto_create"`
	TrustEmail bool      `json:"trust_email"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

func toProviderJSON(p *db.OIDCProvider) oidcProviderJSON {
	return oidcProviderJSON{p.ID, p.Name, p.Issuer, p.ClientID, p.ClientSecret != "", p.Scopes, p.AutoCreate,
		p.TrustEmail, p.Enabled, p.CreatedAt}
}

// GET /admin/api/oidc-providers
func (a *API) listOIDCProviders(w http.ResponseWriter, _ *http.Request) {
	var rows []db.OIDCProvider
	a.db.Order("name").Find(&rows)
	out := make([]oidcProviderJSON, 0, len(rows))
	for i := range rows {
		out = append(out, toProviderJSON(&rows[i]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type oidcProviderReq struct {
	Name         *string `json:"name"`
	Issuer       *string `json:"issuer"`
	ClientID     *string `json:"client_id"`
	ClientSecret *string `json:"client_secret"` // empty or absent keeps the current secret
	Scopes       *string `json:"scopes"`
	AutoCreate   *bool   `json:"auto_create"`
	TrustEmail   *bool   `json:"trust_email"`
	Enabled      *bool   `json:"enabled"`
}

var providerName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

func (a *API) applyProviderReq(p *db.OIDCProvider, req oidcProviderReq) error {
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if !providerName.MatchString(n) {
			return errors.New("name must be 1-32 letters, digits, - or _ (e.g. google, azure, keycloak)")
		}
		var taken int64
		a.db.Model(&db.OIDCProvider{}).Where("name = ? AND id <> ?", n, p.ID).Count(&taken)
		if taken > 0 {
			return errors.New("a provider with this name already exists")
		}
		p.Name = n
	}
	if req.Issuer != nil {
		iss := strings.TrimRight(strings.TrimSpace(*req.Issuer), "/")
		if !strings.HasPrefix(iss, "https://") && !strings.HasPrefix(iss, "http://") {
			return errors.New("issuer must be a URL, e.g. https://accounts.google.com")
		}
		p.Issuer = iss
	}
	if req.ClientID != nil {
		p.ClientID = strings.TrimSpace(*req.ClientID)
	}
	if req.ClientSecret != nil && *req.ClientSecret != "" {
		p.ClientSecret = a.box.Seal(strings.TrimSpace(*req.ClientSecret))
	}
	if req.Scopes != nil {
		p.Scopes = strings.TrimSpace(*req.Scopes)
	}
	if req.AutoCreate != nil {
		p.AutoCreate = *req.AutoCreate
	}
	if req.TrustEmail != nil {
		p.TrustEmail = *req.TrustEmail
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if p.Name == "" || p.Issuer == "" || p.ClientID == "" {
		return errors.New("name, issuer and client ID are required")
	}
	return nil
}

// POST /admin/api/oidc-providers
func (a *API) createOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var req oidcProviderReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	p := db.OIDCProvider{Enabled: true}
	if err := a.applyProviderReq(&p, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Create(&p).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, toProviderJSON(&p))
}

func (a *API) loadProvider(w http.ResponseWriter, r *http.Request) (*db.OIDCProvider, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	var p db.OIDCProvider
	if err := a.db.First(&p, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "provider not found")
		return nil, false
	} else if err != nil {
		serverError(w, err)
		return nil, false
	}
	return &p, true
}

// PATCH /admin/api/oidc-providers/{id}
func (a *API) updateOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	var req oidcProviderReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.applyProviderReq(p, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Save(p).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, toProviderJSON(p))
}

// DELETE /admin/api/oidc-providers/{id} (users stay; their links to it go)
func (a *API) deleteOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProvider(w, r)
	if !ok {
		return
	}
	a.db.Where("provider_id = ?", p.ID).Delete(&db.OIDCIdentity{})
	a.db.Where("provider_id = ?", p.ID).Delete(&db.OIDCSession{})
	a.db.Delete(p)
	w.WriteHeader(http.StatusNoContent)
}

// Deploy tokens for `rustdesk --assign --token ...`.

type deployTokenJSON struct {
	ID        uint       `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// GET /admin/api/deploy-tokens
func (a *API) listDeployTokens(w http.ResponseWriter, _ *http.Request) {
	var rows []db.DeployToken
	a.db.Order("id DESC").Find(&rows)
	out := make([]deployTokenJSON, 0, len(rows))
	for _, t := range rows {
		out = append(out, deployTokenJSON{t.ID, t.Name, t.Prefix, t.ExpiresAt, t.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/api/deploy-tokens {name, expires_days? (0 = never)} -> {token} shown once
func (a *API) createDeployToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		ExpiresDays int    `json:"expires_days"`
	}
	if err := httpx.Decode(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	plain := "rdm_" + strings.ReplaceAll(strings.ReplaceAll(auth.NewSecret(), "-", ""), "_", "")
	t := db.DeployToken{Name: strings.TrimSpace(req.Name), Hash: auth.HashToken(plain), Prefix: plain[:10]}
	if req.ExpiresDays > 0 {
		exp := time.Now().UTC().AddDate(0, 0, req.ExpiresDays)
		t.ExpiresAt = &exp
	}
	if err := a.db.Create(&t).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": plain,
		"info": deployTokenJSON{t.ID, t.Name, t.Prefix, t.ExpiresAt, t.CreatedAt}})
}

// DELETE /admin/api/deploy-tokens/{id}
func (a *API) deleteDeployToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a.db.Delete(&db.DeployToken{}, id)
	w.WriteHeader(http.StatusNoContent)
}

// Recordings.

type recordingJSON struct {
	ID         uint      `json:"id"`
	File       string    `json:"file"`
	DeviceID   string    `json:"device_id"`
	DeviceName string    `json:"device_name"`
	Incoming   bool      `json:"incoming"` // someone connected to this device
	Size       int64     `json:"size"`
	Completed  bool      `json:"completed"`
	CreatedAt  time.Time `json:"created_at"`
}

// GET /admin/api/recordings?device=&current=&pageSize=
func (a *API) listRecordings(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := a.db.Model(&db.Recording{})
	if d := r.URL.Query().Get("device"); d != "" {
		tx = tx.Where("device_id = ?", d)
	}
	var total int64
	tx.Count(&total)
	var rows []db.Recording
	if err := tx.Order("id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, rec := range rows {
		ids = append(ids, rec.DeviceID)
	}
	names := a.hostnames(ids)
	out := httpx.Paged[recordingJSON]{Total: total, Data: make([]recordingJSON, 0, len(rows))}
	for _, rec := range rows {
		out.Data = append(out.Data, recordingJSON{rec.ID, rec.File, rec.DeviceID, names[rec.DeviceID],
			strings.HasPrefix(rec.File, "incoming_"), rec.Size, rec.Completed, rec.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) loadRecording(w http.ResponseWriter, r *http.Request) (*db.Recording, string, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, "", false
	}
	var rec db.Recording
	if err := a.db.First(&rec, id).Error; err != nil {
		httpx.Error(w, http.StatusNotFound, "recording not found")
		return nil, "", false
	}
	return &rec, filepath.Join(a.cfg.RecordingsDir(), filepath.Base(rec.File)), true
}

// GET /admin/api/recordings/{id}/file (supports range requests, for the video player)
func (a *API) recordingFile(w http.ResponseWriter, r *http.Request) {
	rec, path, ok := a.loadRecording(w, r)
	if !ok {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "the file is missing on disk")
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+rec.File+`"`)
	}
	http.ServeContent(w, r, rec.File, info.ModTime(), f)
}

// DELETE /admin/api/recordings/{id}
func (a *API) deleteRecording(w http.ResponseWriter, r *http.Request) {
	rec, path, ok := a.loadRecording(w, r)
	if !ok {
		return
	}
	_ = os.Remove(path)
	a.db.Delete(rec)
	w.WriteHeader(http.StatusNoContent)
}
