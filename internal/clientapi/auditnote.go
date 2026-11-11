package clientapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// End-of-session notes (flutter/lib/models/model.dart and common/widgets/dialog.dart):
// once connected, the controlling client asks for the audit GUID of its session,
// and when the session ends it may send a note for that GUID.

const maxAuditNote = 2000

// mayNote reports whether the logged-in client may see or annotate this session:
// it was the controlling side, or its user can access the controlled device.
func (a *API) mayNote(tok *db.Token, row *db.AuditConn) bool {
	if tok.DeviceID != "" && tok.DeviceID == row.PeerID {
		return true
	}
	var n int64
	a.accessibleDevices(&tok.User).Where("rustdesk_id = ?", row.DeviceID).Count(&n)
	return n > 0
}

// GET /api/audit/conn/active?id=&session_id=&conn_type=
// Replies with the GUID as a bare JSON string, or "" when there is no match.
func (a *API) auditConnActive(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, sid := q.Get("id"), q.Get("session_id")
	connType, err := strconv.Atoi(q.Get("conn_type"))
	if id == "" || sid == "" || err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad request")
		return
	}
	var row db.AuditConn
	if err := a.db.Where("device_id = ? AND session_id = ? AND type = ? AND closed_at IS NULL", id, sid, connType).
		Order("id DESC").Limit(1).Find(&row).Error; err != nil {
		logDBError("audit active", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	if row.ID == 0 || !a.mayNote(tokenFrom(r), &row) {
		httpx.JSON(w, http.StatusOK, "")
		return
	}
	httpx.JSON(w, http.StatusOK, row.GUID)
}

// PUT /api/audit {guid, note}
func (a *API) auditNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GUID string `json:"guid"`
		Note string `json:"note"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.GUID == "" {
		httpx.Error(w, http.StatusBadRequest, "bad request")
		return
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > maxAuditNote {
		note = string([]rune(note)[:maxAuditNote])
	}
	var row db.AuditConn
	if err := a.db.Where("guid = ?", req.GUID).Limit(1).Find(&row).Error; err != nil {
		logDBError("audit note load", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	if row.ID == 0 || !a.mayNote(tokenFrom(r), &row) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	if err := a.db.Model(&row).Update("note", note).Error; err != nil {
		logDBError("audit note save", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// switchGrantWindow is how far the device clock may be off before we hand it our time.
const switchGrantWindow = 5 * time.Minute

// POST /api/switch-grant {id, switch_code_verifier, timestamp, signature}
// The desktop client registers a grant before "switch sides" (src/hbbs_http/sync.rs).
// Nothing in the open-source hbbs or the client ever reads a grant back, and we
// don't have the device's public key to check the signature, so this only
// follows the reply contract so the client doesn't log errors.
func (a *API) switchGrant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == "" {
		httpx.Error(w, http.StatusBadRequest, "bad request")
		return
	}
	now := time.Now()
	ts, err := strconv.ParseInt(req.Timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(ts, 0)).Abs() > switchGrantWindow {
		httpx.JSON(w, http.StatusOK, map[string]any{"accepted": false, "server_time": now.Unix()})
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"accepted": true})
}
