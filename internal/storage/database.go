package storage

import (
	"fmt"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(dataDir string) (*gorm.DB, error) {
	dbPath := filepath.Join(dataDir, "xmanager.db")

	db, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("opening database at %s: %w", dbPath, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting underlying DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	var integrity string
	db.Raw("PRAGMA integrity_check").Scan(&integrity)
	if integrity != "ok" {
		return nil, fmt.Errorf("database integrity check failed: %s", integrity)
	}

	return db, nil
}
