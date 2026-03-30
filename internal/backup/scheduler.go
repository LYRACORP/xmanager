package backup

import (
	"fmt"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Scheduler struct {
	db *gorm.DB
}

func NewScheduler(db *gorm.DB) *Scheduler {
	return &Scheduler{db: db}
}

func (s *Scheduler) ListBackups(serverID uint) ([]storage.Backup, error) {
	var backups []storage.Backup
	query := s.db.Order("backed_at DESC")
	if serverID > 0 {
		query = query.Where("server_id = ?", serverID)
	}
	err := query.Limit(500).Find(&backups).Error
	return backups, err
}

func (s *Scheduler) CreateBackupRecord(serverID uint, backupType, service, path string, size int64) error {
	backup := storage.Backup{
		ServerID: serverID,
		Type:     backupType,
		Service:  service,
		Path:     path,
		Size:     size,
		Status:   "success",
		BackedAt: time.Now(),
	}
	return s.db.Create(&backup).Error
}

func (s *Scheduler) DeleteBackupRecord(id uint) error {
	return s.db.Delete(&storage.Backup{}, id).Error
}

func (s *Scheduler) UpdateSchedule(id uint, schedule string) error {
	return s.db.Model(&storage.Backup{}).Where("id = ?", id).
		Update("schedule", schedule).Error
}

func (s *Scheduler) GetDueBackups() ([]storage.Backup, error) {
	var backups []storage.Backup
	err := s.db.Where("schedule != '' AND schedule IS NOT NULL").Find(&backups).Error
	return backups, err
}

func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func FormatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
