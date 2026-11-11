// Package strategy resolves which settings each device should have and decides
// when to push them in the heartbeat response.
//
// Client behaviour (reference/rustdesk/src/hbbs_http/sync.rs):
//   - The heartbeat carries modified_at, the version the device last applied.
//   - A reply with {strategy:{config_options}, modified_at} sets those options;
//     keys not mentioned keep their value, and "" resets a key to its default.
//   - Keys in BLOCKED_CONFIG_OPTIONS are ignored by the client.
//
// So each push contains the strategy's options plus "" for keys this server set
// earlier but no longer manages. The version is a hash of that payload, which
// means nothing is re-sent until something actually changes.
package strategy

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
)

// Blocked mirrors BLOCKED_CONFIG_OPTIONS in the client: it refuses these keys.
var Blocked = []string{
	"custom-rendezvous-server", "rendezvous-servers", "api-server", "relay-server", "key", "ice-servers",
	"allow-insecure-tls-fallback", "allow-websocket", "websocket-port",
	"2fa", "bot", "preset-address-book-password", "enable-trusted-devices", "trusted-device-expiration-days",
	"preset-address-book-name", "preset-address-book-tag", "preset-address-book-alias", "preset-address-book-note",
	"preset-device-name", "preset-device-username", "preset-note",
	"windows-service-video-save-directory", "stop-service", "reg_recovery",
}

// Validate checks an options map as entered by an admin.
func Validate(opts map[string]string) error {
	for k, v := range opts {
		if k == "" || len(k) > 100 || strings.ContainsAny(k, " \t\r\n\"") {
			return fmt.Errorf("invalid option name %q", k)
		}
		if slices.Contains(Blocked, k) {
			return fmt.Errorf("%q can't be set by a strategy: the RustDesk client refuses it for security reasons", k)
		}
		if len(v) > 4000 {
			return fmt.Errorf("value of %q is too long", k)
		}
	}
	return nil
}

func Decode(s string) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

func Encode(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// Source tells where a device's strategy comes from.
type Source string

const (
	FromDevice  Source = "device"
	FromGroup   Source = "group"
	FromDefault Source = "default"
	FromNone    Source = "none"
)

type Effective struct {
	Strategy *db.Strategy
	Source   Source
	Group    *db.DeviceGroup // set when Source == FromGroup
}

// Resolve picks the device's strategy: its own, else its group's, else the default.
func Resolve(gdb *gorm.DB, dev *db.Device) Effective {
	load := func(id uint) *db.Strategy {
		var s db.Strategy
		if gdb.Limit(1).Find(&s, id).Error == nil && s.ID != 0 {
			return &s
		}
		return nil
	}
	if dev.StrategyID != nil {
		if s := load(*dev.StrategyID); s != nil {
			return Effective{Strategy: s, Source: FromDevice}
		}
	}
	if dev.DeviceGroupID != nil {
		var g db.DeviceGroup
		if gdb.Limit(1).Find(&g, *dev.DeviceGroupID).Error == nil && g.StrategyID != nil {
			if s := load(*g.StrategyID); s != nil {
				return Effective{Strategy: s, Source: FromGroup, Group: &g}
			}
		}
	}
	var s db.Strategy
	if gdb.Where("is_default = ?", true).Limit(1).Find(&s).Error == nil && s.ID != 0 {
		return Effective{Strategy: &s, Source: FromDefault}
	}
	return Effective{Source: FromNone}
}

// Version hashes a payload into a positive int64 that also fits a JavaScript number.
func Version(payload map[string]string) int64 {
	keys := slices.Sorted(maps.Keys(payload))
	h := fnv.New64a()
	for _, k := range keys {
		fmt.Fprintf(h, "%s\x00%s\x00", k, payload[k])
	}
	v := int64(h.Sum64() & (1<<53 - 1))
	if v == 0 {
		v = 1
	}
	return v
}

type entry struct {
	gen     uint64
	opts    map[string]string
	managed []string // keys pushed before, sorted
}

// Service caches each device's resolved options until an admin changes something.
type Service struct {
	db *gorm.DB

	mu       sync.Mutex
	gen      uint64
	cache    map[string]*entry
	reported map[string]int64 // last modified_at each device reported
}

func New(gdb *gorm.DB) *Service {
	return &Service{db: gdb, cache: map[string]*entry{}, reported: map[string]int64{}}
}

// Invalidate must be called after any change to strategies or their assignments.
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
}

// Forget drops one device's cached state (after its record changed).
func (s *Service) Forget(id string) {
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
}

func (s *Service) load(id string, gen uint64) (*entry, error) {
	var dev db.Device
	if err := s.db.Where("rustdesk_id = ?", id).Limit(1).Find(&dev).Error; err != nil || dev.ID == 0 {
		return nil, err
	}
	e := &entry{gen: gen, opts: map[string]string{}}
	if eff := Resolve(s.db, &dev); eff.Strategy != nil {
		e.opts = Decode(eff.Strategy.Options)
		for _, k := range Blocked {
			delete(e.opts, k) // in case of hand-edited data; the client would refuse them
		}
	}
	_ = json.Unmarshal([]byte(dev.StrategyKeys), &e.managed)
	sort.Strings(e.managed)
	return e, nil
}

func (s *Service) saveManaged(id string, keys []string) {
	b, _ := json.Marshal(keys)
	if err := s.db.Model(&db.Device{}).Where("rustdesk_id = ?", id).Update("strategy_keys", string(b)).Error; err != nil {
		slog.Error("save strategy keys", "id", id, "err", err)
	}
}

// ForHeartbeat returns the options to push to a device and their version, or nil
// when the device is up to date. clientVersion is the heartbeat's modified_at.
func (s *Service) ForHeartbeat(id string, clientVersion int64) (map[string]string, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reported[id] = clientVersion
	e := s.cache[id]
	if e == nil || e.gen != s.gen {
		var err error
		if e, err = s.load(id, s.gen); err != nil || e == nil {
			return nil, 0
		}
		s.cache[id] = e
	}

	payload := maps.Clone(e.opts)
	for _, k := range e.managed {
		if _, ok := payload[k]; !ok {
			payload[k] = "" // no longer managed: back to the client default
		}
	}
	if len(payload) == 0 {
		return nil, 0
	}
	version := Version(payload)
	current := slices.Sorted(maps.Keys(e.opts))

	if clientVersion == version {
		if slices.Equal(e.managed, current) {
			return nil, 0 // up to date
		}
		// Applied, including the resets. Stop tracking the reset keys and send the
		// remaining options under their own version right away (same values, so the
		// device sees no change; this just settles the version).
		e.managed = current
		s.saveManaged(id, current)
		if len(e.opts) == 0 {
			return nil, 0
		}
		return maps.Clone(e.opts), Version(e.opts)
	}
	// Remember every key before sending, so a later removal can still be reset.
	union := slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(e.managed), current...))))
	if !slices.Equal(e.managed, union) {
		e.managed = union
		s.saveManaged(id, union)
	}
	return payload, version
}

// Status reports whether a device has applied its current settings.
// pending=true means a push is waiting for the device's next heartbeat.
func (s *Service) Status(id string) (known, pending bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reported, ok := s.reported[id]
	if !ok {
		return false, false
	}
	e := s.cache[id]
	if e == nil || e.gen != s.gen {
		return true, true // changed since the last heartbeat
	}
	payload := maps.Clone(e.opts)
	for _, k := range e.managed {
		if _, ok := payload[k]; !ok {
			payload[k] = ""
		}
	}
	if len(payload) == 0 {
		return true, false
	}
	return true, reported != Version(payload)
}
