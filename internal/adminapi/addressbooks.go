package adminapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"rustdesk-manager/internal/abook"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

type shareJSON struct {
	ID        uint     `json:"id"`
	User      *refJSON `json:"user"`
	UserGroup *refJSON `json:"user_group"`
	Rule      int      `json:"rule"`
}

type addressBookJSON struct {
	ID        uint        `json:"id"`
	GUID      string      `json:"guid"`
	Name      string      `json:"name"`
	Personal  bool        `json:"personal"`
	Owner     refJSON     `json:"owner"`
	Note      string      `json:"note"`
	PeerCount int64       `json:"peer_count"`
	Shares    []shareJSON `json:"shares"`
	UpdatedAt time.Time   `json:"updated_at"`
}

func (a *API) addressBookJSON(books []db.AddressBook) []addressBookJSON {
	ids := make([]uint, 0, len(books))
	for _, b := range books {
		ids = append(ids, b.ID)
	}
	type countRow struct {
		AddressBookID uint
		N             int64
	}
	var counts []countRow
	shares := []db.AbShare{}
	if len(ids) > 0 {
		a.db.Model(&db.AbPeer{}).Select("address_book_id, COUNT(*) AS n").Where("address_book_id IN ?", ids).
			Group("address_book_id").Scan(&counts)
		a.db.Where("address_book_id IN ?", ids).Order("id").Find(&shares)
	}
	peerCount := map[uint]int64{}
	for _, c := range counts {
		peerCount[c.AddressBookID] = c.N
	}
	userNames, groupNames := map[uint]string{}, map[uint]string{}
	var users []db.User
	var groups []db.UserGroup
	a.db.Select("id, name").Find(&users)
	a.db.Select("id, name").Find(&groups)
	for _, u := range users {
		userNames[u.ID] = u.Name
	}
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}
	byBook := map[uint][]shareJSON{}
	for _, s := range shares {
		sj := shareJSON{ID: s.ID, Rule: s.Rule}
		if s.UserID != nil {
			sj.User = &refJSON{*s.UserID, userNames[*s.UserID]}
		}
		if s.UserGroupID != nil {
			sj.UserGroup = &refJSON{*s.UserGroupID, groupNames[*s.UserGroupID]}
		}
		byBook[s.AddressBookID] = append(byBook[s.AddressBookID], sj)
	}
	out := make([]addressBookJSON, 0, len(books))
	for _, b := range books {
		sh := byBook[b.ID]
		if sh == nil {
			sh = []shareJSON{}
		}
		out = append(out, addressBookJSON{b.ID, b.GUID, b.Name, b.Personal, refJSON{b.OwnerID, userNames[b.OwnerID]},
			b.Note, peerCount[b.ID], sh, b.UpdatedAt})
	}
	return out
}

// GET /admin/api/address-books?kind=shared|personal
func (a *API) listAddressBooks(w http.ResponseWriter, r *http.Request) {
	personal := r.URL.Query().Get("kind") == "personal"
	var books []db.AddressBook
	if err := a.db.Where("personal = ?", personal).Order("name").Find(&books).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.addressBookJSON(books))
}

type addressBookReq struct {
	Name    *string `json:"name"`
	Note    *string `json:"note"`
	OwnerID *uint   `json:"owner_id"`
}

func (a *API) applyAddressBookReq(b *db.AddressBook, req addressBookReq) error {
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 100 {
			return errors.New("name must be 1-100 characters")
		}
		// Shared book names are unique: devices join a book by name (preset-address-book-name).
		var taken int64
		a.db.Model(&db.AddressBook{}).Where("name = ? AND personal = ? AND id <> ?", n, false, b.ID).Count(&taken)
		if taken > 0 {
			return errors.New("a shared address book with this name already exists")
		}
		b.Name = n
	}
	if req.Note != nil {
		b.Note = *req.Note
	}
	if req.OwnerID != nil {
		var n int64
		a.db.Model(&db.User{}).Where("id = ?", *req.OwnerID).Count(&n)
		if n == 0 {
			return errors.New("owner not found")
		}
		b.OwnerID = *req.OwnerID
	}
	return nil
}

// POST /admin/api/address-books {name, note?, owner_id?}: creates a shared book.
func (a *API) createAddressBook(w http.ResponseWriter, r *http.Request) {
	var req addressBookReq
	if err := httpx.Decode(r, &req); err != nil || req.Name == nil {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	b := db.AddressBook{GUID: uuid.NewString(), OwnerID: currentUser(r).ID}
	if err := a.applyAddressBookReq(&b, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Create(&b).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a.addressBookJSON([]db.AddressBook{b})[0])
}

func (a *API) loadAddressBook(w http.ResponseWriter, r *http.Request) (*db.AddressBook, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	var b db.AddressBook
	err := a.db.First(&b, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.Error(w, http.StatusNotFound, "address book not found")
		return nil, false
	}
	if err != nil {
		serverError(w, err)
		return nil, false
	}
	return &b, true
}

// GET /admin/api/address-books/{id}
func (a *API) getAddressBook(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, a.addressBookJSON([]db.AddressBook{*b})[0])
}

