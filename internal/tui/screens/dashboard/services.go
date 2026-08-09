package dashboard

import (
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type serviceEntry struct {
	kind   string
	name   string
	status string
	detail string
	active bool
}

type svcFilterMode int

const (
	filterAll svcFilterMode = iota
	filterRunning
	filterFailed
)

func (f svcFilterMode) next() svcFilterMode {
	switch f {
	case filterAll:
		return filterRunning
	case filterRunning:
		return filterFailed
	default:
		return filterAll
	}
}

func (f svcFilterMode) label() string {
	switch f {
	case filterRunning:
		return "running"
	case filterFailed:
		return "failed"
	default:
		return "all"
	}
}

type servicesLoadedMsg struct {
	services []serviceEntry
	err      error
}

func (m *Model) loadServices() tea.Cmd {
	m.servicesState = stateLoading
	serverID := m.ctx.ServerID
	pool := m.ctx.Pool
	return func() tea.Msg {
		ex, ok := pool.GetExecutor(serverID)
		if !ok {
			return servicesLoadedMsg{err: errNotConnected}
		}

		var systemdOut, dockerOut string
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			systemdOut = ex.RunQuiet("systemctl list-units --type=service --state=running,failed --no-pager --no-legend 2>/dev/null")
		}()
		go func() {
			defer wg.Done()
			dockerOut = ex.RunQuiet(`docker ps -a --format '{{.Names}}	{{.Status}}	{{.Ports}}' 2>/dev/null`)
		}()
		wg.Wait()

		services := parseSystemdEntries(systemdOut)
		services = append(services, parseDockerEntries(dockerOut)...)
		return servicesLoadedMsg{services: services}
	}
}

func parseSystemdEntries(out string) []serviceEntry {
	var rows []serviceEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ".service")
		activeState := fields[2]
		sub := fields[3]
		active := activeState == "active"
		rows = append(rows, serviceEntry{
			kind:   "systemd",
			name:   name,
			status: sub,
			detail: strings.Join(fields[4:], " "),
			active: active,
		})
	}
	return rows
}

func parseDockerEntries(out string) []serviceEntry {
	var rows []serviceEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		name := parts[0]
		status := ""
		ports := ""
		if len(parts) > 1 {
			status = parts[1]
		}
		if len(parts) > 2 {
			ports = parts[2]
		}
		low := strings.ToLower(status)
		active := strings.Contains(low, "up") || strings.Contains(low, "running")
		rows = append(rows, serviceEntry{
			kind:   "docker",
			name:   name,
			status: status,
			detail: ports,
			active: active,
		})
	}
	return rows
}

func (m *Model) filteredServices() []serviceEntry {
	switch m.svcFilter {
	case filterRunning:
		var out []serviceEntry
		for _, s := range m.services {
			if s.active {
				out = append(out, s)
			}
		}
		return out
	case filterFailed:
		var out []serviceEntry
		for _, s := range m.services {
			if !s.active {
				out = append(out, s)
			}
		}
		return out
	default:
		return m.services
	}
}

func (m *Model) svcLocalChrome() int {
	return components.FrameChromeRows(true) + components.TabBarRows() + 2
}

func (m *Model) rebuildSvcTable() {
	filtered := m.filteredServices()
	inner := layout.ContentWidth(m.width)
	cols := layout.AdaptiveColumns(inner, []table.Column{
		{Title: " ", Width: 3},
		{Title: "Type", Width: 8},
		{Title: "Name", Width: 22},
		{Title: "Status", Width: 14},
		{Title: "Detail", Width: 0},
	})
	detailW := 24
	fixed := 0
	for _, c := range cols {
		if c.Title != "Detail" {
			fixed += c.Width
		} else if c.Width > 0 {
			detailW = c.Width
		}
	}
	if detailW == 24 && inner > fixed+4 {
		detailW = inner - fixed - 4
	}
	rows := make([]table.Row, len(filtered))
	for i, s := range filtered {
		rows[i] = table.Row{
			theme.StatusDot(s.active),
			s.kind,
			components.Truncate(s.name, 22),
			components.Truncate(s.status, 14),
			components.Truncate(s.detail, detailW),
		}
	}
	h := layout.BodyHeight(m.height, m.svcLocalChrome(), 5)
	m.svcTable = m.svcTable.SetData(inner, cols, rows, h)
}

func (m *Model) renderServices() string {
	filterLabel := theme.MutedText().Render("filter: " + m.svcFilter.label() + "  (a cycle)")
	if m.servicesState == stateLoading && len(m.services) == 0 {
		return filterLabel + "\n" + loadingText("Loading services…")
	}
	if m.servicesState == stateError {
		inner := layout.ContentWidth(m.width)
		return filterLabel + "\n" + theme.ErrorText().Render(components.Wrap(m.servicesErr, inner))
	}
	return filterLabel + "\n" + m.svcTable.View()
}
