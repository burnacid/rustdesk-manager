package clientapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"rustdesk-manager/internal/abook"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// Address book endpoints (reference/rustdesk/flutter/lib/models/ab_model.dart).
// Actions (add/update/delete) must answer 200 with an EMPTY body on success;
// anything else is shown to the user as an error.

func abOK(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) }

func (a *API) abFail(w http.ResponseWriter, err error) {
	var ue abook.UserError
	switch {
	case errors.Is(err, abook.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, abook.ErrReadOnly):
		httpx.Error(w, http.StatusForbidden, err.Error())
	case errors.As(err, &ue):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	default:
		logDBError("address book", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
	}
}

// openAb resolves {guid} for the logged-in user.
func (a *API) openAb(w http.ResponseWriter, r *http.Request, guid string, write bool) (*db.AddressBook, bool) {
	ab, _, err := a.abooks.Open(&tokenFrom(r).User, guid, write)
	if err != nil {
		a.abFail(w, err)
		return nil, false
	}
	return ab, true
}

// POST /api/ab/settings
func (a *API) abSettings(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"max_peer_one_ab": abook.MaxPeers})
}

// POST /api/ab/personal -> {guid}
func (a *API) abPersonal(w http.ResponseWriter, r *http.Request) {
	ab, err := a.abooks.Personal(&tokenFrom(r).User)
	if err != nil {
		logDBError("ab personal", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"guid": ab.GUID})
}

// POST /api/ab/shared/profiles?current=&pageSize=
func (a *API) abSharedProfiles(w http.ResponseWriter, r *http.Request) {
	all, err := a.abooks.SharedProfiles(&tokenFrom(r).User)
	if err != nil {
		logDBError("ab profiles", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	page := httpx.ParsePage(r)
	start := min(page.Offset(), len(all))
	end := min(start+page.PageSize, len(all))
	httpx.JSON(w, http.StatusOK, httpx.Paged[abook.Profile]{Total: int64(len(all)), Data: all[start:end]})
}

// POST /api/ab/peers?current=&pageSize=&ab=<guid>
func (a *API) abPeers(w http.ResponseWriter, r *http.Request) {
	ab, ok := a.openAb(w, r, r.URL.Query().Get("ab"), false)
	if !ok {
		return
	}
	page := httpx.ParsePage(r)
	peers, total, err := a.abooks.Peers(ab, page.Offset(), page.PageSize, true)
	if err != nil {
		logDBError("ab peers", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.Paged[abook.Peer]{Total: total, Data: peers})
}

// POST /api/ab/tags/{guid} -> [{name, color}]
func (a *API) abTags(w http.ResponseWriter, r *http.Request) {
	ab, ok := a.openAb(w, r, chi.URLParam(r, "guid"), false)
	if !ok {
		return
	}
	tags, err := a.abooks.Tags(ab)
	if err != nil {
		logDBError("ab tags", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	httpx.JSON(w, http.StatusOK, tags)
}

// abAction decodes the body into v, opens the book for writing and runs f.
func abAction[T any](a *API, w http.ResponseWriter, r *http.Request, f func(*db.AddressBook, T) error) {
	var v T
	if err := httpx.Decode(r, &v); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ab, ok := a.openAb(w, r, chi.URLParam(r, "guid"), true)
	if !ok {
		return
	}
	if err := f(ab, v); err != nil {
		a.abFail(w, err)
		return
	}
	a.db.Model(ab).Update("updated_at", time.Now().UTC())
	abOK(w)
}

// POST /api/ab/peer/add/{guid} {id, alias?, tags?, hash?|password?, note?, ...}
func (a *API) abPeerAdd(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.AddPeer)
}

// PUT /api/ab/peer/update/{guid} {id, <changed fields>}
func (a *API) abPeerUpdate(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.UpdatePeer)
}

// DELETE /api/ab/peer/{guid} ["id", ...]
func (a *API) abPeerDelete(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.DeletePeers)
}

// POST /api/ab/tag/add/{guid} {name, color}
func (a *API) abTagAdd(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.AddTag)
}

// PUT /api/ab/tag/rename/{guid} {old, new}
func (a *API) abTagRename(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, func(ab *db.AddressBook, v struct {
		Old string `json:"old"`
		New string `json:"new"`
	}) error {
		return a.abooks.RenameTag(ab, v.Old, v.New)
	})
}

// PUT /api/ab/tag/update/{guid} {name, color}
func (a *API) abTagUpdate(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.SetTagColor)
}

// DELETE /api/ab/tag/{guid} ["name", ...]
func (a *API) abTagDelete(w http.ResponseWriter, r *http.Request) {
	abAction(a, w, r, a.abooks.DeleteTags)
}

// Legacy single-blob address book, used only by old clients whose server lacks
// /api/ab/personal. Current clients never call it against this server.

// GET /api/ab -> {data: "<json>"} or null
func (a *API) abLegacyGet(w http.ResponseWriter, r *http.Request) {
	var row db.LegacyAb
	a.db.Where("user_id = ?", tokenFrom(r).UserID).Limit(1).Find(&row)
	if row.UserID == 0 {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": row.Data})
}

// POST /api/ab {data: "<json>"}
func (a *API) abLegacyPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data string `json:"data"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}
	row := db.LegacyAb{UserID: tokenFrom(r).UserID, Data: body.Data}
	if err := a.db.Save(&row).Error; err != nil {
		logDBError("legacy ab", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	abOK(w)
}
