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

type deviceGroupJSON struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Note        string    `json:"note"`
	DeviceCount int64     `json:"device_count"`
	Strategy    *refJSON  `json:"strategy"`
	CreatedAt   time.Time `json:"created_at"`
}

func groupJSON(g *db.DeviceGroup, devices int64) deviceGroupJSON {
	out := deviceGroupJSON{ID: g.ID, Name: g.Name, Note: g.Note, DeviceCount: devices, CreatedAt: g.CreatedAt}
	if g.Strategy != nil {
		out.Strategy = &refJSON{g.Strategy.ID, g.Strategy.Name}
	}
	return out
}

// GET /admin/api/device-groups (all groups with device counts; small lists, no paging)
func (a *API) listDeviceGroups(w http.ResponseWriter, _ *http.Request) {
	var groups []db.DeviceGroup
	if err := a.db.Preload("Strategy").Order("name").Find(&groups).Error; err != nil {
		serverError(w, err)
		return
	}
	type countRow struct {
		DeviceGroupID uint
		N             int64
	}
	var counts []countRow
	a.db.Model(&db.Device{}).Select("device_group_id, COUNT(*) AS n").
		Where("device_group_id IS NOT NULL").Group("device_group_id").Scan(&counts)
	byID := map[uint]int64{}
	for _, c := range counts {
		byID[c.DeviceGroupID] = c.N
	}
	out := make([]deviceGroupJSON, 0, len(groups))
	for i := range groups {
		out = append(out, groupJSON(&groups[i], byID[groups[i].ID]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

type groupReq struct {
	Name       *string `json:"name"`
	Note       *string `json:"note"`
	StrategyID *uint   `json:"strategy_id"` // 0 = use the default strategy
}

func (g groupReq) cleanName() (string, error) {
	if g.Name == nil {
		return "", errors.New("name is required")
	}
	n := strings.TrimSpace(*g.Name)
	if n == "" || len(n) > 100 {
		return "", errors.New("name must be 1-100 characters")
	}
	return n, nil
}

func (a *API) groupNameTaken(name string, exceptID uint) bool {
	var n int64
	a.db.Model(&db.DeviceGroup{}).Where("name = ? AND id <> ?", name, exceptID).Count(&n)
	return n > 0
}

// POST /admin/api/device-groups {name, note?}
func (a *API) createDeviceGroup(w http.ResponseWriter, r *http.Request) {
	var req groupReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	name, err := req.cleanName()
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if a.groupNameTaken(name, 0) {
		httpx.Error(w, http.StatusConflict, "a group with this name already exists")
		return
	}
	g := db.DeviceGroup{Name: name}
	if req.Note != nil {
		g.Note = *req.Note
	}
	if !a.setGroupStrategy(&g, req.StrategyID) {
		httpx.Error(w, http.StatusBadRequest, "strategy not found")
		return
	}
	if err := a.db.Create(&g).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, groupJSON(&g, 0))
}

// PATCH /admin/api/device-groups/{id} {name?, note?}
func (a *API) updateDeviceGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var g db.DeviceGroup
	if err := a.db.First(&g, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "group not found")
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	var req groupReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name != nil {
		name, err := req.cleanName()
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if a.groupNameTaken(name, g.ID) {
			httpx.Error(w, http.StatusConflict, "a group with this name already exists")
			return
		}
		g.Name = name
	}
	if req.Note != nil {
		g.Note = *req.Note
	}
	if !a.setGroupStrategy(&g, req.StrategyID) {
		httpx.Error(w, http.StatusBadRequest, "strategy not found")
		return
	}
	if err := a.db.Omit("Strategy").Save(&g).Error; err != nil {
		serverError(w, err)
		return
	}
	a.strat.Invalidate()
	a.db.Preload("Strategy").First(&g, g.ID)
	httpx.JSON(w, http.StatusOK, groupJSON(&g, 0))
}

// setGroupStrategy applies an optional strategy_id; false if it doesn't exist.
func (a *API) setGroupStrategy(g *db.DeviceGroup, id *uint) bool {
	if id == nil {
		return true
	}
	if !a.strategyRefExists(*id) {
		return false
	}
	g.Strategy = nil
	if *id == 0 {
		g.StrategyID = nil
	} else {
		v := *id
		g.StrategyID = &v
	}
	return true
}

// DELETE /admin/api/device-groups/{id} (devices in it become ungrouped)
func (a *API) deleteDeviceGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.Device{}).Where("device_group_id = ?", id).
			Update("device_group_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("device_group_id = ?", id).Delete(&db.UserGroupAccess{}).Error; err != nil {
			return err
		}
		return tx.Delete(&db.DeviceGroup{}, id).Error
	})
	a.strat.Invalidate()
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
