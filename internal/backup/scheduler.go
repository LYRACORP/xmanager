package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const StatusScheduled = "scheduled"

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

// GetDueBackups returns schedule templates (status=scheduled with a schedule expression).
func (s *Scheduler) GetDueBackups() ([]storage.Backup, error) {
	var backups []storage.Backup
	err := s.db.Where("status = ? AND schedule != '' AND schedule IS NOT NULL", StatusScheduled).Find(&backups).Error
	return backups, err
}

// StartScheduler runs a background loop that checks for due backups every
// minute and executes them via the provided SSH pool (falls back to local exec).
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

func (s *Scheduler) executorFor(pool *ssh.Pool, serverID uint) *ssh.Executor {
	if pool != nil {
		if exec, ok := pool.GetExecutor(serverID); ok && exec != nil {
			return exec
		}
	}
	// Node panel: jobs target the local host.
	return ssh.NewLocalExecutor()
}

func (s *Scheduler) runDue(pool *ssh.Pool) {
	backups, err := s.GetDueBackups()
	if err != nil {
		return
	}

	now := time.Now()
	for _, b := range backups {
		if !IsDue(b, now) {
			continue
		}
		exec := s.executorFor(pool, b.ServerID)
		s.RunSchedule(exec, b, now)
	}
}

// RunSchedule executes one schedule template and records history (without schedule).
func (s *Scheduler) RunSchedule(exec *ssh.Executor, job storage.Backup, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	destIDs := parseDestIDs(job.Destinations)

	var targets []Target
	if job.Type == "all" || job.Service == "__all__" {
		targets = ListDumpable(exec)
	} else {
		targets = []Target{{Type: dbmanager.DBType(job.Type), Name: job.Service}}
	}

	for _, t := range targets {
		res := RunOne(exec, t.Type, t.Name, DefaultDir)
		destLabels := []string{"local"}
		var deliverErrs []string
		if res.Err == nil && len(destIDs) > 0 && s.db != nil {
			for _, id := range destIDs {
				var d storage.BackupDestination
				if err := s.db.Where("server_id = ? AND id = ? AND enabled = ?", job.ServerID, id, true).First(&d).Error; err != nil {
					continue
				}
				if err := Deliver(exec, s.db, d, res.Path, res.Filename); err != nil {
					deliverErrs = append(deliverErrs, d.Name+": "+err.Error())
				} else {
					destLabels = append(destLabels, d.Type+":"+d.Name)
				}
			}
		}
		if len(deliverErrs) > 0 && res.Err == nil {
			res.Err = fmt.Errorf("delivered with errors: %s", strings.Join(deliverErrs, "; "))
		}
		Record(s.db, job.ServerID, res, strings.Join(destLabels, ","))
	}

	_ = s.db.Model(&storage.Backup{}).Where("id = ?", job.ID).Update("backed_at", now).Error
}

func parseDestIDs(raw string) []uint {
	var out []uint
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "local" {
			continue
		}
		// accept "12" or "dest:12"
		if i := strings.LastIndex(part, ":"); i >= 0 {
			part = part[i+1:]
		}
		id, err := strconv.ParseUint(part, 10, 64)
		if err == nil && id > 0 {
			out = append(out, uint(id))
		}
	}
	return out
}

// CreateSchedule inserts a schedule template row.
func CreateSchedule(db *gorm.DB, serverID uint, dbType, service, schedule, destinations string) (*storage.Backup, error) {
	schedule = strings.TrimSpace(schedule)
	if schedule == "" {
		return nil, fmt.Errorf("schedule required")
	}
	if !ValidSchedule(schedule) {
		return nil, fmt.Errorf("invalid schedule %q (use @hourly, @daily, @weekly, @monthly, or durations like 6h)", schedule)
	}
	dbType = strings.TrimSpace(dbType)
	service = strings.TrimSpace(service)
	if dbType == "" {
		return nil, fmt.Errorf("database type required")
	}
	rec := storage.Backup{
		ServerID:     serverID,
		Type:         dbType,
		Service:      service,
		Schedule:     schedule,
		Destinations: destinations,
		Status:       StatusScheduled,
		BackedAt:     time.Time{}, // due immediately on next tick
	}
	if err := db.Create(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// ValidSchedule reports whether expression is supported by IsDue.
func ValidSchedule(s string) bool {
	s = strings.TrimSpace(s)
	switch s {
	case "@hourly", "@daily", "@weekly", "@monthly":
		return true
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d > 0
	}
	if h, err := strconv.Atoi(strings.TrimSuffix(s, "h")); err == nil && h > 0 {
		return true
	}
	return false
}

// IsDue returns true if the backup schedule indicates it should run now.
// Schedule format: "@hourly", "@daily", "@weekly", "@monthly", or a duration like "6h", "30m".
func IsDue(b storage.Backup, now time.Time) bool {
	if strings.TrimSpace(b.Schedule) == "" {
		return false
	}
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
		d, err := time.ParseDuration(b.Schedule)
		if err == nil {
			interval = d
		} else {
			h, err2 := strconv.Atoi(strings.TrimSuffix(b.Schedule, "h"))
			if err2 != nil {
				return false
			}
			interval = time.Duration(h) * time.Hour
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
	if t.IsZero() {
		return "never"
	}
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
