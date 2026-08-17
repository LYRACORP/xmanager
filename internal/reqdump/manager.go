package reqdump

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"gorm.io/gorm"
)

const internalSinkPath = "/internal/reqdump"

// Manager runs panel middleware hooks, nginx sink, and honeypot listeners.
type Manager struct {
	mu        sync.Mutex
	db        *gorm.DB
	serverID  uint
	panelPort int
	cfg       Config
	hpLn      []net.Listener
	hpPorts   []int
	sinkSrv   *http.Server
}

// NewManager creates a dump manager.
func NewManager(db *gorm.DB, serverID uint, panelPort int) *Manager {
	return &Manager{db: db, serverID: serverID, panelPort: panelPort}
}

// Reload applies config from DB.
func (m *Manager) Reload() {
	if m == nil {
		return
	}
	cfg := LoadConfig(m.db, m.serverID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.SinkPort == 0 {
		cfg.SinkPort = DefaultConfig().SinkPort
	}
	m.cfg = cfg
	m.restartLocked()
}

// Config returns current settings.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Stop shuts down listeners.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

func (m *Manager) restartLocked() {
	m.stopLocked()
	if !m.cfg.Enabled {
		return
	}
	if m.cfg.DumpHoneypot {
		m.startHoneypotsLocked()
	}
	if m.cfg.DumpNginx {
		m.startSinkLocked()
	}
}

func (m *Manager) stopLocked() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, ln := range m.hpLn {
		_ = ln.Close()
	}
	m.hpLn = nil
	if len(m.hpPorts) > 0 {
		for _, p := range m.hpPorts {
			_ = hostfirewall.Deny(p, "tcp", protectedPorts(m.panelPort))
		}
	}
	m.hpPorts = nil
	if m.sinkSrv != nil {
		_ = m.sinkSrv.Shutdown(ctx)
		m.sinkSrv = nil
	}
}

func protectedPorts(panelPort int) map[int]string {
	extra := map[int]string{}
	if panelPort > 0 {
		extra[panelPort] = "panel"
	}
	return extra
}

func (m *Manager) startSinkLocked() {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(m.cfg.SinkPort))
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+internalSinkPath, m.HandleNginxSink)
	m.sinkSrv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		_ = m.sinkSrv.ListenAndServe()
	}()
}

func (m *Manager) startHoneypotsLocked() {
	blocked := map[int]bool{22: true, 80: true, 443: true}
	if m.panelPort > 0 {
		blocked[m.panelPort] = true
	}
	for _, p := range ParseHoneypotPorts(m.cfg.HoneypotPorts) {
		if blocked[p] || portInUse(p) {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(p)))
		if err != nil {
			continue
		}
		m.hpLn = append(m.hpLn, ln)
		m.hpPorts = append(m.hpPorts, p)
		_ = hostfirewall.Allow([]int{p}, "tcp")
		port := p
		go m.serveHoneypot(ln, port)
	}
}

func (m *Manager) serveHoneypot(ln net.Listener, port int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go m.handleHoneypotConn(conn, port)
	}
}

func (m *Manager) handleHoneypotConn(conn net.Conn, port int) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, maxBodyBytes+1)
	n, _ := conn.Read(buf)
	truncated := n > maxBodyBytes
	if truncated {
		n = maxBodyBytes
	}
	body := buf[:n]
	remote := conn.RemoteAddr().String()
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	method, path, query := "RAW", "", ""
	headers := ""
	if idx := bytes.Index(body, []byte("\r\n")); idx > 0 {
		line := string(body[:idx])
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.Contains(strings.ToUpper(parts[0]), "HTTP") || (len(parts) >= 3 && strings.HasPrefix(parts[2], "HTTP/")) {
			method = parts[0]
			path = parts[1]
			if qidx := strings.Index(path, "?"); qidx >= 0 {
				query = path[qidx+1:]
				path = path[:qidx]
			}
			headers = string(body[idx+2:])
			if end := strings.Index(headers, "\r\n\r\n"); end >= 0 {
				body = body[idx+2+end+4:]
			}
		}
	}
	_, _ = Record(m.db, Capture{
		ServerID:   m.serverID,
		Source:     SourceHoneypot,
		ListenPort: port,
		RemoteIP:   remote,
		Method:     method,
		Path:       path,
		Query:      query,
		Proto:      "tcp",
		Headers:    headers,
		Body:       body,
		Truncated:  truncated,
		Status:     404,
	})
	if security.ScanProbe(path, query) {
		securityevents.Log(m.db, securityevents.Entry{
			ServerID: m.serverID,
			Kind:     securityevents.KindScan,
			IP:       remote,
			Detail:   path,
		})
	}
	if method != "RAW" {
		_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	}
}

func portInUse(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func (m *Manager) HandleNginxSink(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	truncated := len(body) > maxBodyBytes
	if truncated {
		body = body[:maxBodyBytes]
	}
	path := r.Header.Get("X-Original-URI")
	if path == "" {
		path = r.URL.Path
	}
	method := r.Header.Get("X-Original-Method")
	if method == "" {
		method = r.Method
	}
	remote := r.Header.Get("X-Real-IP")
	if remote == "" {
		remote = clientIP(r)
	}
	_, _ = Record(m.db, Capture{
		ServerID:  m.serverID,
		Source:    SourceNginx,
		RemoteIP:  remote,
		Method:    method,
		Path:      path,
		Query:     r.URL.RawQuery,
		Proto:     r.Proto,
		Headers:   formatHeaders(r.Header),
		Body:      body,
		Truncated: truncated,
		Status:    0,
	})
	if security.ScanProbe(path, r.URL.RawQuery) {
		securityevents.Log(m.db, securityevents.Entry{
			ServerID: m.serverID,
			Kind:     securityevents.KindScan,
			IP:       remote,
			Detail:   path,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// Middleware wraps the panel handler to capture requests when enabled.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m == nil || next == nil {
			if next != nil {
				next.ServeHTTP(w, r)
			}
			return
		}
		cfg := m.Config()
		if !cfg.Enabled || !cfg.DumpPanel {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == internalSinkPath {
			next.ServeHTTP(w, r)
			return
		}
		var bodyBuf bytes.Buffer
		if r.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
			truncated := len(b) > maxBodyBytes
			if truncated {
				b = b[:maxBodyBytes]
			}
			bodyBuf.Write(b)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		body := RedactBody(bodyBuf.Bytes(), r.Header.Get("Content-Type"))
		path := r.URL.Path
		query := r.URL.RawQuery
		_, _ = Record(m.db, Capture{
			ServerID:  m.serverID,
			Source:    SourcePanel,
			RemoteIP:  clientIP(r),
			Method:    r.Method,
			Path:      path,
			Query:     query,
			Proto:     r.Proto,
			Headers:   RedactHeaders(formatHeaders(r.Header)),
			Body:      body,
			Truncated: bodyBuf.Len() > maxBodyBytes,
			Status:    rec.status,
		})
		if security.ScanProbe(path, query) {
			securityevents.Log(m.db, securityevents.Entry{
				ServerID: m.serverID,
				Kind:     securityevents.KindScan,
				IP:       clientIP(r),
				Detail:   path,
			})
		}
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
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func formatHeaders(h http.Header) string {
	var b strings.Builder
	for k, vals := range h {
		for _, v := range vals {
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	return b.String()
}

// SinkURL returns the loopback nginx mirror target.
func (m *Manager) SinkURL() string {
	cfg := m.Config()
	port := cfg.SinkPort
	if port == 0 {
		port = DefaultConfig().SinkPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, internalSinkPath)
}
