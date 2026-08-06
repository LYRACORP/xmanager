package poller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// MetricsUpdatedMsg is emitted after a server metric poll (for TUI).
type MetricsUpdatedMsg struct {
	ServerID uint
	Snapshot storage.ServerMetricSnapshot
}

type Poller struct {
	db       *gorm.DB
	pool     *ssh.Pool
	cfg      config.PollerConfig
	notifier notify.Notifier
	mu       sync.Mutex
	cancel   context.CancelFunc
	onMetric func(MetricsUpdatedMsg)
}

func New(db *gorm.DB, pool *ssh.Pool, cfg config.PollerConfig, notifier notify.Notifier) *Poller {
	if cfg.IntervalSec <= 0 {
		cfg.IntervalSec = 30
	}
	if cfg.MetricRetention <= 0 {
		cfg.MetricRetention = 288
	}
	if cfg.UptimeIntervalSec <= 0 {
		cfg.UptimeIntervalSec = 60
	}
	return &Poller{db: db, pool: pool, cfg: cfg, notifier: notifier}
}

func (p *Poller) SetOnMetric(fn func(MetricsUpdatedMsg)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onMetric = fn
}

func (p *Poller) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.loop(ctx)
}

func (p *Poller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

func (p *Poller) loop(ctx context.Context) {
	metricsTicker := time.NewTicker(time.Duration(p.cfg.IntervalSec) * time.Second)
	uptimeTicker := time.NewTicker(time.Duration(p.cfg.UptimeIntervalSec) * time.Second)
	defer metricsTicker.Stop()
	defer uptimeTicker.Stop()

	p.pollAllMetrics()
	p.pollUptime()

	for {
		select {
		case <-ctx.Done():
			return
		case <-metricsTicker.C:
			p.pollAllMetrics()
		case <-uptimeTicker.C:
			p.pollUptime()
		}
	}
}

func (p *Poller) pollAllMetrics() {
	var servers []storage.Server
	if err := p.db.Find(&servers).Error; err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, srv := range servers {
		wg.Add(1)
		go func(s storage.Server) {
			defer wg.Done()
			p.pollServer(s)
		}(srv)
	}
	wg.Wait()
}

func (p *Poller) pollServer(srv storage.Server) {
	snap := storage.ServerMetricSnapshot{
		ServerID:     srv.ID,
		SampledAt:    time.Now(),
		UptimeStatus: "unknown",
		Online:       false,
	}

	exec, err := p.ensureExecutor(srv)
	if err != nil {
		_ = p.db.Create(&snap).Error
		p.trimSnapshots(srv.ID)
		p.emit(MetricsUpdatedMsg{ServerID: srv.ID, Snapshot: snap})
		return
	}

	snap.Online = true
	snap.UptimeStatus = "up"
	now := time.Now()
	_ = p.db.Model(&srv).Update("last_seen", now).Error

	if out := exec.RunQuiet(`top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - $1}'`); out != "" {
		snap.CPUPct, _ = strconv.ParseFloat(strings.TrimSpace(out), 64)
	}

	if out := exec.RunQuiet(`free -m | awk '/Mem:/ {printf "%s %s %.1f", $3, $2, ($3/$2)*100}'`); out != "" {
		parts := strings.Fields(out)
		if len(parts) >= 3 {
			snap.RAMUsedMB, _ = strconv.ParseFloat(parts[0], 64)
			snap.RAMTotalMB, _ = strconv.ParseFloat(parts[1], 64)
			snap.RAMPct, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	if out := exec.RunQuiet(`df -BG / | awk 'NR==2 {gsub("G",""); printf "%s %s %.1f", $3, $2, ($3/$2)*100}'`); out != "" {
		parts := strings.Fields(out)
		if len(parts) >= 3 {
			snap.DiskUsedGB, _ = strconv.ParseFloat(parts[0], 64)
			snap.DiskTotalGB, _ = strconv.ParseFloat(parts[1], 64)
			snap.DiskPct, _ = strconv.ParseFloat(parts[2], 64)
		}
	}

	if out := exec.RunQuiet(`cat /proc/net/dev | awk 'NR>2 {rx+=$2; tx+=$10} END {print rx, tx}'`); out != "" {
		parts := strings.Fields(out)
		if len(parts) >= 2 {
			rx, _ := strconv.ParseFloat(parts[0], 64)
			tx, _ := strconv.ParseFloat(parts[1], 64)
			snap.NetRxKBps = rx / 1024
			snap.NetTxKBps = tx / 1024
		}
	}

	if out := exec.RunQuiet(`docker ps -q 2>/dev/null | wc -l`); out != "" {
		snap.ContainerCount, _ = strconv.Atoi(strings.TrimSpace(out))
	}

	_ = p.db.Create(&snap).Error
	p.trimSnapshots(srv.ID)
	p.evaluateAlerts(srv, snap)
	p.emit(MetricsUpdatedMsg{ServerID: srv.ID, Snapshot: snap})
}

func (p *Poller) ensureExecutor(srv storage.Server) (*ssh.Executor, error) {
	if exec, ok := p.pool.GetExecutor(srv.ID); ok {
		return exec, nil
	}
	_, err := p.pool.Connect(srv.ID, ssh.ClientConfig{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.User,
		KeyPath:  srv.SSHKeyPath,
		Password: srv.Password,
		JumpHost: srv.JumpHost,
	})
	if err != nil {
		return nil, err
	}
	exec, _ := p.pool.GetExecutor(srv.ID)
	return exec, nil
}

func (p *Poller) trimSnapshots(serverID uint) {
	var count int64
	p.db.Model(&storage.ServerMetricSnapshot{}).Where("server_id = ?", serverID).Count(&count)
	if int(count) <= p.cfg.MetricRetention {
		return
	}
	excess := int(count) - p.cfg.MetricRetention
	var old []storage.ServerMetricSnapshot
	p.db.Where("server_id = ?", serverID).Order("sampled_at ASC").Limit(excess).Find(&old)
	for _, s := range old {
		p.db.Delete(&s)
	}
}

func (p *Poller) evaluateAlerts(srv storage.Server, snap storage.ServerMetricSnapshot) {
	if p.notifier == nil {
		return
	}
	var rules []storage.AlertRule
	p.db.Where("server_id = ? AND enabled = ?", srv.ID, true).Find(&rules)
	for _, rule := range rules {
		threshold, _ := strconv.ParseFloat(rule.Threshold, 64)
		var breached bool
		var value float64
		switch rule.Type {
		case "cpu":
			value = snap.CPUPct
			breached = value >= threshold
		case "ram":
			value = snap.RAMPct
			breached = value >= threshold
		case "disk":
			value = snap.DiskPct
			breached = value >= threshold
		case "unreachable":
			breached = !snap.Online
		}
		if breached {
			_ = p.notifier.Send(notify.Alert{
				ServerName: srv.Name,
				Title:      fmt.Sprintf("%s threshold exceeded", strings.ToUpper(rule.Type)),
				Message:    fmt.Sprintf("%s is %.1f (threshold %s)", rule.Type, value, rule.Threshold),
				Severity:   notify.SeverityWarning,
			})
		}
	}
}

func (p *Poller) pollUptime() {
	var monitors []storage.UptimeMonitor
	p.db.Where("enabled = ?", true).Find(&monitors)
	for _, m := range monitors {
		status, latency, msg := checkMonitor(m)
		ev := storage.UptimeEvent{
			MonitorID: m.ID,
			Status:    status,
			CheckedAt: time.Now(),
			LatencyMS: latency,
			Message:   msg,
		}
		_ = p.db.Create(&ev).Error
		prev := m.LastStatus
		_ = p.db.Model(&m).Update("last_status", status).Error
		if p.notifier != nil && prev != "" && prev != status && status == "down" {
			_ = p.notifier.Send(notify.Alert{
				ServerName: m.Name,
				Title:      "Uptime check failed",
				Message:    fmt.Sprintf("%s is DOWN: %s", m.Name, msg),
				Severity:   notify.SeverityCritical,
			})
		}
	}
}

func checkMonitor(m storage.UptimeMonitor) (status string, latencyMS int64, message string) {
	start := time.Now()
	if m.URL != "" {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Head(m.URL)
		latencyMS = time.Since(start).Milliseconds()
		if err != nil {
			return "down", latencyMS, err.Error()
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return "down", latencyMS, fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode >= 400 {
			return "degraded", latencyMS, fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "up", latencyMS, "ok"
	}
	host := m.Host
	port := m.Port
	if port == 0 {
		port = 80
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	latencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return "down", latencyMS, err.Error()
	}
	_ = conn.Close()
	return "up", latencyMS, "ok"
}

func (p *Poller) emit(msg MetricsUpdatedMsg) {
	p.mu.Lock()
	fn := p.onMetric
	p.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// LatestSnapshots returns the newest metric snapshot for each server.
func LatestSnapshots(db *gorm.DB) (map[uint]storage.ServerMetricSnapshot, error) {
	var servers []storage.Server
	if err := db.Find(&servers).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]storage.ServerMetricSnapshot, len(servers))
	for _, s := range servers {
		var snap storage.ServerMetricSnapshot
		err := db.Where("server_id = ?", s.ID).Order("sampled_at DESC").First(&snap).Error
		if err == nil {
			out[s.ID] = snap
		}
	}
	return out, nil
}
