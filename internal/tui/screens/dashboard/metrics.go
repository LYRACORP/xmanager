package dashboard

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

var (
	cpuIdleRE    = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*id`)
	memLineRE    = regexp.MustCompile(`(?i)^Mem:\s+(\d+)\s+(\d+)`)
	swapLineRE   = regexp.MustCompile(`(?i)^Swap:\s+(\d+)\s+(\d+)`)
	dfUseRE      = regexp.MustCompile(`(\d+)%`)
	loadAvgRE    = regexp.MustCompile(`^([\d.]+)\s+([\d.]+)\s+([\d.]+)`)
	netDevLineRE = regexp.MustCompile(`^\s*(\S+):\s*(\d+)\s+\d+\s+\d+\s+\d+\s+\d+\s+\d+\s+\d+\s+\d+\s+(\d+)`)
)

type metricsData struct {
	cpuUsage    float64
	ramUsage    float64
	swapUsage   float64
	diskUsage   float64
	ramUsedMB   int
	ramTotalMB  int
	swapUsedMB  int
	swapTotalMB int
	cores       int
	load1       string
	load5       string
	load15      string
	netIface    string
	netRx       int64
	netTx       int64
	mountCount  int
	worstMount  string
	worstPct    int
	hostname    string
	uptime      string
	kernel      string
}

type metricsLoadedMsg struct {
	data metricsData
	err  error
}

func (m *Model) loadMetrics() tea.Cmd {
	m.metricsState = stateLoading
	serverID := m.ctx.ServerID
	pool := m.ctx.Pool
	return func() tea.Msg {
		ex, ok := pool.GetExecutor(serverID)
		if !ok {
			return metricsLoadedMsg{err: errNotConnected}
		}

		cmds := map[string]string{
			"top":      "top -bn1 | head -5",
			"free":     "free -m",
			"df":       "df -h",
			"load":     "cat /proc/loadavg",
			"net":      "cat /proc/net/dev",
			"nproc":    "nproc 2>/dev/null || echo 1",
			"uptime":   "uptime -p 2>/dev/null || uptime",
			"hostname": "hostname -s 2>/dev/null || hostname",
			"kernel":   "uname -r",
		}

		out := make(map[string]string, len(cmds))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for k, cmd := range cmds {
			wg.Add(1)
			go func(key, command string) {
				defer wg.Done()
				s := ex.RunQuiet(command)
				mu.Lock()
				out[key] = s
				mu.Unlock()
			}(k, cmd)
		}
		wg.Wait()

		data := metricsData{
			cpuUsage: parseCPUUsage(out["top"]),
			ramUsage: parseMemUsage(out["free"]),
			cores:    parseInt(strings.TrimSpace(out["nproc"]), 1),
			hostname: strings.TrimSpace(out["hostname"]),
			uptime:   parseUptime(out["uptime"]),
			kernel:   strings.TrimSpace(out["kernel"]),
		}
		parseMemDetail(out["free"], &data)
		parseSwapDetail(out["free"], &data)
		parseDiskDetail(out["df"], &data)
		parseLoadAvg(out["load"], &data)
		parseNetDev(out["net"], &data)

		return metricsLoadedMsg{data: data}
	}
}

func parseMemDetail(freeOut string, d *metricsData) {
	for _, line := range strings.Split(freeOut, "\n") {
		m := memLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) >= 3 {
			total, _ := strconv.Atoi(m[1])
			used, _ := strconv.Atoi(m[2])
			d.ramTotalMB = total
			d.ramUsedMB = used
			return
		}
	}
}

func parseSwapDetail(freeOut string, d *metricsData) {
	for _, line := range strings.Split(freeOut, "\n") {
		m := swapLineRE.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) >= 3 {
			total, _ := strconv.Atoi(m[1])
			used, _ := strconv.Atoi(m[2])
			d.swapTotalMB = total
			d.swapUsedMB = used
			if total > 0 {
				d.swapUsage = min(1, float64(used)/float64(total))
			}
			return
		}
	}
}

func parseDiskDetail(dfOut string, d *metricsData) {
	lines := strings.Split(strings.TrimSpace(dfOut), "\n")
	if len(lines) < 2 {
		return
	}
	d.mountCount = len(lines) - 1
	worstPct := 0
	worstMount := "/"
	rootPct := 0.0

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		mount := fields[len(fields)-1]
		pctStr := strings.TrimSuffix(fields[len(fields)-2], "%")
		pct, err := strconv.Atoi(pctStr)
		if err != nil {
			m := dfUseRE.FindStringSubmatch(line)
			if len(m) >= 2 {
				pct, _ = strconv.Atoi(m[1])
			}
		}
		if mount == "/" {
			rootPct = float64(pct) / 100
		}
		if pct > worstPct {
			worstPct = pct
			worstMount = mount
		}
	}
	d.diskUsage = rootPct
	if d.diskUsage == 0 && worstPct > 0 {
		d.diskUsage = float64(worstPct) / 100
	}
	d.worstMount = worstMount
	d.worstPct = worstPct
}

func parseLoadAvg(raw string, d *metricsData) {
	m := loadAvgRE.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) >= 4 {
		d.load1 = m[1]
		d.load5 = m[2]
		d.load15 = m[3]
	}
}

func parseNetDev(raw string, d *metricsData) {
	var bestRx, bestTx int64
	var bestIface string
	for _, line := range strings.Split(raw, "\n") {
		m := netDevLineRE.FindStringSubmatch(line)
		if len(m) < 4 {
			continue
		}
		iface := m[1]
		if strings.HasPrefix(iface, "lo") {
			continue
		}
		rx, _ := strconv.ParseInt(m[2], 10, 64)
		tx, _ := strconv.ParseInt(m[3], 10, 64)
		if rx+tx > bestRx+bestTx {
			bestRx, bestTx = rx, tx
			bestIface = iface
		}
	}
	d.netIface = bestIface
	d.netRx = bestRx
	d.netTx = bestTx
}

func parseUptime(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "up ")
	if len(raw) > 40 {
		return raw[:37] + "..."
	}
	return raw
}

func parseInt(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func parseCPUUsage(topOut string) float64 {
	m := cpuIdleRE.FindStringSubmatch(topOut)
	if len(m) < 2 {
		return 0
	}
	idle, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	usage := (100 - idle) / 100
	if usage < 0 {
		return 0
	}
	if usage > 1 {
		return 1
	}
	return usage
}

func parseMemUsage(freeOut string) float64 {
	for _, line := range strings.Split(freeOut, "\n") {
		line = strings.TrimSpace(line)
		m := memLineRE.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		total, err1 := strconv.ParseFloat(m[1], 64)
		used, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil || total <= 0 {
			return 0
		}
		return min(1, used/total)
	}
	return 0
}

func (m *Model) renderOverview() string {
	if m.metricsState == stateLoading && m.metricsErr == "" && m.metrics.cores == 0 {
		return theme.MutedText().Render("  Loading metrics…")
	}
	if m.metricsState == stateError {
		return theme.ErrorText().Render("  " + m.metricsErr)
	}

	bp := layout.Breakpoint(m.width)
	gaugeCount := 5
	if bp == layout.BreakpointNarrow {
		gaugeCount = 2
	}
	gw := layout.GaugeWidth(m.width, gaugeCount, 2, 14)

	gCPU := components.NewGauge("CPU", m.metrics.cpuUsage)
	gCPU.Width = gw
	gRAM := components.NewGauge("RAM", m.metrics.ramUsage)
	gRAM.Width = gw
	gSWP := components.NewGauge("SWP", m.metrics.swapUsage)
	gSWP.Width = gw
	gDSK := components.NewGauge("DSK", m.metrics.diskUsage)
	gDSK.Width = gw

	netLabel := "NET"
	netVal := 0.0
	if m.metrics.netRx+m.metrics.netTx > 0 {
		netVal = 0.3
	}
	gNET := components.NewGauge(netLabel, netVal)
	gNET.Width = gw
	gNET.ShowPct = false

	var gauges string
	if bp == layout.BreakpointNarrow {
		gauges = lipgloss.JoinVertical(lipgloss.Left,
			gCPU.View(), gRAM.View(), gSWP.View(), gDSK.View(), gNET.View(),
		)
	} else {
		gauges = lipgloss.JoinHorizontal(lipgloss.Top,
			gCPU.View(), " ", gRAM.View(), " ", gSWP.View(), " ", gDSK.View(), " ", gNET.View(),
		)
	}

	detail := theme.MutedText().Render(fmtMemDetail(m.metrics))
	if m.metrics.worstPct > 0 {
		detail += "  " + theme.MutedText().Render(
			"worst "+m.metrics.worstMount+" "+strconv.Itoa(m.metrics.worstPct)+"%",
		)
	}
	if m.metrics.netIface != "" {
		detail += "  " + theme.MutedText().Render(
			m.metrics.netIface+" ↓"+components.HumanBytes(m.metrics.netRx)+" ↑"+components.HumanBytes(m.metrics.netTx),
		)
	}

	chips := components.StatRow(
		components.StatChip("load", loadLabel(m.metrics)),
		components.StatChip("cores", strconv.Itoa(m.metrics.cores)),
		components.StatChip("mounts", strconv.Itoa(m.metrics.mountCount)),
	)
	if m.metrics.hostname != "" {
		chips = components.StatChip("host", m.metrics.hostname) + " " + chips
	}
	if m.metrics.uptime != "" {
		chips += " " + components.StatChip("up", m.metrics.uptime)
	}
	if m.metrics.kernel != "" {
		chips += " " + components.StatChip("krn", m.metrics.kernel)
	}

	leftW, rightW, stack := splitPanels(m.width, 1, 50, 28, 24)
	alertsContent := m.renderAlerts()
	sysContent := theme.SubtitleStyle().Render("System") + "\n" + chips

	var main string
	alertsPanel := theme.PanelStyle().Width(leftW).Render(alertsContent)
	sysPanel := theme.PanelStyle().Width(rightW).Render(sysContent)
	if stack {
		main = lipgloss.JoinVertical(lipgloss.Left, alertsPanel, sysPanel)
	} else {
		main = lipgloss.JoinHorizontal(lipgloss.Top, alertsPanel, " ", sysPanel)
	}

	return lipgloss.JoinVertical(lipgloss.Left, gauges, detail, "", main)
}

func fmtMemDetail(d metricsData) string {
	if d.ramTotalMB == 0 {
		return ""
	}
	return "RAM " + strconv.Itoa(d.ramUsedMB) + "/" + strconv.Itoa(d.ramTotalMB) + " MB"
}

func loadLabel(d metricsData) string {
	if d.load1 == "" {
		return "-"
	}
	return d.load1 + "/" + d.load5 + "/" + d.load15
}

func (m *Model) renderAlerts() string {
	if m.alertsState == stateLoading && len(m.alerts) == 0 {
		return theme.MutedText().Render("  Loading alerts…")
	}
	if len(m.alerts) == 0 {
		return theme.SubtitleStyle().Render("Recent alerts") + "\n" + theme.EmptyStateText()
	}
	var b strings.Builder
	b.WriteString(theme.SubtitleStyle().Render("Recent alerts") + "\n")
	for _, a := range m.alerts {
		sev := strings.ToLower(a.Severity)
		line := a.Service + " · " + truncateStr(a.Message, 48)
		switch sev {
		case "critical":
			b.WriteString(theme.ErrorText().Render("▸ "+line) + "\n")
		case "warning":
			b.WriteString(theme.WarningText().Render("▸ "+line) + "\n")
		default:
			b.WriteString(theme.MutedText().Render("▸ "+line) + "\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func truncateStr(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func splitPanels(width, gap, leftPct, minLeft, minRight int) (int, int, bool) {
	return layout.SplitHorizontal(width, gap, leftPct, minLeft, minRight)
}
