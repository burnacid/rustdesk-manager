package adminapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

type userGroupJSON struct {
	ID           uint      `json:"id"`
	Name         string    `json:"name"`
	Note         string    `json:"note"`
	MemberCount  int64     `json:"member_count"`
	DeviceGroups []refJSON `json:"device_groups"` // device groups members can access
	CreatedAt    time.Time `json:"created_at"`
}

// GET /admin/api/user-groups
func (a *API) listUserGroups(w http.ResponseWriter, _ *http.Request) {
	var groups []db.UserGroup
	if err := a.db.Order("name").Find(&groups).Error; err != nil {
		serverError(w, err)
		return
	}
	type countRow struct {
		UserGroupID uint
		N           int64
	}
	var counts []countRow
	a.db.Model(&db.User{}).Select("user_group_id, COUNT(*) AS n").Where("user_group_id IS NOT NULL").
		Group("user_group_id").Scan(&counts)
	members := map[uint]int64{}
	for _, c := range counts {
		members[c.UserGroupID] = c.N
	}
	type accessRow struct {
		UserGroupID   uint
		DeviceGroupID uint
		Name          string
	}
	var access []accessRow
	a.db.Table("user_group_accesses").
		Select("user_group_accesses.user_group_id, user_group_accesses.device_group_id, device_groups.name").
		Joins("JOIN device_groups ON device_groups.id = user_group_accesses.device_group_id").
		Order("device_groups.name").Scan(&access)
	byGroup := map[uint][]refJSON{}
	for _, ac := range access {
		byGroup[ac.UserGroupID] = append(byGroup[ac.UserGroupID], refJSON{ac.DeviceGroupID, ac.Name})
	}
	out := make([]userGroupJSON, 0, len(groups))
	for _, g := range groups {
		dg := byGroup[g.ID]
		if dg == nil {
			dg = []refJSON{}
		}
		out = append(out, userGroupJSON{g.ID, g.Name, g.Note, members[g.ID], dg, g.CreatedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

type userGroupReq struct {
	Name           *string `json:"name"`
	Note           *string `json:"note"`
	DeviceGroupIDs *[]uint `json:"device_group_ids"`
}

func (a *API) saveUserGroup(g *db.UserGroup, req userGroupReq) error {
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 {
			return errors.New("name must be 1-100 characters")
		}
		var taken int64
		a.db.Model(&db.UserGroup{}).Where("name = ? AND id <> ?", n, g.ID).Count(&taken)
		if taken > 0 {
			return errors.New("a user group with this name already exists")
		}
		g.Name = n
	}
	if g.Name == "" {
		return errors.New("name is required")
	}
	if req.Note != nil {
		g.Note = *req.Note
	}
	return a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(g).Error; err != nil {
			return err
		}
		if req.DeviceGroupIDs == nil {
			return nil
		}
		if err := tx.Where("user_group_id = ?", g.ID).Delete(&db.UserGroupAccess{}).Error; err != nil {
			return err
		}
		for _, dg := range *req.DeviceGroupIDs {
			var n int64
			tx.Model(&db.DeviceGroup{}).Where("id = ?", dg).Count(&n)
			if n == 0 {
				continue // silently skip groups deleted meanwhile
			}
			if err := tx.Create(&db.UserGroupAccess{UserGroupID: g.ID, DeviceGroupID: dg}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// POST /admin/api/user-groups {name, note?, device_group_ids?}
func (a *API) createUserGroup(w http.ResponseWriter, r *http.Request) {
	var req userGroupReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	var g db.UserGroup
	if err := a.saveUserGroup(&g, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// PATCH /admin/api/user-groups/{id}
func (a *API) updateUserGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var g db.UserGroup
	if err := a.db.First(&g, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "user group not found")
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	var req userGroupReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.saveUserGroup(&g, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /admin/api/user-groups/{id} (members become ungrouped)
func (a *API) deleteUserGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.User{}).Where("user_group_id = ?", id).Update("user_group_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("user_group_id = ?", id).Delete(&db.UserGroupAccess{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_group_id = ?", id).Delete(&db.AbShare{}).Error; err != nil {
			return err
		}
		return tx.Delete(&db.UserGroup{}, id).Error
	})
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
