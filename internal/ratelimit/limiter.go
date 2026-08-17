package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"gorm.io/gorm"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a per-IP token bucket rate limiter.
type Limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	db       *gorm.DB
	serverID uint
	policy   security.Policy
}

// NewLimiter creates a rate limiter gate.
func NewLimiter(db *gorm.DB, serverID uint) *Limiter {
	return &Limiter{
		buckets:  make(map[string]*bucket),
		db:       db,
		serverID: serverID,
		policy:   security.LoadPolicy(db, serverID),
	}
}

// Reload refreshes policy.
func (l *Limiter) Reload() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.policy = security.LoadPolicy(l.db, l.serverID)
	l.mu.Unlock()
}

func (l *Limiter) currentPolicy() security.Policy {
	if l == nil {
		return security.DefaultPolicy()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.policy
}

// Allow returns false when rate exceeded.
func (l *Limiter) Allow(ip string, p security.Policy) bool {
	if !p.Enabled || p.RateRPM <= 0 {
		return true
	}
	rate := float64(p.RateRPM) / 60.0
	burst := float64(p.RateBurst)
	if burst <= 0 {
		burst = rate * 2
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	now := time.Now()
	if !ok {
		l.buckets[ip] = &bucket{tokens: burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware returns 429 when rate limit exceeded.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil || next == nil {
			if next != nil {
				next.ServeHTTP(w, r)
			}
			return
		}
		p := l.currentPolicy()
		ip := clientIP(r)
		if !l.Allow(ip, p) {
			securityevents.Log(l.db, securityevents.Entry{
				ServerID: l.serverID,
				Kind:     securityevents.KindRateLimit,
				IP:       ip,
				Detail:   r.Method + " " + r.URL.Path,
			})
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