// PATCH /admin/api/address-books/{id} {name?, note?, owner_id?} (shared books only)
func (a *API) updateAddressBook(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	if b.Personal {
		httpx.Error(w, http.StatusBadRequest, "personal address books can't be renamed or reassigned")
		return
	}
	var req addressBookReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := a.applyAddressBookReq(b, req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.db.Save(b).Error; err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a.addressBookJSON([]db.AddressBook{*b})[0])
}

// DELETE /admin/api/address-books/{id}
func (a *API) deleteAddressBook(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	if err := a.abooks.DeleteBook(b); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /admin/api/address-books/{id}/shares [{user_id?|user_group_id?, rule}]
// Replaces all shares of a shared book.
func (a *API) setAddressBookShares(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	if b.Personal {
		httpx.Error(w, http.StatusBadRequest, "personal address books can't be shared")
		return
	}
	var req []struct {
		UserID      *uint `json:"user_id"`
		UserGroupID *uint `json:"user_group_id"`
		Rule        int   `json:"rule"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("address_book_id = ?", b.ID).Delete(&db.AbShare{}).Error; err != nil {
			return err
		}
		for _, s := range req {
			if (s.UserID == nil) == (s.UserGroupID == nil) {
				return errors.New("each share needs either a user or a user group")
			}
			if s.Rule < db.RuleRead || s.Rule > db.RuleFullControl {
				return errors.New("rule must be 1 (read), 2 (read/write) or 3 (full control)")
			}
			if err := tx.Create(&db.AbShare{AddressBookID: b.ID, UserID: s.UserID, UserGroupID: s.UserGroupID, Rule: s.Rule}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, a.addressBookJSON([]db.AddressBook{*b})[0])
}

type adminPeerJSON struct {
	abook.Peer
	HasPassword bool `json:"has_password"`
}

// GET /admin/api/address-books/{id}/peers?current=&pageSize=
// Passwords are never returned; has_password tells whether one is stored.
func (a *API) listAddressBookPeers(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	page := httpx.ParsePage(r)
	peers, total, err := a.abooks.Peers(b, page.Offset(), page.PageSize, false)
	if err != nil {
		serverError(w, err)
		return
	}
	tags, _ := a.abooks.Tags(b)
	out := make([]adminPeerJSON, 0, len(peers))
	for _, p := range peers {
		out = append(out, adminPeerJSON{p, p.HasPassword})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"total": total, "data": out, "tags": tags})
}

// abResult maps service errors to responses.
func abResult(w http.ResponseWriter, err error) {
	var ue abook.UserError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &ue):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	default:
		serverError(w, err)
	}
}

// POST /admin/api/address-books/{id}/peers {id, alias?, tags?, password?, note?}
func (a *API) addAddressBookPeer(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	var f abook.PeerFields
	if err := httpx.Decode(r, &f); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	f.Hash = nil // the web console only sets shared passwords
	abResult(w, a.abooks.AddPeer(b, f))
}

// PATCH /admin/api/address-books/{id}/peers/{peer} {alias?, tags?, password?, note?}
func (a *API) updateAddressBookPeer(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	var f abook.PeerFields
	if err := httpx.Decode(r, &f); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	f.ID, f.Hash = chi.URLParam(r, "peer"), nil
	abResult(w, a.abooks.UpdatePeer(b, f))
}

// DELETE /admin/api/address-books/{id}/peers/{peer}
func (a *API) deleteAddressBookPeer(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	abResult(w, a.abooks.DeletePeers(b, []string{chi.URLParam(r, "peer")}))
}

// POST /admin/api/address-books/{id}/tags {name, color}
func (a *API) addAddressBookTag(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	var t abook.Tag
	if err := httpx.Decode(r, &t); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	abResult(w, a.abooks.AddTag(b, t))
}

// DELETE /admin/api/address-books/{id}/tags/{name}
func (a *API) deleteAddressBookTag(w http.ResponseWriter, r *http.Request) {
	b, ok := a.loadAddressBook(w, r)
	if !ok {
		return
	}
	abResult(w, a.abooks.DeleteTags(b, []string{chi.URLParam(r, "name")}))
}
