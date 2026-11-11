// Package live keeps short-lived device state in memory: last heartbeat,
// active connection IDs and disconnect requests waiting for the next heartbeat.
package live

import (
	"slices"
	"sync"
	"time"
)

// OnlineWindow is how long after the last heartbeat a device counts as online.
// Idle clients send a heartbeat every 15s, busy ones every 3s.
const OnlineWindow = 45 * time.Second

type Device struct {
	LastSeen  time.Time
	IP        string
	Conns     []int64
	persisted time.Time // last time LastSeen was written to the database
}

// PersistEvery limits how often last-seen times are written to the database.
const PersistEvery = time.Minute

type State struct {
	mu      sync.Mutex
	devices map[string]*Device
	pending map[string][]int64 // device id -> conn ids to disconnect
}

func New() *State {
	return &State{devices: map[string]*Device{}, pending: map[string][]int64{}}
}

// Heartbeat records a heartbeat. It returns (and clears) pending disconnects,
// and whether the last-seen time is due to be written to the database.
func (s *State) Heartbeat(id, ip string, conns []int64, now time.Time) (disconnect []int64, persist bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil {
		d = &Device{}
		s.devices[id] = d
	}
	persist = now.Sub(d.persisted) >= PersistEvery || d.IP != ip
	if persist {
		d.persisted = now
	}
	d.LastSeen, d.IP, d.Conns = now, ip, conns
	p := s.pending[id]
	delete(s.pending, id)
	// Only send conn ids that are still alive on the device.
	out := p[:0]
	for _, c := range p {
		if slices.Contains(conns, c) {
			out = append(out, c)
		}
	}
	return out, persist
}

func (s *State) Get(id string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok {
		return Device{}, false
	}
	return Device{LastSeen: d.LastSeen, IP: d.IP, Conns: slices.Clone(d.Conns)}, true
}

func (s *State) Online(id string, now time.Time) bool {
	d, ok := s.Get(id)
	return ok && now.Sub(d.LastSeen) < OnlineWindow
}

// OnlineIDs returns the ids of all devices seen within OnlineWindow.
func (s *State) OnlineIDs(now time.Time) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id, d := range s.devices {
		if now.Sub(d.LastSeen) < OnlineWindow {
			ids = append(ids, id)
		}
	}
	return ids
}

// RequestDisconnect queues conn ids to be closed. Empty connIDs means all live connections.
// It returns the conn ids that were queued.
func (s *State) RequestDisconnect(id string, connIDs []int64) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(connIDs) == 0 {
		if d, ok := s.devices[id]; ok {
			connIDs = slices.Clone(d.Conns)
		}
	}
	for _, c := range connIDs {
		if !slices.Contains(s.pending[id], c) {
			s.pending[id] = append(s.pending[id], c)
		}
	}
	return connIDs
}

func (s *State) Forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, id)
	delete(s.pending, id)
}
