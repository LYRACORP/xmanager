package storage

import "gorm.io/gorm"

func runMigrations(db *gorm.DB) error {
	return db.AutoMigrate(
		&Server{},
		&ServerProfile{},
		&ErrorEvent{},
		&AlertRule{},
		&DeployHistory{},
		&AISession{},
		&Backup{},
		&AIConfigRecord{},
	)
}
