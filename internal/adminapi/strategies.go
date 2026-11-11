package adminapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/strategy"
)

type strategyJSON struct {
	ID          uint              `json:"id"`
	Name        string            `json:"name"`
	IsDefault   bool              `json:"is_default"`
	Options     map[string]string `json:"options"`
	DeviceCount int64             `json:"device_count"` // assigned directly
	Groups      []refJSON         `json:"groups"`       // device groups using it
	UpdatedAt   time.Time         `json:"updated_at"`
}

func (a *API) strategiesJSON(rows []db.Strategy) []strategyJSON {
	type countRow struct {
		StrategyID uint
		N          int64
	}
	var counts []countRow
	a.db.Model(&db.Device{}).Select("strategy_id, COUNT(*) AS n").Where("strategy_id IS NOT NULL").
		Group("strategy_id").Scan(&counts)
	devices := map[uint]int64{}
	for _, c := range counts {
		devices[c.StrategyID] = c.N
	}
	var groups []db.DeviceGroup
	a.db.Where("strategy_id IS NOT NULL").Order("name").Find(&groups)
	byStrategy := map[uint][]refJSON{}
	for _, g := range groups {
		byStrategy[*g.StrategyID] = append(byStrategy[*g.StrategyID], refJSON{g.ID, g.Name})
	}
	out := make([]strategyJSON, 0, len(rows))
	for _, s := range rows {
		gs := byStrategy[s.ID]
		if gs == nil {
			gs = []refJSON{}
		}
		out = append(out, strategyJSON{s.ID, s.Name, s.IsDefault, strategy.Decode(s.Options), devices[s.ID], gs, s.UpdatedAt})
	}
	return out
}

// GET /admin/api/strategies (default first)
func (a *API) listStrategies(w http.ResponseWriter, _ *http.Request) {
	var rows []db.Strategy
	if err := a.db.Order("is_default DESC, name").Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.strategiesJSON(rows))
}

type strategyReq struct {
	Name      *string            `json:"name"`
	Options   *map[string]string `json:"options"`
	IsDefault *bool              `json:"is_default"`
}

func (a *API) applyStrategyReq(s *db.Strategy, req strategyReq) error {
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 {
			return errors.New("name must be 1-100 characters")
		}
		var taken int64
		a.db.Model(&db.Strategy{}).Where("name = ? AND id <> ?", n, s.ID).Count(&taken)
		if taken > 0 {
			return errors.New("a strategy with this name already exists")
		}
		s.Name = n
	}
	if req.Options != nil {
		opts := *req.Options
		if err := strategy.Validate(opts); err != nil {
			return err
		}
		s.Options = strategy.Encode(opts)
	}
	return nil
}

// saveStrategy stores s; is_default=true moves the default flag to it.
func (a *API) saveStrategy(s *db.Strategy, makeDefault bool) error {
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if makeDefault {
			if err := tx.Model(&db.Strategy{}).Where("is_default = ? AND id <> ?", true, s.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
			s.IsDefault = true
		}
		return tx.Save(s).Error
	})
	a.strat.Invalidate()
	return err
}

// POST /admin/api/strategies {name, options, is_default?}
func (a *API) createStrategy(w http.ResponseWriter, r *http.Request) {
	var req strategyReq
	if err := httpx.Decode(r, &req); err != nil || req.Name == nil {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	s := db.Strategy{Options: "{}"}
	if err := a.applyStrategyReq(&s, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.saveStrategy(&s, req.IsDefault != nil && *req.IsDefault); err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a.strategiesJSON([]db.Strategy{s})[0])
}

func (a *API) loadStrategy(w http.ResponseWriter, r *http.Request) (*db.Strategy, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	var s db.Strategy
	err := a.db.First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "strategy not found")
		return nil, false
	}
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return &s, true
}

// PATCH /admin/api/strategies/{id} {name?, options?, is_default?: true}
// The default can only be moved to another strategy, not switched off.
func (a *API) updateStrategy(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadStrategy(w, r)
	if !ok {
		return
	}
	var req strategyReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.applyStrategyReq(s, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.saveStrategy(s, req.IsDefault != nil && *req.IsDefault); err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.strategiesJSON([]db.Strategy{*s})[0])
}

// DELETE /admin/api/strategies/{id}
// Devices and groups using it fall back to their group's or the default strategy.
func (a *API) deleteStrategy(w http.ResponseWriter, r *http.Request) {
	s, ok := a.loadStrategy(w, r)
	if !ok {
		return
	}
	if s.IsDefault {
		httpx.Error(w, http.StatusBadRequest, "make another strategy the default before deleting this one")
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.Device{}).Where("strategy_id = ?", s.ID).Update("strategy_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Model(&db.DeviceGroup{}).Where("strategy_id = ?", s.ID).Update("strategy_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(s).Error
	})
	a.strat.Invalidate()
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// strategyRefExists validates a strategy id from a PATCH body (0 = none).
func (a *API) strategyRefExists(id uint) bool {
	if id == 0 {
		return true
	}
	var n int64
	a.db.Model(&db.Strategy{}).Where("id = ?", id).Count(&n)
	return n > 0
}

type effectiveJSON struct {
	Strategy *refJSON `json:"strategy"`
	Source   string   `json:"source"` // device, group, default, none
	Group    *refJSON `json:"group,omitempty"`
	Status   string   `json:"status"` // applied, pending, unknown (no heartbeat since server start)
}

func (a *API) effectiveStrategy(d *db.Device) effectiveJSON {
	eff := strategy.Resolve(a.db, d)
	out := effectiveJSON{Source: string(eff.Source), Status: "unknown"}
	if eff.Strategy != nil {
		out.Strategy = &refJSON{eff.Strategy.ID, eff.Strategy.Name}
	}
	if eff.Group != nil {
		out.Group = &refJSON{eff.Group.ID, eff.Group.Name}
	}
	if known, pending := a.strat.Status(d.RustdeskID); known {
		out.Status = "applied"
		if pending {
			out.Status = "pending"
		}
	}
	return out
}
