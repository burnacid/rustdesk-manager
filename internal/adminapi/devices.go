package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/live"
)

type refJSON struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type deviceJSON struct {
	ID               uint       `json:"id"`
	RustdeskID       string     `json:"rustdesk_id"`
	Hostname         string     `json:"hostname"` // custom name if set, else reported
	ReportedHostname string     `json:"reported_hostname"`
	CustomHostname   string     `json:"custom_hostname"`
	OS               string     `json:"os"`
	CPU              string     `json:"cpu"`
	Memory           string     `json:"memory"`
	Username         string     `json:"username"`
	Version          string     `json:"version"`
	Note             string     `json:"note"`
	Group            *refJSON   `json:"group"`
	User             *refJSON   `json:"user"`
	Strategy         *refJSON   `json:"strategy"` // assigned directly (nil = inherited)
	Online           bool       `json:"online"`
	ActiveConns      []int64    `json:"active_conns"`
	LastSeenAt       *time.Time `json:"last_seen_at"`
	LastIP           string     `json:"last_ip"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (a *API) toDeviceJSON(d *db.Device, now time.Time) deviceJSON {
	out := deviceJSON{
		ID: d.ID, RustdeskID: d.RustdeskID, Hostname: d.DisplayHostname(), OS: d.OS, CPU: d.CPU,
		Memory: d.Memory, Username: d.DisplayUsername(), Version: d.Version, Note: d.Note,
		ReportedHostname: d.Hostname, CustomHostname: d.CustomHostname,
		LastSeenAt: d.LastSeenAt, LastIP: d.LastIP, CreatedAt: d.CreatedAt, ActiveConns: []int64{},
	}
	if d.DeviceGroup != nil {
		out.Group = &refJSON{d.DeviceGroup.ID, d.DeviceGroup.Name}
	}
	if d.User != nil {
		out.User = &refJSON{d.User.ID, d.User.Name}
	}
	if d.Strategy != nil {
		out.Strategy = &refJSON{d.Strategy.ID, d.Strategy.Name}
	}
	if l, ok := a.live.Get(d.RustdeskID); ok && now.Sub(l.LastSeen) < live.OnlineWindow {
		out.Online = true
		out.LastSeenAt = &l.LastSeen
		out.LastIP = l.IP
		if l.Conns != nil {
			out.ActiveConns = l.Conns
		}
	}
	return out
}

// GET /admin/api/devices?q=&status=online|offline&group=<id>&current=&pageSize=
func (a *API) listDevices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := httpx.ParsePage(r)
	now := time.Now().UTC()
	tx := a.db.Model(&db.Device{})
	if s := q.Get("q"); s != "" {
		p := likePattern(s)
		tx = tx.Where("(rustdesk_id LIKE ?"+likeEscape+" OR hostname LIKE ?"+likeEscape+
			" OR username LIKE ?"+likeEscape+" OR note LIKE ?"+likeEscape+")", p, p, p, p)
	}
	if g := q.Get("group"); g != "" {
		if g == "none" {
			tx = tx.Where("device_group_id IS NULL")
		} else {
			tx = tx.Where("device_group_id = ?", g)
		}
	}
	switch q.Get("status") {
	case "online":
		tx = tx.Where("rustdesk_id IN ?", append(a.live.OnlineIDs(now), ""))
	case "offline":
		tx = tx.Where("rustdesk_id NOT IN ?", append(a.live.OnlineIDs(now), ""))
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.Device
	if err := tx.Preload("DeviceGroup").Preload("User").Preload("Strategy").Order("last_seen_at DESC").
		Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	out := httpx.Paged[deviceJSON]{Total: total, Data: make([]deviceJSON, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, a.toDeviceJSON(&rows[i], now))
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (a *API) loadDevice(w http.ResponseWriter, r *http.Request) (*db.Device, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	var d db.Device
	err := a.db.Preload("DeviceGroup").Preload("User").Preload("Strategy").First(&d, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "device not found")
		return nil, false
	}
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return &d, true
}

// GET /admin/api/devices/{id}
// Includes the raw sysinfo, the open sessions and the last 20 sessions.
func (a *API) getDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC()
	dev := a.toDeviceJSON(d, now)
	var sysinfo map[string]any
	_ = json.Unmarshal([]byte(d.Sysinfo), &sysinfo)

	var recent []db.AuditConn
	a.db.Where("device_id = ?", d.RustdeskID).Order("id DESC").Limit(20).Find(&recent)
	sessions := make([]auditConnJSON, 0, len(recent))
	for i := range recent {
		sessions = append(sessions, toAuditConnJSON(&recent[i], d.Hostname))
	}
	// Live sessions: open audit rows for the conn ids the device reports as alive.
	active := []auditConnJSON{}
	if len(dev.ActiveConns) > 0 {
		var rows []db.AuditConn
		a.db.Where("device_id = ? AND conn_id IN ? AND closed_at IS NULL", d.RustdeskID, dev.ActiveConns).
			Order("id DESC").Find(&rows)
		for i := range rows {
			active = append(active, toAuditConnJSON(&rows[i], d.Hostname))
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"device": dev, "sysinfo": sysinfo, "uuid_bound": d.UUID != "", "strategy": a.effectiveStrategy(d),
		"active_sessions": active, "recent_sessions": sessions,
	})
}

// PATCH /admin/api/devices/{id} {note?, group_id?, user_id?, strategy_id?} (0 = none/inherit)
func (a *API) updateDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	var req struct {
		Note    *string `json:"note"`
		GroupID *uint   `json:"group_id"`
		UserID  *uint   `json:"user_id"`
		StratID *uint   `json:"strategy_id"`
		Name    *string `json:"custom_hostname"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	updates := map[string]any{}
	if req.Note != nil {
		updates["note"] = *req.Note
	}
	if req.GroupID != nil {
		if *req.GroupID == 0 {
			updates["device_group_id"] = nil
		} else {
			var n int64
			a.db.Model(&db.DeviceGroup{}).Where("id = ?", *req.GroupID).Count(&n)
			if n == 0 {
				httpx.Error(w, http.StatusBadRequest, "device group not found")
				return
			}
			updates["device_group_id"] = *req.GroupID
		}
	}
	if req.UserID != nil {
		if *req.UserID == 0 {
			updates["user_id"] = nil
		} else {
			var n int64
			a.db.Model(&db.User{}).Where("id = ?", *req.UserID).Count(&n)
			if n == 0 {
				httpx.Error(w, http.StatusBadRequest, "user not found")
				return
			}
			updates["user_id"] = *req.UserID
		}
	}
	if req.Name != nil {
		updates["custom_hostname"] = strings.TrimSpace(*req.Name)
	}
	if req.StratID != nil {
		if !a.strategyRefExists(*req.StratID) {
			httpx.Error(w, http.StatusBadRequest, "strategy not found")
			return
		}
		if *req.StratID == 0 {
			updates["strategy_id"] = nil
		} else {
			updates["strategy_id"] = *req.StratID
		}
	}
	if len(updates) > 0 {
		if err := a.db.Model(d).Updates(updates).Error; err != nil {
			serverError(w, err)
			return
		}
		a.strat.Forget(d.RustdeskID) // group or strategy may have changed
	}
	a.db.Preload("DeviceGroup").Preload("User").Preload("Strategy").First(d, d.ID)
	httpx.JSON(w, http.StatusOK, a.toDeviceJSON(d, time.Now().UTC()))
}

