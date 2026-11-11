// Package cleanup removes old data in the background: audit entries and
// recordings past their retention period, and expired logins and SSO attempts.
package cleanup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/settings"
)

const interval = time.Hour

// Run cleans up once shortly after start, then every hour, until ctx ends.
func Run(ctx context.Context, gdb *gorm.DB, store *settings.Store, recordingsDir string) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			Once(gdb, store, recordingsDir, time.Now().UTC())
			timer.Reset(interval)
		}
	}
}

// Once runs one cleanup pass. now is passed in for tests.
func Once(gdb *gorm.DB, store *settings.Store, recordingsDir string, now time.Time) {
	removed := map[string]int64{}
	del := func(name string, tx *gorm.DB) {
		if tx.Error != nil {
			slog.Error("cleanup", "what", name, "err", tx.Error)
			return
		}
		if tx.RowsAffected > 0 {
			removed[name] += tx.RowsAffected
		}
	}

	if days := store.Int(settings.AuditRetentionDays); days > 0 {
		cutoff := now.AddDate(0, 0, -days)
		del("sessions", gdb.Where("started_at < ?", cutoff).Delete(&db.AuditConn{}))
		del("file transfers", gdb.Where("created_at < ?", cutoff).Delete(&db.AuditFile{}))
		del("alarms", gdb.Where("created_at < ?", cutoff).Delete(&db.AuditAlarm{}))
	}
	if days := store.Int(settings.RecordingRetentionDays); days > 0 {
		var old []db.Recording
		gdb.Where("created_at < ?", now.AddDate(0, 0, -days)).Find(&old)
		for _, r := range old {
			if err := os.Remove(filepath.Join(recordingsDir, filepath.Base(r.File))); err != nil && !os.IsNotExist(err) {
				slog.Error("cleanup recording", "file", r.File, "err", err)
				continue
			}
			del("recordings", gdb.Delete(&r))
		}
	}
	del("expired logins", gdb.Where("expires_at IS NOT NULL AND expires_at < ?", now).Delete(&db.Token{}))
	del("sso attempts", gdb.Where("expires_at < ?", now).Delete(&db.OIDCSession{}))

	if len(removed) > 0 {
		args := []any{}
		for k, v := range removed {
			args = append(args, k, v)
		}
		slog.Info("cleanup removed old data", args...)
	}
}
