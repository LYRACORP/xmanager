package storage

import "gorm.io/gorm"

func runMigrations(db *gorm.DB) error {
	return db.AutoMigrate(
		&Server{},
		&ServerProfile{},
		&ServerMetricSnapshot{},
		&MetricSample{},
		&ErrorEvent{},
		&AlertRule{},
		&DeployHistory{},
		&AISession{},
		&Backup{},
		&BackupDestination{},
		&ActivityLog{},
		&AIConfigRecord{},
		&User{},
		&Project{},
		&ProjectDomain{},
		&ProjectEnvVar{},
		&GitCredential{},
		&GitOAuthApp{},
		&CronJob{},
		&CronRun{},
		&UptimeMonitor{},
		&UptimeEvent{},
		&AlertChannel{},
		&ServiceInstance{},
		&ScriptRun{},
		&DatabaseUser{},
		&ConnectedDomain{},
		&NodeSettings{},
		&Mailbox{},
		&ProjectDatabase{},
		&FTPUser{},
	)
}
