package adminapi

import (
	"net/http"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

const maxBulk = 1000

// POST /admin/api/devices/bulk {ids, action, value}
// action: "delete", "disconnect", "set_group", "set_user" or "set_strategy".
// For the set_* actions value is the id to assign, or 0 to clear it.
// Replies {affected}.
func (a *API) bulkDevices(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []uint `json:"ids"`
		Action string `json:"action"`
		Value  uint   `json:"value"`
	}
	if err := httpx.Decode(r, &req); err != nil || len(req.IDs) == 0 {
		httpx.Error(w, http.StatusBadRequest, "choose at least one device")
		return
	}
	if len(req.IDs) > maxBulk {
		httpx.Error(w, http.StatusBadRequest, "too many devices at once")
		return
	}
	switch req.Action {
	case "delete", "disconnect", "set_group", "set_user", "set_strategy":
	default:
		httpx.Error(w, http.StatusBadRequest, "unknown action")
		return
	}
	var devs []db.Device
	if err := a.db.Select("id, rustdesk_id").Where("id IN ?", req.IDs).Find(&devs).Error; err != nil {
		serverError(w, err)
		return
	}
	ids := make([]uint, 0, len(devs))
	for _, d := range devs {
		ids = append(ids, d.ID)
	}
	if len(ids) == 0 {
		httpx.JSON(w, http.StatusOK, map[string]any{"affected": 0})
		return
	}

	// For set_*: the column to change and the table the value must exist in.
	var col string
	var ref any
	switch req.Action {
	case "delete":
		if err := a.db.Where("id IN ?", ids).Delete(&db.Device{}).Error; err != nil {
			serverError(w, err)
			return
		}
		for _, d := range devs {
			a.devices.ForgetDevice(d.RustdeskID)
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"affected": len(ids)})
		return
	case "disconnect":
		n := 0
		for _, d := range devs {
			if len(a.live.RequestDisconnect(d.RustdeskID, nil)) > 0 {
				n++
			}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"affected": n})
		return
	case "set_group":
		col, ref = "device_group_id", &db.DeviceGroup{}
	case "set_user":
		col, ref = "user_id", &db.User{}
	case "set_strategy":
		col, ref = "strategy_id", &db.Strategy{}
	default:
		httpx.Error(w, http.StatusBadRequest, "unknown action")
		return
	}

	var value any
	if req.Value != 0 {
		var n int64
		a.db.Model(ref).Where("id = ?", req.Value).Count(&n)
		if n == 0 {
			httpx.Error(w, http.StatusBadRequest, "not found")
			return
		}
		value = req.Value
	}
	if err := a.db.Model(&db.Device{}).Where("id IN ?", ids).Update(col, value).Error; err != nil {
		serverError(w, err)
		return
	}
	for _, d := range devs {
		a.strat.Forget(d.RustdeskID)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"affected": len(ids)})
}
