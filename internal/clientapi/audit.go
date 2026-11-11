package clientapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// Audit replies follow the client's retry rules (src/server/connection.rs, post_audit_async):
//   - 2xx with an empty body = stored.
//   - 200 with {"error": ...} or a 5xx = retried for up to two minutes.
//   - other 4xx = dropped.
// Retries carry the same nonce, so a nonce we already have is answered as stored.

func auditOK(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }

func auditRetry(w http.ResponseWriter, op string, err error) {
	logDBError(op, err)
	httpx.JSON(w, http.StatusOK, map[string]string{"error": "storage failed"})
}

func nonceSeen(gdb *gorm.DB, model any, nonce *string) (bool, error) {
	if nonce == nil {
		return false, nil
	}
	var n int64
	err := gdb.Model(model).Where("nonce = ?", *nonce).Count(&n).Error
	return n > 0, err
}

func optNonce(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return nil
	}
	return &s
}

type connAuditReq struct {
	ID          string          `json:"id"`
	UUID        string          `json:"uuid"`
	ConnID      int64           `json:"conn_id"`
	SessionID   json.Number     `json:"session_id"` // u64, may not fit in int64
	Nonce       string          `json:"nonce"`
	Action      string          `json:"action"` // "new", "close", or empty for the login event
	IP          string          `json:"ip"`
	Peer        json.RawMessage `json:"peer"` // [peer_id, peer_name] on the login event
	Type        *int            `json:"type"`
	PrimaryAuth int             `json:"primary_auth"`
	TwoFactor   int             `json:"two_factor"`
}

// POST /api/audit/conn
// A session posts three events in order: {action:"new", ip}, then on login
// {peer:[id,name], type, primary_auth?, two_factor?}, then {action:"close"}.
func (a *API) auditConn(w http.ResponseWriter, r *http.Request) {
	var req connAuditReq
	if err := httpx.Decode(r, &req); err != nil || req.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	nonce := optNonce(req.Nonce)
	if seen, err := nonceSeen(a.db, &db.AuditConn{}, nonce); err != nil {
		auditRetry(w, "audit conn nonce", err)
		return
	} else if seen {
		auditOK(w)
		return
	}
	now := time.Now().UTC()

	// The open row for this connection, if any. conn_id restarts from 1 when the
	// client restarts, so only an unclosed row counts.
	var open db.AuditConn
	if err := a.db.Where("device_id = ? AND conn_id = ? AND closed_at IS NULL", req.ID, req.ConnID).
		Order("id DESC").Limit(1).Find(&open).Error; err != nil {
		auditRetry(w, "audit conn load", err)
		return
	}

	var err error
	switch {
	case req.Action == "new":
		if open.ID != 0 {
			// A stale row from before a client restart; close it.
			a.db.Model(&open).Update("closed_at", now)
		}
		err = a.db.Create(&db.AuditConn{
			GUID: uuid.NewString(), Nonce: nonce, DeviceID: req.ID, DeviceUUID: req.UUID,
			ConnID: req.ConnID, SessionID: req.SessionID.String(), IP: req.IP, StartedAt: now,
		}).Error

	case req.Action == "close":
		if open.ID != 0 {
			err = a.db.Model(&open).Update("closed_at", now).Error
		}

	case len(req.Peer) > 0:
		var peer []string
		_ = json.Unmarshal(req.Peer, &peer)
		row := open
		if row.ID == 0 {
			// Missed the "new" event (e.g. server was down); start the row here.
			row = db.AuditConn{GUID: uuid.NewString(), Nonce: nonce, DeviceID: req.ID,
				DeviceUUID: req.UUID, ConnID: req.ConnID, StartedAt: now}
		}
		if len(peer) > 0 {
			row.PeerID = peer[0]
		}
		if len(peer) > 1 {
			row.PeerName = peer[1]
		}
		if req.Type != nil {
			row.Type = *req.Type
		}
		row.PrimaryAuth, row.TwoFactor = req.PrimaryAuth, req.TwoFactor
		row.SessionID = req.SessionID.String()
		row.AuthedAt = &now
		err = a.db.Save(&row).Error
	}
	if err != nil {
		auditRetry(w, "audit conn save", err)
		return
	}
	auditOK(w)
}

type fileAuditReq struct {
	ID     string `json:"id"`
	PeerID string `json:"peer_id"`
	ConnID int64  `json:"conn_id"`
	Type   int    `json:"type"`
	Path   string `json:"path"`
	IsFile bool   `json:"is_file"`
	Info   string `json:"info"` // JSON string: {ip, name, num, files:[[name,size]]}
	Nonce  string `json:"nonce"`
}

// POST /api/audit/file
func (a *API) auditFile(w http.ResponseWriter, r *http.Request) {
	var req fileAuditReq
	if err := httpx.Decode(r, &req); err != nil || req.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	nonce := optNonce(req.Nonce)
	if seen, err := nonceSeen(a.db, &db.AuditFile{}, nonce); err != nil {
		auditRetry(w, "audit file nonce", err)
		return
	} else if seen {
		auditOK(w)
		return
	}
	err := a.db.Create(&db.AuditFile{
		Nonce: nonce, DeviceID: req.ID, PeerID: req.PeerID, ConnID: req.ConnID,
		Type: req.Type, Path: req.Path, IsFile: req.IsFile, Info: req.Info,
	}).Error
	if err != nil {
		auditRetry(w, "audit file save", err)
		return
	}
	auditOK(w)
}

type alarmAuditReq struct {
	ID     string `json:"id"`
	Typ    int    `json:"typ"`
	Info   string `json:"info"` // JSON string, contents depend on typ
	ConnID int64  `json:"conn_id"`
	Nonce  string `json:"nonce"`
}

// POST /api/audit/alarm
func (a *API) auditAlarm(w http.ResponseWriter, r *http.Request) {
	var req alarmAuditReq
	if err := httpx.Decode(r, &req); err != nil || req.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	nonce := optNonce(req.Nonce)
	if seen, err := nonceSeen(a.db, &db.AuditAlarm{}, nonce); err != nil {
		auditRetry(w, "audit alarm nonce", err)
		return
	} else if seen {
		auditOK(w)
		return
	}
	err := a.db.Create(&db.AuditAlarm{
		Nonce: nonce, DeviceID: req.ID, ConnID: req.ConnID, Type: req.Typ, Info: req.Info,
	}).Error
	if err != nil {
		auditRetry(w, "audit alarm save", err)
		return
	}
	auditOK(w)
}
