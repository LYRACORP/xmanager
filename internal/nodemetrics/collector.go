package nodemetrics

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot is a point-in-time view of the local host.
type Snapshot struct {
	Hostname       string      `json:"hostname"`
	SampledAt      time.Time   `json:"sampled_at"`
	CPUPct         float64     `json:"cpu_pct"`
	RAMPct         float64     `json:"ram_pct"`
	RAMUsedMB      float64     `json:"ram_used_mb"`
	RAMTotalMB     float64     `json:"ram_total_mb"`
	DiskPct        float64     `json:"disk_pct"`
	DiskUsedGB     float64     `json:"disk_used_gb"`
	DiskTotalGB    float64     `json:"disk_total_gb"`
	Load1          float64     `json:"load1"`
	Load5          float64     `json:"load5"`
	Load15         float64     `json:"load15"`
	Cores          int         `json:"cores"`
	Kernel         string      `json:"kernel"`
	UptimeSec      int64       `json:"uptime_sec"`
	NetIface       string      `json:"net_iface"`
	NetRxBytes     uint64      `json:"net_rx_bytes"`
	NetTxBytes     uint64      `json:"net_tx_bytes"`
	ContainerCount int         `json:"container_count"`
	Containers     []Container `json:"containers"`
	Ports          []Port      `json:"ports"`
	Error          string      `json:"error,omitempty"`
}

type Container struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

type Port struct {
	Proto    string `json:"proto"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Process  string `json:"process"`
	CanClose bool   `json:"can_close"`
}

// Collector samples local metrics on an interval.
type Collector struct {
	mu       sync.RWMutex
	latest   Snapshot
	interval time.Duration
	stop     chan struct{}
}

func NewCollector(interval time.Duration) *Collector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Collector{interval: interval, stop: make(chan struct{})}
}

func (c *Collector) Start() {
	c.refresh()
	go func() {
		t := time.NewTicker(c.interval)
		defer t.Stop()
		for {
			select {
			case <-c.stop:
				return
			case <-t.C:
				c.refresh()
			}
		}
	}()
}

func (c *Collector) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

func (c *Collector) Latest() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latest
}

// Refresh forces an immediate sample (e.g. after firewall changes).
func (c *Collector) Refresh() {
	c.refresh()
}

func (c *Collector) refresh() {
	snap := Sample()
	c.mu.Lock()
	c.latest = snap
	c.mu.Unlock()
}

// Sample collects metrics once from the local machine.
func Sample() Snapshot {
	snap := Snapshot{
		SampledAt: time.Now(),
		Cores:     runtime.NumCPU(),
	}
	if h, err := os.Hostname(); err == nil {
		snap.Hostname = h
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		snap.Kernel = strings.TrimSpace(string(out))
	}

	snap.CPUPct = sampleCPU()
	snap.RAMUsedMB, snap.RAMTotalMB, snap.RAMPct = sampleRAM()
	snap.DiskUsedGB, snap.DiskTotalGB, snap.DiskPct = sampleDisk()
	snap.Load1, snap.Load5, snap.Load15 = sampleLoad()
	snap.UptimeSec = sampleUptime()
	snap.NetIface, snap.NetRxBytes, snap.NetTxBytes = sampleNet()
	snap.Containers = sampleContainers()
	snap.ContainerCount = len(snap.Containers)
	snap.Ports = samplePorts()
	return snap
}

func sampleCPU() float64 {
	// Two reads of /proc/stat 200ms apart.
	aIdle, aTotal, err := readCPUStat()
	if err != nil {
		return 0
	}
	time.Sleep(200 * time.Millisecond)
	bIdle, bTotal, err := readCPUStat()
	if err != nil {
		return 0
	}
	dTotal := bTotal - aTotal
	dIdle := bIdle - aIdle
	if dTotal == 0 {
		return 0
	}
	return (1.0 - float64(dIdle)/float64(dTotal)) * 100
}

func readCPUStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, fmt.Errorf("empty /proc/stat")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("bad /proc/stat")
	}
	var vals []uint64
	for _, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		vals = append(vals, v)
		total += v
	}
	if len(vals) > 3 {
		idle = vals[3]
	}
	return idle, total, nil
}

func sampleRAM() (usedMB, totalMB, pct float64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0
	}
	defer f.Close()
	var memTotal, memAvail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d", &memTotal)
		}
		if strings.HasPrefix(line, "MemAvailable:") {
			fmt.Sscanf(line, "MemAvailable: %d", &memAvail)
		}
	}
	if memTotal == 0 {
		return 0, 0, 0
	}
	totalMB = float64(memTotal) / 1024
	usedMB = float64(memTotal-memAvail) / 1024
	pct = (usedMB / totalMB) * 100
	return usedMB, totalMB, pct
}

func sampleDisk() (usedGB, totalGB, pct float64) {
	out, err := exec.Command("df", "-BG", "/").Output()
	if err != nil {
		return 0, 0, 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0, 0, 0
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return 0, 0, 0
	}
	total, _ := strconv.ParseFloat(strings.TrimSuffix(fields[1], "G"), 64)
	used, _ := strconv.ParseFloat(strings.TrimSuffix(fields[2], "G"), 64)
	p, _ := strconv.ParseFloat(strings.TrimSuffix(fields[4], "%"), 64)
	return used, total, p
}

func sampleLoad() (l1, l5, l15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(fields[0], 64)
	l5, _ = strconv.ParseFloat(fields[1], 64)
	l15, _ = strconv.ParseFloat(fields[2], 64)
	return
}

func sampleUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return int64(v)
}

func sampleNet() (iface string, rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return "", 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var bestIface string
	var bestRx, bestTx uint64
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue
		}
		line := strings.TrimSpace(sc.Text())
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		if r+t > bestRx+bestTx {
			bestIface, bestRx, bestTx = name, r, t
		}
	}
	return bestIface, bestRx, bestTx
}

func sampleContainers() []Container {
	out, err := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}").Output()
	if err != nil {
		return nil
	}
	var list []Container
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		c := Container{}
		if len(f) > 0 {
			c.ID = f[0]
			if len(c.ID) > 12 {
				c.ID = c.ID[:12]
			}
		}
		if len(f) > 1 {
			c.Name = f[1]
		}
		if len(f) > 2 {
			c.Image = f[2]
		}
		if len(f) > 3 {
			c.Status = f[3]
		}
		if len(f) > 4 {
			c.Ports = f[4]
		}
		list = append(list, c)
	}
	return list
}

func samplePorts() []Port {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		out, err = exec.Command("ss", "-tln").Output()
		if err != nil {
			return nil
		}
	}
	var ports []Port
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		p := Port{Proto: "tcp", Address: fields[3]}
		p.Port = parseListenPort(fields[3])
		if len(fields) >= 6 {
			p.Process = fields[len(fields)-1]
		}
		ports = append(ports, p)
		if len(ports) >= 40 {
			break
		}
	}
	return ports
}

// parseListenPort extracts the port from an ss local address (e.g. 0.0.0.0:8080, *:22, [::]:443).
func parseListenPort(addr string) int {
	addr = strings.TrimSpace(addr)
	if i := strings.LastIndex(addr, ":"); i >= 0 && i+1 < len(addr) {
		n, err := strconv.Atoi(addr[i+1:])
		if err == nil {
			return n
		}
	}
	return 0
}

func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func FormatUptime(sec int64) string {
	d := time.Duration(sec) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
