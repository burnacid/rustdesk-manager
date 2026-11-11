package clientapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// `rustdesk --assign --token <t> [--user_name ..] [--device_group_name ..] ...`
// (reference/rustdesk/src/core_main.rs). The client prints our reply: empty means "Done!".
//
// POST /api/devices/cli, Authorization: Bearer <deploy token>
// {id, uuid, user_name?, strategy_name?, address_book_name?, address_book_tag?,
//  address_book_alias?, address_book_password?, address_book_note?,
//  device_group_name?, note?, device_username?, device_name?}

type assignReq struct {
	ID                  string  `json:"id"`
	UUID                string  `json:"uuid"`
	UserName            *string `json:"user_name"`
	StrategyName        *string `json:"strategy_name"`
	AddressBookName     *string `json:"address_book_name"`
	AddressBookTag      string  `json:"address_book_tag"`
	AddressBookAlias    string  `json:"address_book_alias"`
	AddressBookPassword string  `json:"address_book_password"`
	AddressBookNote     string  `json:"address_book_note"`
	DeviceGroupName     *string `json:"device_group_name"`
	Note                *string `json:"note"`
	DeviceUsername      *string `json:"device_username"`
	DeviceName          *string `json:"device_name"`
}

// validDeployToken checks a deploy token from the Authorization header.
func (a *API) validDeployToken(r *http.Request) bool {
	plain, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(plain) == "" {
		return false
	}
	var t db.DeployToken
	a.db.Where("hash = ?", auth.HashToken(strings.TrimSpace(plain))).Limit(1).Find(&t)
	return t.ID != 0 && (t.ExpiresAt == nil || time.Now().UTC().Before(*t.ExpiresAt))
}

func plainError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

func (a *API) assignDevice(w http.ResponseWriter, r *http.Request) {
	if !a.validDeployToken(r) {
		plainError(w, http.StatusUnauthorized, "Invalid or expired deploy token.")
		return
	}
	var req assignReq
	if err := httpx.Decode(r, &req); err != nil || !validDeviceID(req.ID) || req.UUID == "" {
		plainError(w, http.StatusBadRequest, "Invalid request.")
		return
	}

	// Look everything up first, so a typo changes nothing.
	updates := map[string]any{}
	lookup := func(name *string, model any, what, col string) bool {
		if name == nil {
			return true
		}
		var id uint
		a.db.Model(model).Where("name = ?", *name).Limit(1).Pluck("id", &id)
		if id == 0 {
			plainError(w, http.StatusBadRequest, what+" '"+*name+"' not found on the server.")
			return false
		}
		updates[col] = id
		return true
	}
	if !lookup(req.UserName, &db.User{}, "User", "user_id") ||
		!lookup(req.StrategyName, &db.Strategy{}, "Strategy", "strategy_id") ||
		!lookup(req.DeviceGroupName, &db.DeviceGroup{}, "Device group", "device_group_id") {
		return
	}
	if req.AddressBookName != nil {
		var n int64
		a.db.Model(&db.AddressBook{}).Where("name = ? AND personal = ?", *req.AddressBookName, false).Count(&n)
		if n == 0 {
			plainError(w, http.StatusBadRequest, "Shared address book '"+*req.AddressBookName+"' not found on the server.")
			return
		}
	}
	if req.Note != nil {
		updates["note"] = *req.Note
	}
	if req.DeviceUsername != nil {
		updates["custom_username"] = *req.DeviceUsername
	}
	if req.DeviceName != nil {
		updates["custom_hostname"] = *req.DeviceName
	}

	// Register the device if it hasn't reported yet; this also binds its uuid.
	var dev db.Device
	a.db.Where("rustdesk_id = ?", req.ID).Limit(1).Find(&dev)
	if dev.ID != 0 && dev.UUID != "" && dev.UUID != req.UUID {
		plainError(w, http.StatusConflict, "This ID is registered to another machine. Reset its machine binding in the web console first.")
		return
	}
	if dev.ID == 0 {
		dev = db.Device{RustdeskID: req.ID, UUID: req.UUID}
		if err := a.db.Create(&dev).Error; err != nil {
			logDBError("assign create", err)
			plainError(w, http.StatusInternalServerError, "Server error.")
			return
		}
	} else if dev.UUID == "" {
		updates["uuid"] = req.UUID
	}
	if len(updates) > 0 {
		if err := a.db.Model(&dev).Updates(updates).Error; err != nil {
			logDBError("assign update", err)
			plainError(w, http.StatusInternalServerError, "Server error.")
			return
		}
	}
	if req.AddressBookName != nil {
		if err := a.abooks.AddPreset(*req.AddressBookName, req.ID, req.AddressBookTag, req.AddressBookAlias,
			req.AddressBookPassword, req.AddressBookNote); err != nil {
			logDBError("assign address book", err)
		}
	}
	a.known.Store(req.ID, req.UUID)
	a.strat.Forget(req.ID)
	slog.Info("device assigned", "id", req.ID, "ip", clientIP(r))
	w.WriteHeader(http.StatusOK) // empty body: the client prints "Done!"
}

// POST /api/devices/deploy: explicit deployment is a feature of RustDesk Server Pro's
// ID server (hbbs); with the open-source hbbs, devices don't need it.
func (a *API) deployDevice(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"result": "NOT_ENABLED"})
}

func validDeviceID(id string) bool {
	return id != "" && len(id) <= 100 && !strings.ContainsAny(id, " \t\r\n/\\")
}
