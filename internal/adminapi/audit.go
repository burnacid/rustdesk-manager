package adminapi

import (
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

type auditConnJSON struct {
	ID          uint       `json:"id"`
	GUID        string     `json:"guid"`
	DeviceID    string     `json:"device_id"`
	DeviceName  string     `json:"device_name"`
	ConnID      int64      `json:"conn_id"`
	IP          string     `json:"ip"`
	PeerID      string     `json:"peer_id"`
	PeerName    string     `json:"peer_name"`
	Type        int        `json:"type"`
	PrimaryAuth int        `json:"primary_auth"`
	TwoFactor   int        `json:"two_factor"`
	Note        string     `json:"note"`
	StartedAt   time.Time  `json:"started_at"`
	AuthedAt    *time.Time `json:"authed_at"`
	ClosedAt    *time.Time `json:"closed_at"`
}

func toAuditConnJSON(c *db.AuditConn, deviceName string) auditConnJSON {
	return auditConnJSON{
		ID: c.ID, GUID: c.GUID, DeviceID: c.DeviceID, DeviceName: deviceName, ConnID: c.ConnID,
		IP: c.IP, PeerID: c.PeerID, PeerName: c.PeerName, Type: c.Type, PrimaryAuth: c.PrimaryAuth,
		TwoFactor: c.TwoFactor, Note: c.Note, StartedAt: c.StartedAt, AuthedAt: c.AuthedAt, ClosedAt: c.ClosedAt,
	}
}

// hostnames maps rustdesk ids to hostnames for display.
func (a *API) hostnames(ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	var rows []db.Device
	a.db.Select("rustdesk_id, hostname, custom_hostname").Where("rustdesk_id IN ?", ids).Find(&rows)
	for i := range rows {
		out[rows[i].RustdeskID] = rows[i].DisplayHostname()
	}
	return out
}

// auditFilters applies ?device=&from=&to= to an audit query on the given time column.
func auditFilters(tx *gorm.DB, r *http.Request, timeCol string) *gorm.DB {
	q := r.URL.Query()
	if d := q.Get("device"); d != "" {
		tx = tx.Where("device_id = ?", d)
	}
	if t := parseTime(q.Get("from")); t != nil {
		tx = tx.Where(timeCol+" >= ?", *t)
	}
	if t := parseTime(q.Get("to")); t != nil {
		if len(q.Get("to")) == len("2006-01-02") {
			*t = t.Add(24 * time.Hour) // a date means "through the end of that day"
		}
		tx = tx.Where(timeCol+" < ?", *t)
	}
	return tx
}

// GET /admin/api/audit/conns?device=&peer=&from=&to=&current=&pageSize=
func (a *API) listAuditConns(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := auditFilters(a.db.Model(&db.AuditConn{}), r, "started_at")
	if p := r.URL.Query().Get("peer"); p != "" {
		tx = tx.Where("peer_id = ?", p)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.AuditConn
	if err := tx.Order("id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, c := range rows {
		ids = append(ids, c.DeviceID)
	}
	names := a.hostnames(ids)
	out := httpx.Paged[auditConnJSON]{Total: total, Data: make([]auditConnJSON, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, toAuditConnJSON(&rows[i], names[rows[i].DeviceID]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type auditFileJSON struct {
	ID         uint      `json:"id"`
	DeviceID   string    `json:"device_id"`
	DeviceName string    `json:"device_name"`
	PeerID     string    `json:"peer_id"`
	Type       int       `json:"type"`
	Path       string    `json:"path"`
	IsFile     bool      `json:"is_file"`
	Info       any       `json:"info"`
	CreatedAt  time.Time `json:"created_at"`
}

// GET /admin/api/audit/files?device=&from=&to=&current=&pageSize=
func (a *API) listAuditFiles(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := auditFilters(a.db.Model(&db.AuditFile{}), r, "created_at")
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.AuditFile
	if err := tx.Order("id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	ids := make([]string, 0, len(rows))
	for _, f := range rows {
		ids = append(ids, f.DeviceID)
	}
	names := a.hostnames(ids)
	out := httpx.Paged[auditFileJSON]{Total: total, Data: make([]auditFileJSON, 0, len(rows))}
	for _, f := range rows {
		out.Data = append(out.Data, auditFileJSON{f.ID, f.DeviceID, names[f.DeviceID], f.PeerID, f.Type,
			f.Path, f.IsFile, parseInfo(f.Info), f.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

type auditAlarmJSON struct {
	ID         uint      `json:"id"`
	DeviceID   string    `json:"device_id"`
	DeviceName string    `json:"device_name"`
	Type       int       `json:"type"`
	Info       any       `json:"info"`
	CreatedAt  time.Time `json:"created_at"`
}

// GET /admin/api/audit/alarms?device=&from=&to=&current=&pageSize=
func (a *API) listAuditAlarms(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := auditFilters(a.db.Model(&db.AuditAlarm{}), r, "created_at")
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.AuditAlarm
	if err := tx.Order("id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.Paged[auditAlarmJSON]{Total: total, Data: a.alarmsJSON(rows)})
}

func (a *API) alarmsJSON(rows []db.AuditAlarm) []auditAlarmJSON {
	ids := make([]string, 0, len(rows))
	for _, al := range rows {
		ids = append(ids, al.DeviceID)
	}
	names := a.hostnames(ids)
	out := make([]auditAlarmJSON, 0, len(rows))
	for _, al := range rows {
		out = append(out, auditAlarmJSON{al.ID, al.DeviceID, names[al.DeviceID], al.Type, parseInfo(al.Info), al.CreatedAt})
	}
	return out
}

// parseInfo turns the client's JSON-in-a-string info field into an object when possible.
func parseInfo(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}
