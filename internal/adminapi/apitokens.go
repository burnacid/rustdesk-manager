package adminapi

import (
	"net/http"
	"strings"
	"time"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// API tokens let scripts call the admin API with "Authorization: Bearer <token>".
// A token acts as the admin who created it, and stops working if that user is
// disabled, loses admin rights or changes their password.

type apiTokenJSON struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Owner      string     `json:"owner"`
	LastUsedAt time.Time  `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func toAPITokenJSON(t *db.Token) apiTokenJSON {
	return apiTokenJSON{ID: t.ID, Name: t.Name, Owner: t.User.Name, LastUsedAt: t.LastUsedAt,
		ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt}
}

// apiTokenUser resolves a Bearer API token; nil if missing or invalid.
func (a *API) apiTokenUser(r *http.Request) *db.User {
	plain, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return nil
	}
	tok, err := auth.Lookup(a.db, auth.KindAPI, strings.TrimSpace(plain))
	if err != nil {
		return nil
	}
	return &tok.User
}

func hasBearer(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// GET /admin/api/api-tokens
func (a *API) listAPITokens(w http.ResponseWriter, _ *http.Request) {
	var rows []db.Token
	if err := a.db.Preload("User").Where("kind = ?", auth.KindAPI).Order("id DESC").Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	out := make([]apiTokenJSON, 0, len(rows))
	for i := range rows {
		out = append(out, toAPITokenJSON(&rows[i]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/api/api-tokens {name, expires_days? (0 = never)} -> {token, info}; the token is shown once.
func (a *API) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		ExpiresDays int    `json:"expires_days"`
	}
	if err := httpx.Decode(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	me := currentUser(r)
	t := db.Token{Kind: auth.KindAPI, Name: strings.TrimSpace(req.Name), UserID: me.ID}
	if req.ExpiresDays > 0 {
		exp := time.Now().UTC().AddDate(0, 0, req.ExpiresDays)
		t.ExpiresAt = &exp
	}
	plain, err := auth.IssueToken(a.db, &t)
	if err != nil {
		serverError(w, err)
		return
	}
	t.User = *me
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": plain, "info": toAPITokenJSON(&t)})
}

// DELETE /admin/api/api-tokens/{id}
func (a *API) deleteAPIToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.db.Where("id = ? AND kind = ?", id, auth.KindAPI).Delete(&db.Token{}).Error; err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
