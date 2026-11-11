package clientapi

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/settings"
)

// Session recording upload (reference/rustdesk/src/hbbs_http/record_upload.rs).
//
// POST /api/record?type=new|part|tail|remove&file=<name>[&offset=&length=]
//   - new:    a recording starts
//   - part:   bytes to append at offset
//   - tail:   the final header, written over the start of the file (offset 0)
//   - remove: the client discarded the recording
//
// The stock RustDesk client ships with uploading switched off; only custom builds
// call this. Requests carry no login, so uploads are refused unless enabled in
// Settings, and only accepted for registered devices with the client's file name format.

// File names: {incoming|outgoing}_{id}_{yyyymmddHHMMSSmmm}_{display|camera}{n}_{codec}.{webm|mp4}
var recordName = regexp.MustCompile(`^(incoming|outgoing)_([A-Za-z0-9_-]{1,100})_\d{17}_(display|camera)\d{1,2}_[a-z0-9]{1,10}\.(webm|mp4)$`)

const maxRecording = 8 << 30 // 8 GB per file

func (a *API) record(w http.ResponseWriter, r *http.Request) {
	if !a.settings.Bool(settings.AcceptRecordings) {
		httpx.Error(w, http.StatusForbidden, "recording upload is disabled on this server")
		return
	}
	q := r.URL.Query()
	name := q.Get("file")
	m := recordName.FindStringSubmatch(name)
	if m == nil || filepath.Base(name) != name {
		httpx.Error(w, http.StatusBadRequest, "invalid file name")
		return
	}
	deviceID := m[2]
	if bound, err := a.boundUUID(deviceID); err != nil || bound == "" {
		httpx.Error(w, http.StatusForbidden, "unknown device")
		return
	}
	path := filepath.Join(a.cfg.RecordingsDir(), name)

	switch q.Get("type") {
	case "new":
		f, err := os.Create(path)
		if err != nil {
			a.recordFail(w, "create", err)
			return
		}
		f.Close()
		rec := db.Recording{File: name, DeviceID: deviceID}
		a.db.Where("file = ?", name).Delete(&db.Recording{})
		if err := a.db.Create(&rec).Error; err != nil {
			a.recordFail(w, "save", err)
			return
		}
	case "part", "tail":
		offset, err1 := strconv.ParseInt(q.Get("offset"), 10, 64)
		length, err2 := strconv.ParseInt(q.Get("length"), 10, 64)
		if err1 != nil || err2 != nil || offset < 0 || length < 0 || offset+length > maxRecording {
			httpx.Error(w, http.StatusBadRequest, "invalid offset or length")
			return
		}
		var rec db.Recording
		a.db.Where("file = ?", name).Limit(1).Find(&rec)
		if rec.ID == 0 {
			httpx.Error(w, http.StatusBadRequest, "recording was not started")
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, length+1))
		if err != nil || int64(len(data)) != length {
			httpx.Error(w, http.StatusBadRequest, "body does not match length")
			return
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0o640)
		if err != nil {
			a.recordFail(w, "open", err)
			return
		}
		_, err = f.WriteAt(data, offset)
		info, _ := f.Stat()
		f.Close()
		if err != nil {
			a.recordFail(w, "write", err)
			return
		}
		updates := map[string]any{"size": info.Size()}
		if q.Get("type") == "tail" {
			updates["completed"] = true
		}
		a.db.Model(&rec).Updates(updates)
	case "remove":
		a.db.Where("file = ?", name).Delete(&db.Recording{})
		_ = os.Remove(path)
	default:
		httpx.Error(w, http.StatusBadRequest, "invalid type")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *API) recordFail(w http.ResponseWriter, op string, err error) {
	slog.Error("recording upload", "op", op, "err", err)
	httpx.Error(w, http.StatusInternalServerError, "storage error")
}
