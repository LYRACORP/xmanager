package waf

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"gorm.io/gorm"
)

// Gate holds policy and DB for middleware.
type Gate struct {
	mu       sync.RWMutex
	db       *gorm.DB
	serverID uint
	policy   security.Policy
}

// NewGate creates a WAF gate.
func NewGate(db *gorm.DB, serverID uint) *Gate {
	return &Gate{db: db, serverID: serverID, policy: security.LoadPolicy(db, serverID)}
}

// Reload refreshes policy from DB.
func (g *Gate) Reload() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.policy = security.LoadPolicy(g.db, g.serverID)
	g.mu.Unlock()
}

// Policy returns current policy.
func (g *Gate) Policy() security.Policy {
	if g == nil {
		return security.DefaultPolicy()
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.policy
}

// Middleware blocks requests that fail WAF rules.
func (g *Gate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g == nil || next == nil {
			if next != nil {
				next.ServeHTTP(w, r)
			}
			return
		}
		p := g.Policy()
		res := Evaluate(r, p)
		if res.Blocked {
			ip := clientIP(r)
			securityevents.Log(g.db, securityevents.Entry{
				ServerID: g.serverID,
				Kind:     securityevents.KindWAFBlock,
				IP:       ip,
				Detail:   res.RuleID + ": " + res.Detail,
			})
			http.Error(w, "Forbidden", http.StatusForbidden)
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
