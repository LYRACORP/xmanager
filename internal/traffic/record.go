package traffic

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const (
	SourcePanel = "panel"
	SourceNginx = "nginx"

	maxHitRows       = 50000
	hitRetentionDays = 7
)

// Hit is one traffic sample input.
type Hit struct {
	ServerID uint
	Source   string
	IP       string
	Method   string
	Path     string
	Status   int
	Blocked  bool
	Rule     string
}

// Recorder stores traffic hits.
type Recorder struct {
	mu       sync.RWMutex
	db       *gorm.DB
	serverID uint
	policy   security.Policy
}

// NewRecorder creates a traffic recorder.
func NewRecorder(db *gorm.DB, serverID uint) *Recorder {
	return &Recorder{db: db, serverID: serverID, policy: security.LoadPolicy(db, serverID)}
}

// Reload refreshes policy.
func (r *Recorder) Reload() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.policy = security.LoadPolicy(r.db, r.serverID)
	r.mu.Unlock()
}

func (r *Recorder) analysisOn() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policy.Enabled && r.policy.AnalysisEnabled
}

// Record stores a hit when analysis is enabled.
func (r *Recorder) Record(h Hit) {
	if r == nil || r.db == nil || !r.analysisOn() {
		return
	}
	row := storage.TrafficHit{
		At:       time.Now(),
		ServerID: h.ServerID,
		Source:   truncate(h.Source, 16),
		IP:       truncate(h.IP, 64),
		Method:   truncate(h.Method, 16),
		Path:     truncate(h.Path, 512),
		Status:   h.Status,
		Blocked:  h.Blocked,
		Rule:     truncate(h.Rule, 64),
	}
	if row.ServerID == 0 {
		row.ServerID = r.serverID
	}
	_ = r.db.Create(&row).Error
	trim(r.db)
}

// Middleware records panel traffic after handler runs.
func (r *Recorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r == nil || next == nil {
			if next != nil {
				next.ServeHTTP(w, req)
			}
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, req)
		if !r.analysisOn() {
			return
		}
		ip := clientIP(req)
		r.Record(Hit{
			ServerID: r.serverID,
			Source:   SourcePanel,
			IP:       ip,
			Method:   req.Method,
			Path:     req.URL.Path,
			Status:   rec.status,
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	return r.RemoteAddr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trim(db *gorm.DB) {
	cutoff := time.Now().AddDate(0, 0, -hitRetentionDays)
	_ = db.Where("at < ?", cutoff).Delete(&storage.TrafficHit{}).Error
	var count int64
	if err := db.Model(&storage.TrafficHit{}).Count(&count).Error; err != nil || count <= maxHitRows {
		return
	}
	var row storage.TrafficHit
	if err := db.Order("id desc").Offset(maxHitRows).Limit(1).Find(&row).Error; err != nil || row.ID == 0 {
		return
	}
	_ = db.Where("id <= ?", row.ID).Delete(&storage.TrafficHit{}).Error
}
