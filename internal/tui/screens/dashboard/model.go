package dashboard

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

var (
	errNotConnected = errors.New("not connected — open server from list")
	cpuIdleRE       = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*id`)
	memLineRE       = regexp.MustCompile(`(?i)^Mem:\s+(\d+)\s+(\d+)`)
	dfUseRE         = regexp.MustCompile(`(\d+)%`)
)

type tickMsg struct{}

type alertsLoadedMsg struct {
	alerts []storage.ErrorEvent
}

type dashboardDataMsg struct {
	cpu, ram, disk float64
	services       []serviceRow
	err            error
}

type serviceRow struct {
	name   string
	active bool
}

type Model struct {
	ctx           *shared.AppContext
	width, height int
	cpu, ram, disk float64
	services      []serviceRow
	serviceTable  table.Model
	alerts        []storage.ErrorEvent
	fetchErr      string
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "Dashboard" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadAlerts(), m.refresh(), m.scheduleTick())
}

func (m *Model) scheduleTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) loadAlerts() tea.Cmd {
	return func() tea.Msg {
		var alerts []storage.ErrorEvent
		if m.ctx.DB != nil && m.ctx.ServerID != 0 {
			m.ctx.DB.Where("server_id = ? AND resolved = ?", m.ctx.ServerID, false).
				Order("last_seen desc").
				Limit(10).
				Find(&alerts)
		}
		return alertsLoadedMsg{alerts: alerts}
	}
}

func (m *Model) refresh() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return dashboardDataMsg{err: errNotConnected}
		}
		topOut := ex.RunQuiet("top -bn1 | head -5")
		freeOut := ex.RunQuiet("free -m")
		dfOut := ex.RunQuiet("df -h /")
		svcOut := ex.RunQuiet("systemctl list-units --type=service --state=running --no-pager --no-legend 2>/dev/null | head -24")
		var services []serviceRow
		if strings.TrimSpace(svcOut) != "" {
			services = parseSystemdServices(svcOut)
		} else {
			services = parseDockerServices(ex.RunQuiet(`docker ps --format '{{.Names}}\t{{.Status}}' 2>/dev/null | head -24`))
		}
		return dashboardDataMsg{
			cpu:      parseCPUUsage(topOut),
			ram:      parseMemUsage(freeOut),
			disk:     parseDiskUsage(dfOut),
			services: services,
		}
	}
}

func (m *Model) rebuildTable() {
	cols := []table.Column{
		{Title: " ", Width: 3},
		{Title: "Service", Width: 28},
		{Title: "State", Width: 12},
	}
	rows := make([]table.Row, len(m.services))
	for i, s := range m.services {
		st := "active"
		if !s.active {
			st = "inactive"
		}
		rows[i] = table.Row{theme.StatusDot(s.active), s.name, st}
	}
	h := m.height - 14
	if h < 5 {
		h = 5
	}
	m.serviceTable = components.StyledTable(cols, rows, h)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(m.refresh(), m.scheduleTick(), m.loadAlerts())
	case alertsLoadedMsg:
		m.alerts = msg.alerts
		return m, nil
	case dashboardDataMsg:
		if msg.err != nil {
			m.fetchErr = msg.err.Error()
			m.cpu, m.ram, m.disk = 0, 0, 0
			m.services = nil
		} else {
			m.fetchErr = ""
			m.cpu, m.ram, m.disk = msg.cpu, msg.ram, msg.disk
			m.services = msg.services
		}
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		if nav, ok := m.handleKeys(msg); ok {
			return m, nav
		}
	}
	var cmd tea.Cmd
	m.serviceTable, cmd = m.serviceTable.Update(msg)
	return m, cmd
}

func (m *Model) handleKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "b":
		return func() tea.Msg { return shared.GoBackMsg{} }, true
	case "r":
		return tea.Batch(m.refresh(), m.loadAlerts()), true
	case "d":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDocker, ServerID: m.ctx.ServerID}
		}, true
	case "p":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenPM2, ServerID: m.ctx.ServerID}
		}, true
	case "l":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenLogs, ServerID: m.ctx.ServerID}
		}, true
	case "m":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenServerMap, ServerID: m.ctx.ServerID}
		}, true
	case "e":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenErrTrack, ServerID: m.ctx.ServerID}
		}, true
	case "c":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenChat, ServerID: m.ctx.ServerID}
		}, true
	case "u":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenBackup, ServerID: m.ctx.ServerID}
		}, true
	case "g":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDatabase, ServerID: m.ctx.ServerID}
		}, true
	case "x":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenProxy, ServerID: m.ctx.ServerID}
		}, true
	case ",":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenSettings, ServerID: m.ctx.ServerID}
		}, true
	}
	return nil, false
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Server Dashboard")

	gw := (m.width - 8) / 3
	if gw < 18 {
		gw = 18
	}
	gCPU := components.NewGauge("CPU", m.cpu)
	gCPU.Width = gw
	gRAM := components.NewGauge("RAM", m.ram)
	gRAM.Width = gw
	gDisk := components.NewGauge("DSK", m.disk)
	gDisk.Width = gw
	gauges := lipgloss.JoinHorizontal(lipgloss.Top, gCPU.View(), "  ", gRAM.View(), "  ", gDisk.View())

	errLine := ""
	if m.fetchErr != "" {
		errLine = "\n " + theme.ErrorText().Render(m.fetchErr)
	}

	leftW := (m.width * 58) / 100
	if leftW < 32 {
		leftW = 32
	}
	rightW := m.width - leftW - 2
	if rightW < 24 {
		rightW = 24
	}

	svcPanel := theme.PanelStyle().Width(leftW).Render(m.serviceTable.View())
	alertsPanel := theme.PanelStyle().Width(rightW).Render(m.renderAlerts())
	main := lipgloss.JoinHorizontal(lipgloss.Top, svcPanel, " ", alertsPanel)

	quick := theme.MutedText().Render(
		" d docker · p pm2 · l logs · m map · e errors · c chat · g db · x proxy · u backup · , settings · r refresh · b back ",
	)
	help := components.NewHelpBar(
		components.KeyBinding{Key: "r", Desc: "refresh"},
		components.KeyBinding{Key: "d/p/l", Desc: "docker/pm2/logs"},
		components.KeyBinding{Key: "m", Desc: "server map"},
		components.KeyBinding{Key: "b", Desc: "back"},
	)
	help.Width = m.width

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		gauges+errLine,
		main,
		quick,
		help.View(),
	)
}

func (m *Model) renderAlerts() string {
	if len(m.alerts) == 0 {
		return theme.MutedText().Render("No recent alerts.")
	}
	var b strings.Builder
	b.WriteString(theme.SubtitleStyle().Render("Recent alerts") + "\n")
	for _, a := range m.alerts {
		sev := strings.ToLower(a.Severity)
		line := fmt.Sprintf("%s · %s", a.Service, truncate(a.Message, 48))
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

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
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

func parseDiskUsage(dfOut string) float64 {
	lines := strings.Split(strings.TrimSpace(dfOut), "\n")
	if len(lines) < 2 {
		return 0
	}
	last := lines[len(lines)-1]
	m := dfUseRE.FindStringSubmatch(last)
	if len(m) < 2 {
		return 0
	}
	pct, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return min(1, float64(pct)/100)
}

func parseSystemdServices(out string) []serviceRow {
	var rows []serviceRow
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ".service")
		if name == "" {
			continue
		}
		rows = append(rows, serviceRow{name: name, active: true})
	}
	return rows
}

func parseDockerServices(out string) []serviceRow {
	var rows []serviceRow
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := parts[0]
		status := ""
		if len(parts) > 1 {
			status = parts[1]
		}
		active := strings.Contains(strings.ToLower(status), "up") || strings.Contains(strings.ToLower(status), "running")
		rows = append(rows, serviceRow{name: name, active: active})
	}
	return rows
}
