package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

const minPasswordLen = 8

type userAdminJSON struct {
	ID          uint       `json:"id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Note        string     `json:"note"`
	IsAdmin     bool       `json:"is_admin"`
	TOTPEnabled bool       `json:"totp_enabled"`
	Status      int        `json:"status"`
	Group       *refJSON   `json:"group"`
	DeviceCount int64      `json:"device_count"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func toUserAdminJSON(u *db.User, devices int64) userAdminJSON {
	out := userAdminJSON{ID: u.ID, Name: u.Name, DisplayName: u.DisplayName, Email: u.Email, Note: u.Note,
		IsAdmin: u.IsAdmin, TOTPEnabled: u.TOTPEnabled, Status: u.Status, DeviceCount: devices, LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt}
	if u.UserGroup != nil {
		out.Group = &refJSON{u.UserGroup.ID, u.UserGroup.Name}
	}
	return out
}

// deviceCounts returns how many devices each user owns.
func (a *API) deviceCounts(userIDs []uint) map[uint]int64 {
	type row struct {
		UserID uint
		N      int64
	}
	var rows []row
	out := map[uint]int64{}
	if len(userIDs) == 0 {
		return out
	}
	a.db.Model(&db.Device{}).Select("user_id, COUNT(*) AS n").Where("user_id IN ?", userIDs).Group("user_id").Scan(&rows)
	for _, r := range rows {
		out[r.UserID] = r.N
	}
	return out
}

// GET /admin/api/users?q=&group=&current=&pageSize=
func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := a.db.Model(&db.User{})
	if s := r.URL.Query().Get("q"); s != "" {
		p := likePattern(s)
		tx = tx.Where("(name LIKE ?"+likeEscape+" OR display_name LIKE ?"+likeEscape+" OR email LIKE ?"+likeEscape+")", p, p, p)
	}
	if g := r.URL.Query().Get("group"); g != "" {
		tx = tx.Where("user_group_id = ?", g)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.User
	if err := tx.Preload("UserGroup").Order("name").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	ids := make([]uint, 0, len(rows))
	for _, u := range rows {
		ids = append(ids, u.ID)
	}
	counts := a.deviceCounts(ids)
	out := httpx.Paged[userAdminJSON]{Total: total, Data: make([]userAdminJSON, 0, len(rows))}
	for i := range rows {
		out.Data = append(out.Data, toUserAdminJSON(&rows[i], counts[rows[i].ID]))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// userReq is used for create (all fields) and update (only fields present).
type userReq struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Note        *string `json:"note"`
	Password    *string `json:"password"`
	IsAdmin     *bool   `json:"is_admin"`
	Status      *int    `json:"status"`
	GroupID     *uint   `json:"group_id"`   // 0 = no group
	ResetTOTP   bool    `json:"reset_totp"` // turn off the user's 2FA (lost phone)
}

// apply validates the request and copies it onto u.
func (a *API) applyUserReq(u *db.User, req userReq) error {
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 || strings.ContainsAny(n, " \t\r\n") {
			return errors.New("username must be 1-100 characters without spaces")
		}
		var taken int64
		a.db.Model(&db.User{}).Where("name = ? AND id <> ?", n, u.ID).Count(&taken)
		if taken > 0 {
			return errors.New("this username is already taken")
		}
		u.Name = n
	}
	if req.DisplayName != nil {
		u.DisplayName = strings.TrimSpace(*req.DisplayName)
	}
	if req.Email != nil {
		e := strings.TrimSpace(*req.Email)
		if e != "" {
			if _, err := mail.ParseAddress(e); err != nil {
				return errors.New("invalid email address")
			}
		}
		u.Email = e
	}
	if req.Note != nil {
		u.Note = *req.Note
	}
	if req.Password != nil {
		if len(*req.Password) < minPasswordLen {
			return errors.New("password must be at least 8 characters")
		}
		h, err := auth.HashPassword(*req.Password)
		if err != nil {
			return err
		}
		u.PasswordHash = h
	}
	if req.IsAdmin != nil {
		u.IsAdmin = *req.IsAdmin
	}
	if req.Status != nil {
		if *req.Status != db.UserNormal && *req.Status != db.UserDisabled {
			return errors.New("status must be 1 (enabled) or 0 (disabled)")
		}
		u.Status = *req.Status
	}
	if req.ResetTOTP {
		u.TOTPEnabled, u.TOTPSecret = false, ""
	}
	if req.GroupID != nil {
		if *req.GroupID == 0 {
			u.UserGroupID = nil
		} else {
			var n int64
			a.db.Model(&db.UserGroup{}).Where("id = ?", *req.GroupID).Count(&n)
			if n == 0 {
				return errors.New("user group not found")
			}
			id := *req.GroupID
			u.UserGroupID = &id
		}
		u.UserGroup = nil
	}
	return nil
}

// POST /admin/api/users
func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req userReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == nil || req.Password == nil {
		httpx.Error(w, http.StatusBadRequest, "username and password are required")
		return
	}
	u := db.User{Status: db.UserNormal}
	if err := a.applyUserReq(&u, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Create(&u).Error; err != nil {
		serverError(w, err)
		return
	}
	a.db.Preload("UserGroup").First(&u, u.ID)
	httpx.JSON(w, http.StatusCreated, toUserAdminJSON(&u, 0))
}

func (a *API) loadUser(w http.ResponseWriter, r *http.Request) (*db.User, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	var u db.User
	err := a.db.Preload("UserGroup").First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return nil, false
	}
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return &u, true
}

