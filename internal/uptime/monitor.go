package uptime

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Monitor struct {
	db *gorm.DB
}

func NewMonitor(db *gorm.DB) *Monitor {
	return &Monitor{db: db}
}

// Create adds a new UptimeMonitor record.
func (m *Monitor) Create(mon *storage.UptimeMonitor) error {
	return m.db.Create(mon).Error
}

// List returns all monitors, optionally filtered by serverID (0 = all).
func (m *Monitor) List(serverID uint) ([]storage.UptimeMonitor, error) {
	var monitors []storage.UptimeMonitor
	q := m.db.Order("name ASC")
	if serverID > 0 {
		q = q.Where("server_id = ?", serverID)
	}
	err := q.Find(&monitors).Error
	return monitors, err
}

// Get returns a single monitor by ID.
func (m *Monitor) Get(id uint) (*storage.UptimeMonitor, error) {
	var mon storage.UptimeMonitor
	if err := m.db.First(&mon, id).Error; err != nil {
		return nil, fmt.Errorf("monitor not found: %w", err)
	}
	return &mon, nil
}

// Update saves changes to an existing monitor.
func (m *Monitor) Update(mon *storage.UptimeMonitor) error {
	return m.db.Save(mon).Error
}

// Delete removes a monitor and its history.
func (m *Monitor) Delete(id uint) error {
	return m.db.Delete(&storage.UptimeMonitor{}, id).Error
}

// CheckResult is the outcome of a single health probe.
type CheckResult struct {
	Status    string // up, down, degraded
	LatencyMS int64
	Message   string
}

// Check performs a single probe for the given monitor and records the event.
func (m *Monitor) Check(mon *storage.UptimeMonitor) (*CheckResult, error) {
	timeout := time.Duration(mon.IntervalSec/2) * time.Second
	if timeout < 2*time.Second {
		timeout = 5 * time.Second
	}

	var result CheckResult
	var probeErr error

	if mon.URL != "" {
		result, probeErr = checkHTTP(mon.URL, timeout)
	} else if mon.Host != "" && mon.Port > 0 {
		result, probeErr = checkTCP(mon.Host, mon.Port, timeout)
	} else {
		return nil, fmt.Errorf("monitor has neither url nor host:port")
	}

	if probeErr != nil {
		result.Status = "down"
		result.Message = probeErr.Error()
	}

	event := &storage.UptimeEvent{
		MonitorID: mon.ID,
		Status:    result.Status,
		CheckedAt: time.Now(),
		LatencyMS: result.LatencyMS,
		Message:   result.Message,
	}
	_ = m.db.Create(event).Error

	// update last status on monitor
	_ = m.db.Model(mon).Update("last_status", result.Status).Error
	mon.LastStatus = result.Status

	return &result, nil
}

// CheckAll runs Check on every enabled monitor and returns all results.
func (m *Monitor) CheckAll() (map[uint]*CheckResult, error) {
	monitors, err := m.List(0)
	if err != nil {
		return nil, err
	}

	results := make(map[uint]*CheckResult, len(monitors))
	for i := range monitors {
		if !monitors[i].Enabled {
			continue
		}
		r, _ := m.Check(&monitors[i])
		results[monitors[i].ID] = r
	}
	return results, nil
}

// RecentEvents returns the last n events for a monitor.
func (m *Monitor) RecentEvents(monitorID uint, n int) ([]storage.UptimeEvent, error) {
	var events []storage.UptimeEvent
	if n <= 0 {
		n = 50
	}
	err := m.db.Where("monitor_id = ?", monitorID).
		Order("checked_at DESC").Limit(n).Find(&events).Error
	return events, err
}

// UptimePct computes the percentage of "up" checks within the given window.
func (m *Monitor) UptimePct(monitorID uint, window time.Duration) (float64, error) {
	var total, up int64
	since := time.Now().Add(-window)
	m.db.Model(&storage.UptimeEvent{}).
		Where("monitor_id = ? AND checked_at >= ?", monitorID, since).
		Count(&total)
	m.db.Model(&storage.UptimeEvent{}).
		Where("monitor_id = ? AND checked_at >= ? AND status = 'up'", monitorID, since).
		Count(&up)
	if total == 0 {
		return 0, nil
	}
	return float64(up) / float64(total) * 100, nil
}

func checkHTTP(url string, timeout time.Duration) (CheckResult, error) {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	start := time.Now()
	resp, err := client.Head(url)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return CheckResult{Status: "down", LatencyMS: latency, Message: err.Error()}, err
	}
	defer resp.Body.Close()

	status := "up"
	msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode >= 500 {
		status = "down"
	} else if resp.StatusCode >= 400 {
		status = "degraded"
	}
	return CheckResult{Status: status, LatencyMS: latency, Message: msg}, nil
}

func checkTCP(host string, port int, timeout time.Duration) (CheckResult, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return CheckResult{Status: "down", LatencyMS: latency, Message: err.Error()}, err
	}
	conn.Close()
	return CheckResult{Status: "up", LatencyMS: latency, Message: "tcp ok"}, nil
}
