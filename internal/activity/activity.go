package activity

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const maxRows = 10000

var retentionDays atomic.Int32

func init() {
	retentionDays.Store(30)
}

// ClampRetentionDays limits n to 0–365.
func ClampRetentionDays(n int) int {
	return config.ClampRetentionDays(n)
}

// SetRetentionDays sets age-based prune (0 = no time prune). Clamped to 0–365.
func SetRetentionDays(n int) {
	retentionDays.Store(int32(ClampRetentionDays(n)))
}

// RetentionDays returns the current time-based retention in days.
func RetentionDays() int {
	return int(retentionDays.Load())
}

// Entry is a single audit event.
type Entry struct {
	ServerID uint
	Source   string // web | tui
	Actor    string
	Action   string
	Method   string
	Path     string
	Resource string
	Detail   string
	Status   string // ok | error
	IP       string
}

// Log writes an activity row. Never fails callers.
func Log(db *gorm.DB, e Entry) {
	if db == nil {
		return
	}
	if e.Source == "" {
		e.Source = "web"
	}
	if e.Status == "" {
		e.Status = "ok"
	}
	if e.Action == "" {
		e.Action = e.Method
	}
	row := storage.ActivityLog{
		CreatedAt: time.Now(),
		ServerID:  e.ServerID,
		Source:    truncate(e.Source, 16),
		Actor:     truncate(e.Actor, 128),
		Action:    truncate(e.Action, 64),
		Method:    truncate(e.Method, 16),
		Path:      truncate(e.Path, 512),
		Resource:  truncate(e.Resource, 256),
		Detail:    truncate(e.Detail, 4000),
		Status:    truncate(e.Status, 16),
		IP:        truncate(e.IP, 64),
	}
	_ = db.Create(&row).Error
	trim(db)
}

// Trim deletes activity older than RetentionDays (if > 0) and enforces the row cap.
func Trim(db *gorm.DB) {
	trim(db)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trim(db *gorm.DB) {
	if db == nil {
		return
	}
	if days := RetentionDays(); days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
		_ = db.Where("created_at < ?", cutoff).Delete(&storage.ActivityLog{}).Error
	}
	var count int64
	if err := db.Model(&storage.ActivityLog{}).Count(&count).Error; err != nil || count <= maxRows {
		return
	}
	var cutoff storage.ActivityLog
	if err := db.Order("id desc").Offset(maxRows).Limit(1).Find(&cutoff).Error; err != nil || cutoff.ID == 0 {
		return
	}
	_ = db.Where("id <= ?", cutoff.ID).Delete(&storage.ActivityLog{}).Error
}

// Filter for List queries.
type Filter struct {
	ServerID uint
	Source   string
	Actor    string
	Query    string
	Since    time.Time
	Limit    int
}

// List returns newest activity rows matching filter.
func List(db *gorm.DB, f Filter) ([]storage.ActivityLog, error) {
	if db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := db.Model(&storage.ActivityLog{}).Order("id desc")
	if f.ServerID > 0 {
		q = q.Where("server_id = ?", f.ServerID)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Actor != "" {
		q = q.Where("actor = ?", f.Actor)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		like := "%" + s + "%"
		q = q.Where("path LIKE ? OR action LIKE ? OR resource LIKE ? OR detail LIKE ?", like, like, like, like)
	}
	var rows []storage.ActivityLog
	err := q.Limit(limit).Find(&rows).Error
	return rows, err
}
