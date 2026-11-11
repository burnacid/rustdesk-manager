// Package settings stores runtime options edited in the web console.
package settings

import (
	"sync"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
)

const (
	// RequireRegistered refuses devices that weren't registered first with a deploy
	// token (`rustdesk --assign`).
	RequireRegistered = "devices.require_registered"
	// AcceptRecordings allows session recording uploads (POST /api/record).
	AcceptRecordings = "recordings.accept"
)

type Store struct {
	db    *gorm.DB
	mu    sync.RWMutex
	cache map[string]string
}

func New(gdb *gorm.DB) *Store { return &Store{db: gdb} }

func (s *Store) load() {
	if s.cache != nil {
		return
	}
	var rows []db.Setting
	s.db.Find(&rows)
	s.cache = map[string]string{}
	for _, r := range rows {
		s.cache[r.Key] = r.Value
	}
}

func (s *Store) Bool(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.cache[key] == "true"
}

func (s *Store) SetBool(key string, v bool) error {
	val := "false"
	if v {
		val = "true"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.db.Save(&db.Setting{Key: key, Value: val}).Error; err != nil {
		return err
	}
	s.load()
	s.cache[key] = val
	return nil
}
