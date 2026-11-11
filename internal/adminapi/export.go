package adminapi

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
)

// GET /admin/api/audit/{kind}/export?device=&from=&to=  (kind: conns, files, alarms, admin)
// Streams a CSV with the same filters as the list. Times are UTC, RFC 3339.

var connTypes = map[int]string{0: "Remote control", 1: "File transfer", 2: "Port forward", 3: "Camera", 4: "Terminal"}
var primaryAuths = map[int]string{1: "Accepted by click", 2: "One-time password", 3: "Permanent password", 4: "Switch sides"}
var twoFactors = map[int]string{1: "2FA code", 2: "Trusted device"}
var alarmTypes = map[int]string{
	0: "Blocked by IP whitelist", 1: "Over 30 failed password attempts", 2: "6 failed attempts within 1 minute",
	6: "Many attempts from one IPv6 prefix", 7: "Terminal OS login back-off", 8: "Terminal OS login concurrency limit",
	9: "Session scope violation", 10: "Blocked by ID whitelist",
}

// cell guards against spreadsheet formula injection: peer names and paths come
// from remote machines, and a leading = + - @ would run as a formula in Excel.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func ts(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func label(m map[int]string, v int) string {
	if v == 0 {
		if s, ok := m[0]; ok {
			return s
		}
		return ""
	}
	if s, ok := m[v]; ok {
		return s
	}
	return strconv.Itoa(v)
}

func (a *API) exportAudit(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	var (
		model   any
		timeCol string
		header  []string
	)
	switch kind {
	case "conns":
		model, timeCol = &db.AuditConn{}, "started_at"
		header = []string{"Started", "Logged in", "Closed", "Device ID", "Device", "From ID", "From name", "IP", "Type", "Login", "Second factor", "Note"}
	case "files":
		model, timeCol = &db.AuditFile{}, "created_at"
		header = []string{"Time", "Device ID", "Device", "Peer ID", "Peer name", "IP", "Direction", "Path", "Items", "Files"}
	case "alarms":
		model, timeCol = &db.AuditAlarm{}, "created_at"
		header = []string{"Time", "Device ID", "Device", "Alarm", "Details"}
	case "admin":
		model = &db.AdminEvent{}
		header = []string{"Time", "User", "Action", "Method", "Route", "Target ID", "Status", "IP", "Via"}
	default:
		http.NotFound(w, r)
		return
	}
	var tx *gorm.DB
	if kind == "admin" {
		tx = adminEventFilters(a.db.Model(model), r)
	} else {
		tx = auditFilters(a.db.Model(model), r, timeCol)
	}
	if kind == "conns" {
		if p := r.URL.Query().Get("peer"); p != "" {
			tx = tx.Where("peer_id = ?", p)
		}
	}

	name := fmt.Sprintf("rustdesk-%s-%s.csv", map[string]string{"conns": "sessions", "files": "file-transfers", "alarms": "alarms", "admin": "admin-actions"}[kind],
		time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = w.Write([]byte("\xEF\xBB\xBF")) // BOM, so Excel reads UTF-8 correctly
	cw := csv.NewWriter(w)
	_ = cw.Write(header)

	// Device names, loaded once.
	names := map[string]string{}
	var devs []db.Device
	a.db.Select("rustdesk_id, hostname, custom_hostname").Find(&devs)
	for i := range devs {
		names[devs[i].RustdeskID] = devs[i].DisplayHostname()
	}

	var err error
	switch kind {
	case "conns":
		var batch []db.AuditConn
		err = tx.Order("id").FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, c := range batch {
				started := c.StartedAt
				_ = cw.Write([]string{ts(&started), ts(c.AuthedAt), ts(c.ClosedAt), cell(c.DeviceID), cell(names[c.DeviceID]),
					cell(c.PeerID), cell(c.PeerName), cell(c.IP), connTypeLabel(c), label(primaryAuths, c.PrimaryAuth),
					label(twoFactors, c.TwoFactor), cell(c.Note)})
			}
			cw.Flush()
			return cw.Error()
		}).Error
	case "files":
		var batch []db.AuditFile
		err = tx.Order("id").FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, f := range batch {
				var info struct {
					IP    string  `json:"ip"`
					Name  string  `json:"name"`
					Num   int     `json:"num"`
					Files [][]any `json:"files"`
				}
				_ = json.Unmarshal([]byte(f.Info), &info)
				var files []string
				for _, fl := range info.Files {
					if len(fl) == 2 {
						files = append(files, fmt.Sprintf("%v (%v bytes)", fl[0], fl[1]))
					}
				}
				dir := "Device to peer"
				if f.Type == 1 {
					dir = "Peer to device"
				}
				created := f.CreatedAt
				_ = cw.Write([]string{ts(&created), cell(f.DeviceID), cell(names[f.DeviceID]), cell(f.PeerID), cell(info.Name),
					cell(info.IP), dir, cell(f.Path), strconv.Itoa(info.Num), cell(strings.Join(files, "; "))})
			}
			cw.Flush()
			return cw.Error()
		}).Error
	case "admin":
		var batch []db.AdminEvent
		err = tx.Order("id").FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, e := range batch {
				at := e.CreatedAt
				_ = cw.Write([]string{ts(&at), cell(e.UserName), describeEvent(e.Method, e.Route), e.Method, cell(e.Route),
					cell(e.TargetID), strconv.Itoa(e.Status), cell(e.IP), e.Via})
			}
			cw.Flush()
			return cw.Error()
		}).Error
	case "alarms":
		var batch []db.AuditAlarm
		err = tx.Order("id").FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, al := range batch {
				created := al.CreatedAt
				_ = cw.Write([]string{ts(&created), cell(al.DeviceID), cell(names[al.DeviceID]),
					label(alarmTypes, al.Type), cell(al.Info)})
			}
			cw.Flush()
			return cw.Error()
		}).Error
	}
	cw.Flush()
	if err != nil {
		logError(err) // headers are already sent; the file ends early
	}
}

func connTypeLabel(c db.AuditConn) string {
	if c.AuthedAt == nil {
		return "" // never logged in
	}
	return label(connTypes, c.Type)
}