// otherActiveAdmins counts enabled admins other than id, to never lock everyone out.
func (a *API) otherActiveAdmins(id uint) int64 {
	var n int64
	a.db.Model(&db.User{}).Where("is_admin = ? AND status = ? AND id <> ?", true, db.UserNormal, id).Count(&n)
	return n
}

// PATCH /admin/api/users/{id}
// Changing the password or disabling the user signs them out everywhere.
func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadUser(w, r)
	if !ok {
		return
	}
	var req userReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	me := currentUser(r)
	losesAdmin := (req.IsAdmin != nil && !*req.IsAdmin) || (req.Status != nil && *req.Status != db.UserNormal)
	if losesAdmin && u.IsAdmin {
		if u.ID == me.ID {
			httpx.Error(w, http.StatusBadRequest, "you can't remove your own admin rights or disable yourself")
			return
		}
		if a.otherActiveAdmins(u.ID) == 0 {
			httpx.Error(w, http.StatusBadRequest, "at least one enabled admin must remain")
			return
		}
	}
	if err := a.applyUserReq(u, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("UserGroup").Save(u).Error; err != nil {
			return err
		}
		signOut := req.Password != nil || (req.Status != nil && *req.Status != db.UserNormal)
		if signOut {
			q := tx.Where("user_id = ?", u.ID)
			if u.ID == me.ID {
				// Keep the admin's current browser session when they change their own password.
				if c, err := r.Cookie(sessionCookie); err == nil {
					q = q.Where("hash <> ?", auth.HashToken(c.Value))
				}
			}
			return q.Delete(&db.Token{}).Error
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	a.db.Preload("UserGroup").First(u, u.ID)
	httpx.JSON(w, http.StatusOK, toUserAdminJSON(u, a.deviceCounts([]uint{u.ID})[u.ID]))
}

// DELETE /admin/api/users/{id}
// The user's devices stay registered without an owner; their logins are revoked.
// Their personal address book is deleted; shared books they own move to the admin.
func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadUser(w, r)
	if !ok {
		return
	}
	if u.ID == currentUser(r).ID {
		httpx.Error(w, http.StatusBadRequest, "you can't delete yourself")
		return
	}
	if u.IsAdmin && a.otherActiveAdmins(u.ID) == 0 {
		httpx.Error(w, http.StatusBadRequest, "at least one enabled admin must remain")
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.Device{}).Where("user_id = ?", u.ID).Update("user_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", u.ID).Delete(&db.Token{}).Error; err != nil {
			return err
		}
		if err := a.abooks.DeleteUserBooks(tx, u.ID, currentUser(r).ID); err != nil {
			return err
		}
		return tx.Delete(u).Error
	})
	if err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionJSON struct {
	ID         uint            `json:"id"`
	Kind       string          `json:"kind"`
	DeviceID   string          `json:"device_id"`
	DeviceInfo json.RawMessage `json:"device_info"`
	UserAgent  string          `json:"user_agent,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	LastUsedAt time.Time       `json:"last_used_at"`
	Current    bool            `json:"current"`
}

// GET /admin/api/users/{id}/sessions: RustDesk app logins and web console sessions.
func (a *API) listUserSessions(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadUser(w, r)
	if !ok {
		return
	}
	var rows []db.Token
	a.db.Where("user_id = ?", u.ID).Order("last_used_at DESC").Find(&rows)
	cur := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		cur = auth.HashToken(c.Value)
	}
	out := make([]sessionJSON, 0, len(rows))
	now := time.Now().UTC()
	for _, t := range rows {
		if t.ExpiresAt != nil && now.After(*t.ExpiresAt) {
			continue
		}
		s := sessionJSON{ID: t.ID, Kind: t.Kind, DeviceID: t.DeviceID, CreatedAt: t.CreatedAt,
			LastUsedAt: t.LastUsedAt, Current: t.Hash == cur}
		if t.Kind == auth.KindClient && json.Valid([]byte(t.DeviceInfo)) {
			s.DeviceInfo = json.RawMessage(t.DeviceInfo)
		} else {
			s.UserAgent = t.DeviceInfo
		}
		out = append(out, s)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// DELETE /admin/api/users/{id}/sessions/{sid}
func (a *API) revokeUserSession(w http.ResponseWriter, r *http.Request) {
	u, ok := a.loadUser(w, r)
	if !ok {
		return
	}
	sid := chi.URLParam(r, "sid")
	if err := a.db.Where("id = ? AND user_id = ?", sid, u.ID).Delete(&db.Token{}).Error; err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
