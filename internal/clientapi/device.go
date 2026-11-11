package clientapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/settings"
)

// Preset keys a device can send with sysinfo (set at install time).
// From reference/rustdesk/libs/base/src/config/keys.rs.
const (
	presetUsername        = "preset-user-name"
	presetStrategyName    = "preset-strategy-name"
	presetDeviceGroupName = "preset-device-group-name"
	presetNote            = "preset-note"

	presetAbName     = "preset-address-book-name"
	presetAbTag      = "preset-address-book-tag"
	presetAbAlias    = "preset-address-book-alias"
	presetAbPassword = "preset-address-book-password"
	presetAbNote     = "preset-address-book-note"
)

// POST /api/sysinfo
// Body: {cpu, memory, os, hostname, username?, version, id, uuid, preset-*...}
// Reply: plain text "SYSINFO_UPDATED" on success, "ID_NOT_FOUND" makes the client resend
// sooner; any other text makes it retry after 120s.
func (a *API) sysinfo(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := httpx.Decode(r, &raw); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	str := func(k string) string {
		s, _ := raw[k].(string)
		return strings.TrimSpace(s)
	}
	id, uuid := str("id"), str("uuid")
	if id == "" || uuid == "" || len(id) > 100 {
		http.Error(w, "missing id or uuid", http.StatusBadRequest)
		return
	}

	var dev db.Device
	if err := a.db.Where("rustdesk_id = ?", id).Limit(1).Find(&dev).Error; err != nil {
		logDBError("sysinfo load", err)
		writeText(w, "ERROR")
		return
	}
	if dev.ID != 0 && dev.UUID != "" && dev.UUID != uuid {
		// Another machine claims this id. Keep the first one until an admin resets the binding.
		slog.Warn("sysinfo uuid mismatch; ignoring", "id", id, "ip", clientIP(r))
		writeText(w, "UUID_MISMATCH")
		return
	}

	if dev.ID == 0 && a.settings.Bool(settings.RequireRegistered) {
		slog.Warn("unregistered device refused", "id", id, "ip", clientIP(r))
		writeText(w, "REGISTRATION_REQUIRED") // the client retries after two minutes
		return
	}

	body, _ := json.Marshal(raw)
	sum := sha256.Sum256(body)
	now := time.Now().UTC()
	isNew := dev.ID == 0
	dev.RustdeskID = id
	dev.UUID = uuid
	dev.Hostname = str("hostname")
	dev.OS = str("os")
	dev.CPU = str("cpu")
	dev.Memory = str("memory")
	dev.Username = str("username")
	dev.Version = str("version")
	dev.Sysinfo = string(body)
	dev.SysinfoHash = hex.EncodeToString(sum[:16])
	dev.LastIP = clientIP(r)
	dev.LastSeenAt = &now
	a.applyPresets(&dev, str)

	var err error
	if isNew {
		err = a.db.Create(&dev).Error
	} else {
		err = a.db.Save(&dev).Error
	}
	if err != nil {
		logDBError("sysinfo save", err)
		writeText(w, "ERROR")
		return
	}
	a.known.Store(id, uuid)
	a.strat.Forget(id) // presets may have changed its group or strategy
	if name := str(presetAbName); name != "" {
		if err := a.abooks.AddPreset(name, id, str(presetAbTag), str(presetAbAlias),
			str(presetAbPassword), str(presetAbNote)); err != nil {
			slog.Warn("preset address book", "id", id, "book", name, "err", err)
		}
	}
	if isNew {
		slog.Info("new device registered", "id", id, "hostname", dev.Hostname)
	}
	writeText(w, "SYSINFO_UPDATED")
}

// applyPresets assigns install-time presets, but only to fields an admin has not set.
// Groups, users and strategies must already exist: an unauthenticated device must
// not be able to create them.
func (a *API) applyPresets(dev *db.Device, str func(string) string) {
	if name := str(presetDeviceGroupName); name != "" && dev.DeviceGroupID == nil {
		var g db.DeviceGroup
		if a.db.Where("name = ?", name).Limit(1).Find(&g).Error == nil && g.ID != 0 {
			dev.DeviceGroupID = &g.ID
		}
	}
	if name := str(presetUsername); name != "" && dev.UserID == nil {
		var u db.User
		if a.db.Where("name = ?", name).Limit(1).Find(&u).Error == nil && u.ID != 0 {
			dev.UserID = &u.ID
		}
	}
	if name := str(presetStrategyName); name != "" && dev.StrategyID == nil {
		var s db.Strategy
		if a.db.Where("name = ?", name).Limit(1).Find(&s).Error == nil && s.ID != 0 {
			dev.StrategyID = &s.ID
		}
	}
	if note := str(presetNote); note != "" && dev.Note == "" {
		dev.Note = note
	}
}

// POST /api/sysinfo_ver
// The client only uses this against public servers, but answer it anyway.
func (a *API) sysinfoVer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	_ = httpx.Decode(r, &body)
	var dev db.Device
	if body.ID != "" {
		a.db.Select("sysinfo_hash").Where("rustdesk_id = ?", body.ID).Limit(1).Find(&dev)
	}
	writeText(w, dev.SysinfoHash)
}

type heartbeatReq struct {
	ID         string  `json:"id"`
	UUID       string  `json:"uuid"`
	Ver        int64   `json:"ver"`
	Conns      []int64 `json:"conns"`
	ModifiedAt int64   `json:"modified_at"`
}

// POST /api/heartbeat
// Body: {id, uuid, ver, conns?, modified_at}
// Reply JSON, all optional: {sysinfo: true} asks for a sysinfo upload,
// {disconnect: [conn_id]} closes sessions, {strategy: {config_options}, modified_at} pushes settings.
func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatReq
	if err := httpx.Decode(r, &req); err != nil || req.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp := map[string]any{}
	bound, err := a.boundUUID(req.ID)
	switch {
	case err != nil:
		logDBError("heartbeat", err)
	case bound == "":
		// Unknown device (new, or deleted by an admin): ask for its sysinfo,
		// unless only registered devices are accepted.
		if !a.settings.Bool(settings.RequireRegistered) {
			resp["sysinfo"] = true
		}
	case bound != req.UUID:
		// Not the machine this id is bound to; don't let it look online.
	default:
		now := time.Now().UTC()
		ip := clientIP(r)
		disconnect, persist := a.live.Heartbeat(req.ID, ip, req.Conns, now)
		if len(disconnect) > 0 {
			resp["disconnect"] = disconnect
		}
		if persist {
			a.db.Model(&db.Device{}).Where("rustdesk_id = ?", req.ID).
				Updates(map[string]any{"last_seen_at": now, "last_ip": ip})
		}
		if opts, version := a.strat.ForHeartbeat(req.ID, req.ModifiedAt); opts != nil {
			resp["strategy"] = map[string]any{"config_options": opts}
			resp["modified_at"] = version
		}
	}
	httpx.JSON(w, http.StatusOK, resp)
}
