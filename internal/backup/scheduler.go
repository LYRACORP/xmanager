package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
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

// StartScheduler runs a background loop that checks for due backups every
// minute and executes them via the provided SSH pool. It blocks until ctx is
// done or stop is closed.
func (s *Scheduler) StartScheduler(pool *ssh.Pool, stop <-chan struct{}) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.runDue(pool)
		}
	}
}

func (s *Scheduler) runDue(pool *ssh.Pool) {
	backups, err := s.GetDueBackups()
	if err != nil {
		return
	}

	now := time.Now()
	for _, b := range backups {
		if !isDue(b, now) {
			continue
		}

		exec, ok := pool.GetExecutor(b.ServerID)
		if !ok {
			continue
		}

		runner := NewRunner(exec)
		destDir := "/var/backups/xmanager"
		_ = runner.EnsureDir(destDir)

		var (
			path   string
			size   int64
			runErr error
		)
		switch b.Type {
		case "postgres":
			path, size, runErr = runner.BackupPostgres(b.Service, destDir)
		case "mysql", "mariadb":
			path, size, runErr = runner.BackupMySQL(b.Service, destDir)
		case "mongodb":
			path, size, runErr = runner.BackupMongoDB(b.Service, destDir)
		case "volume":
			path, size, runErr = runner.BackupDockerVolume(b.Service, destDir)
		}

		status := "success"
		if runErr != nil {
			status = "failed"
		}

		record := storage.Backup{
			ServerID: b.ServerID,
			Type:     b.Type,
			Service:  b.Service,
			Path:     path,
			Size:     size,
			Schedule: b.Schedule,
			Status:   status,
			BackedAt: now,
		}
		_ = s.db.Create(&record).Error
	}
}

// isDue returns true if the backup schedule indicates it should run now.
// Schedule format: "@hourly", "@daily", "@weekly", or a simple interval like "1h", "24h".
func isDue(b storage.Backup, now time.Time) bool {
	if b.BackedAt.IsZero() {
		return true
	}
	var interval time.Duration
	switch strings.TrimSpace(b.Schedule) {
	case "@hourly":
		interval = time.Hour
	case "@daily":
		interval = 24 * time.Hour
	case "@weekly":
		interval = 7 * 24 * time.Hour
	case "@monthly":
		interval = 30 * 24 * time.Hour
	default:
		// try parsing as a Go duration string like "6h", "30m"
		d, err := time.ParseDuration(b.Schedule)
		if err == nil {
			interval = d
		} else {
			// try as plain hours integer
			h, err2 := strconv.Atoi(strings.TrimSuffix(b.Schedule, "h"))
			if err2 == nil {
				interval = time.Duration(h) * time.Hour
			} else {
				return false
			}
		}
	}
	return now.Sub(b.BackedAt) >= interval
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