// DELETE /admin/api/devices/{id}
// Audit history is kept. A device that is still running re-registers on its next heartbeat.
func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	if err := a.db.Delete(d).Error; err != nil {
		serverError(w, err)
		return
	}
	a.devices.ForgetDevice(d.RustdeskID)
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/api/devices/{id}/disconnect {conn_ids?: []} (empty = all active sessions)
// Delivered with the device's next heartbeat (within ~3 seconds while a session is active).
func (a *API) disconnectDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	var req struct {
		ConnIDs []int64 `json:"conn_ids"`
	}
	_ = httpx.Decode(r, &req) // body is optional
	queued := a.live.RequestDisconnect(d.RustdeskID, req.ConnIDs)
	httpx.JSON(w, http.StatusOK, map[string]any{"queued": queued})
}

// POST /admin/api/devices/{id}/reset-uuid
// Lets the next machine that reports this id take it over (e.g. after replacing hardware).
func (a *API) resetDeviceUUID(w http.ResponseWriter, r *http.Request) {
	d, ok := a.loadDevice(w, r)
	if !ok {
		return
	}
	if err := a.db.Model(d).Update("uuid", "").Error; err != nil {
		serverError(w, err)
		return
	}
	a.devices.ForgetDevice(d.RustdeskID)
	w.WriteHeader(http.StatusNoContent)
}
