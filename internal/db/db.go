// Package db opens the database (SQLite or MySQL/MariaDB) and migrates the schema.
package db

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rustdesk-manager/internal/config"
)

func Open(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DBDriver {
	case "sqlite":
		// WAL + busy timeout let the heartbeat writers and the UI readers coexist.
		dialector = sqlite.Open(cfg.DBDSN + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	case "mysql":
		dialector = mysql.Open(cfg.DBDSN)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger:  logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", cfg.DBDriver, err)
	}
	if cfg.DBDriver == "sqlite" {
		// SQLite allows one writer at a time; a single connection avoids "database is locked".
		sqlDB, _ := gdb.DB()
		sqlDB.SetMaxOpenConns(1)
	}
	// Earlier versions created unused, always-empty tables with GORM's default
	// names for the OIDC models; remove them.
	for _, t := range []string{"o_id_c_providers", "o_id_c_identities", "o_id_c_sessions"} {
		if gdb.Migrator().HasTable(t) {
			_ = gdb.Migrator().DropTable(t)
		}
	}
	if err := gdb.AutoMigrate(allModels()...); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return gdb, nil
}

// Bootstrap creates the first admin user when the users table is empty.
func Bootstrap(gdb *gorm.DB, cfg *config.Config) error {
	var count int64
	if err := gdb.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	password := cfg.AdminPassword
	generated := password == ""
	if generated {
		password = config.RandomHex(8)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := User{Name: cfg.AdminUser, DisplayName: "Administrator", PasswordHash: string(hash), IsAdmin: true, Status: UserNormal}
	if err := gdb.Create(&admin).Error; err != nil {
		return err
	}
	if generated {
		slog.Warn("created initial admin user; change this password after first login",
			"user", admin.Name, "password", password)
	} else {
		slog.Info("created initial admin user", "user", admin.Name)
	}
	return ensureDefaultStrategy(gdb)
}

func ensureDefaultStrategy(gdb *gorm.DB) error {
	var count int64
	if err := gdb.Model(&Strategy{}).Where("is_default = ?", true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gdb.Create(&Strategy{Name: "Default", Options: "{}", IsDefault: true}).Error
	}
	return nil
}
