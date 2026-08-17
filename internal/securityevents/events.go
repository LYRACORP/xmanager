package securityevents

import (
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const (
	KindLoginOK   = "login_ok"
	KindLoginFail = "login_fail"
	KindSSHFail   = "ssh_fail"
	KindFail2ban  = "fail2ban"
	KindScan      = "scan"
	KindWAFBlock  = "waf_block"
	KindRateLimit = "rate_limit"
)

const maxEventRows = 20000
const eventRetentionDays = 30

// Entry is a security signal row.
type Entry struct {
	ServerID uint
	Kind     string
	IP       string
	Actor    string
	Detail   string
}

// Log writes a security event. Never fails callers.
func Log(db *gorm.DB, e Entry) {
	if db == nil || e.Kind == "" {
		return
	}
	row := storage.SecurityEvent{
		CreatedAt: time.Now(),
		ServerID:  e.ServerID,
		Kind:      truncate(e.Kind, 32),
		IP:        truncate(e.IP, 64),
		Actor:     truncate(e.Actor, 128),
		Detail:    truncate(e.Detail, 4000),
	}
	_ = db.Create(&row).Error
	trim(db)
}

// Filter for List.
type Filter struct {
	ServerID uint
	Kind     string
	Query    string
	Since    time.Time
	Limit    int
}

// List returns newest security events.
func List(db *gorm.DB, f Filter) ([]storage.SecurityEvent, error) {
	if db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := db.Model(&storage.SecurityEvent{}).Order("id desc")
	if f.ServerID > 0 {
		q = q.Where("server_id = ?", f.ServerID)
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at >= ?", f.Since)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		like := "%" + s + "%"
		q = q.Where("ip LIKE ? OR actor LIKE ? OR detail LIKE ?", like, like, like)
	}
	var rows []storage.SecurityEvent
	err := q.Limit(limit).Find(&rows).Error
	return rows, err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trim(db *gorm.DB) {
	cutoff := time.Now().AddDate(0, 0, -eventRetentionDays)
	_ = db.Where("created_at < ?", cutoff).Delete(&storage.SecurityEvent{}).Error
	var count int64
	if err := db.Model(&storage.SecurityEvent{}).Count(&count).Error; err != nil || count <= maxEventRows {
		return
	}
	var row storage.SecurityEvent
	if err := db.Order("id desc").Offset(maxEventRows).Limit(1).Find(&row).Error; err != nil || row.ID == 0 {
		return
	}
	_ = db.Where("id <= ?", row.ID).Delete(&storage.SecurityEvent{}).Error
}
