package clientapi

import (
	"net/http"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// The client's "Accessible devices" tab (flutter/lib/models/group_model.dart) loads
// three paged lists. Visibility rules:
//   - admins see every user, device and device group;
//   - other users see themselves and the members of their user group, the devices
//     owned by those users, and the devices in device groups their user group may access.

// visibleUserIDs returns nil for "everyone" (admins).
func (a *API) visibleUserIDs(u *db.User) []uint {
	if u.IsAdmin {
		return nil
	}
	ids := []uint{u.ID}
	if u.UserGroupID != nil {
		var members []uint
		a.db.Model(&db.User{}).Where("user_group_id = ? AND id <> ?", *u.UserGroupID, u.ID).Pluck("id", &members)
		ids = append(ids, members...)
	}
	return ids
}

func (a *API) accessibleDeviceGroupIDs(u *db.User) []uint {
	if u.UserGroupID == nil {
		return nil
	}
	var ids []uint
	a.db.Model(&db.UserGroupAccess{}).Where("user_group_id = ?", *u.UserGroupID).Pluck("device_group_id", &ids)
	return ids
}

// GET /api/users?current=&pageSize=&accessible=&status=1
func (a *API) accessibleUsers(w http.ResponseWriter, r *http.Request) {
	u := &tokenFrom(r).User
	page := httpx.ParsePage(r)
	tx := a.db.Model(&db.User{}).Where("status = ?", db.UserNormal)
	if ids := a.visibleUserIDs(u); ids != nil {
		tx = tx.Where("id IN ?", ids)
	}
	var total int64
	tx.Count(&total)
	var rows []db.User
	if err := tx.Order("name").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		logDBError("accessible users", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	out := httpx.Paged[userPayload]{Total: total, Data: make([]userPayload, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, toUserPayload(&rows[i]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// peerPayload is the client's PeerPayload (flutter/lib/common/hbbs/hbbs.dart).
type peerPayload struct {
	ID              string         `json:"id"`
	Info            map[string]any `json:"info"` // username, os ("windows / ..."), device_name
	Status          int            `json:"status"`
	User            string         `json:"user"`
	UserName        string         `json:"user_name"` // owner; the client groups devices by it
	DeviceGroupName string         `json:"device_group_name"`
	Note            string         `json:"note"`
}

// accessibleDevices scopes a device query to what the user may see.
func (a *API) accessibleDevices(u *db.User) *gorm.DB {
	tx := a.db.Model(&db.Device{})
	if u.IsAdmin {
		return tx
	}
	cond := a.db.Where("user_id IN ?", a.visibleUserIDs(u))
	if groups := a.accessibleDeviceGroupIDs(u); len(groups) > 0 {
		cond = cond.Or("device_group_id IN ?", groups)
	}
	return tx.Where(cond)
}

// GET /api/peers?current=&pageSize=&accessible=&status=1
func (a *API) accessiblePeers(w http.ResponseWriter, r *http.Request) {
	u := &tokenFrom(r).User
	page := httpx.ParsePage(r)
	tx := a.accessibleDevices(u)
	var total int64
	tx.Count(&total)
	var rows []db.Device
	if err := tx.Preload("User").Preload("DeviceGroup").Order("rustdesk_id").
		Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		logDBError("accessible peers", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	out := httpx.Paged[peerPayload]{Total: total, Data: make([]peerPayload, 0, len(rows))}
	for _, d := range rows {
		p := peerPayload{
			ID:     d.RustdeskID,
			Info:   map[string]any{"username": d.DisplayUsername(), "os": d.OS, "device_name": d.DisplayHostname()},
			Status: 1,
			Note:   d.Note,
		}
		if d.User != nil {
			p.User, p.UserName = d.User.Name, d.User.Name
		}
		if d.DeviceGroup != nil {
			p.DeviceGroupName = d.DeviceGroup.Name
		}
		out.Data = append(out.Data, p)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /api/device-group/accessible?current=&pageSize=
func (a *API) accessibleDeviceGroups(w http.ResponseWriter, r *http.Request) {
	u := &tokenFrom(r).User
	page := httpx.ParsePage(r)
	tx := a.db.Model(&db.DeviceGroup{})
	if !u.IsAdmin {
		tx = tx.Where("id IN ?", append(a.accessibleDeviceGroupIDs(u), 0))
	}
	var total int64
	tx.Count(&total)
	var rows []db.DeviceGroup
	if err := tx.Order("name").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		logDBError("accessible device groups", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	type group struct {
		Name string `json:"name"`
	}
	out := httpx.Paged[group]{Total: total, Data: make([]group, 0, len(rows))}
	for _, g := range rows {
		out.Data = append(out.Data, group{g.Name})
	}
	httpx.JSON(w, http.StatusOK, out)
}
