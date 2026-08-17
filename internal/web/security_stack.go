package web

import (
	"net/http"

	"github.com/lyracorp/xmanager/internal/ratelimit"
	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/traffic"
	"github.com/lyracorp/xmanager/internal/waf"
	"gorm.io/gorm"
)

// securityStack wires policy, WAF, rate limit, and traffic recording.
type securityStack struct {
	db          *gorm.DB
	serverID    uint
	wafGate     *waf.Gate
	limiter     *ratelimit.Limiter
	recorder    *traffic.Recorder
	dumpMgr     *reqdump.Manager
	trafficStop chan struct{}
}

func newSecurityStack(db *gorm.DB, serverID uint, dump *reqdump.Manager) *securityStack {
	return &securityStack{
		db:          db,
		serverID:    serverID,
		wafGate:     waf.NewGate(db, serverID),
		limiter:     ratelimit.NewLimiter(db, serverID),
		recorder:    traffic.NewRecorder(db, serverID),
		dumpMgr:     dump,
		trafficStop: make(chan struct{}),
	}
}

func (s *securityStack) reload(exec *ssh.Executor) {
	if s == nil {
		return
	}
	s.wafGate.Reload()
	s.limiter.Reload()
	s.recorder.Reload()
	p := security.LoadPolicy(s.db, s.serverID)
	if exec != nil {
		_ = ratelimit.ApplyNginx(exec, p)
		_ = waf.ApplyModsec(exec, p)
	}
}

func (s *securityStack) stop() {
	if s == nil || s.trafficStop == nil {
		return
	}
	select {
	case <-s.trafficStop:
	default:
		close(s.trafficStop)
	}
}

func (s *securityStack) startNginxPoller(exec *ssh.Executor) {
	if s == nil || exec == nil {
		return
	}
	go traffic.StartNginxPoller(exec, s.serverID, s.recorder, s.trafficStop)
}

func (s *securityStack) wrap(mux http.Handler) http.Handler {
	h := http.Handler(mux)
	if s.dumpMgr != nil {
		h = s.dumpMgr.Middleware(h)
	}
	h = s.recorder.Middleware(h)
	h = s.wafGate.Middleware(h)
	h = s.limiter.Middleware(h)
	h = s.ipGate(h)
	return h
}

func (s *securityStack) ipGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := security.LoadPolicy(s.db, s.serverID)
		if !p.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIPFromRequest(r)
		if !security.IsAllowed(ip, p) {
			securityevents.Log(s.db, securityevents.Entry{
				ServerID: s.serverID,
				Kind:     securityevents.KindWAFBlock,
				IP:       ip,
				Detail:   "blocked_ip",
			})
			s.recorder.Record(traffic.Hit{
				ServerID: s.serverID,
				Source:   traffic.SourcePanel,
				IP:       ip,
				Method:   r.Method,
				Path:     r.URL.Path,
				Status:   403,
				Blocked:  true,
				Rule:     "block_ip",
			})
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
