package adminapi

import (
	"net/http"
	"time"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// GET /admin/api/dashboard
func (a *API) dashboard(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)
	online := a.live.OnlineIDs(now)

	activeSessions := 0
	for _, id := range online {
		if d, ok := a.live.Get(id); ok {
			activeSessions += len(d.Conns)
		}
	}
	var total, sessions24h, alarms24h int64
	a.db.Model(&db.Device{}).Count(&total)
	a.db.Model(&db.AuditConn{}).Where("started_at >= ?", dayAgo).Count(&sessions24h)
	a.db.Model(&db.AuditAlarm{}).Where("created_at >= ?", dayAgo).Count(&alarms24h)

	var conns []db.AuditConn
	a.db.Where("peer_id <> ''").Order("id DESC").Limit(10).Find(&conns)
	ids := make([]string, 0, len(conns))
	for _, c := range conns {
		ids = append(ids, c.DeviceID)
	}
	names := a.hostnames(ids)
	recent := make([]auditConnJSON, 0, len(conns))
	for i := range conns {
		recent = append(recent, toAuditConnJSON(&conns[i], names[conns[i].DeviceID]))
	}

	var alarms []db.AuditAlarm
	a.db.Order("id DESC").Limit(10).Find(&alarms)

	// Count only online ids that are registered devices.
	var onlineKnown int64
	if len(online) > 0 {
		a.db.Model(&db.Device{}).Where("rustdesk_id IN ?", online).Count(&onlineKnown)
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"devices_total":   total,
		"devices_online":  onlineKnown,
		"active_sessions": activeSessions,
		"sessions_24h":    sessions24h,
		"alarms_24h":      alarms24h,
		"recent_sessions": recent,
		"recent_alarms":   a.alarmsJSON(alarms),
	})
}
