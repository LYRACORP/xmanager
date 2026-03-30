package servermap

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
	"gorm.io/gorm"
)

type profileLoadedMsg struct {
	profile *storage.ServerProfile
	services []mapService
	parseErr error
}

type mapService struct {
	Name   string
	Tech   []string
	Status string
	Port   int
}

type Model struct {
	ctx           *shared.AppContext
	width, height int
	services      []mapService
	svcTable      table.Model
	profile       *storage.ServerProfile
	parseErr      string
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "Server Map" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.loadProfile
}

func (m *Model) loadProfile() tea.Msg {
	if m.ctx.DB == nil || m.ctx.ServerID == 0 {
		return profileLoadedMsg{parseErr: fmt.Errorf("no server context")}
	}
	var prof storage.ServerProfile
	err := m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("scanned_at desc").First(&prof).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return profileLoadedMsg{}
		}
		return profileLoadedMsg{parseErr: err}
	}
	services, perr := parseProfileJSON(prof.ProfileJSON)
	return profileLoadedMsg{profile: &prof, services: services, parseErr: perr}
}

func (m *Model) rebuildTable() {
	cols := []table.Column{
		{Title: " ", Width: 3},
		{Title: "Service", Width: 22},
		{Title: "Tech", Width: 24},
		{Title: "Port", Width: 6},
		{Title: "Status", Width: 12},
	}
	rows := make([]table.Row, len(m.services))
	for i, s := range m.services {
		active := statusActive(s.Status)
		tech := strings.Join(s.Tech, ", ")
		if len(tech) > 22 {
			tech = tech[:19] + "..."
		}
		port := ""
		if s.Port > 0 {
			port = fmt.Sprintf("%d", s.Port)
		}
		rows[i] = table.Row{
			theme.StatusDot(active),
			s.Name,
			tech,
			port,
			s.Status,
		}
	}
	h := m.height - 10
	if h < 5 {
		h = 5
	}
	m.svcTable = components.StyledTable(cols, rows, h)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case profileLoadedMsg:
		m.profile = msg.profile
		m.services = msg.services
		if msg.parseErr != nil {
			m.parseErr = msg.parseErr.Error()
		} else {
			m.parseErr = ""
		}
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		if cmd, ok := m.handleKeys(msg); ok {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.svcTable, cmd = m.svcTable.Update(msg)
	return m, cmd
}

func (m *Model) handleKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "b":
		return func() tea.Msg { return shared.GoBackMsg{} }, true
	case "r":
		return m.loadProfile, true
	case "enter":
		if m.ctx.ServerID == 0 {
			return nil, true
		}
		idx := m.svcTable.Cursor()
		if idx < 0 || idx >= len(m.services) {
			return nil, true
		}
		sel := m.services[idx]
		return func() tea.Msg {
			return shared.NavigateMsg{
				Screen:   shared.ScreenDocker,
				ServerID: m.ctx.ServerID,
				Params: map[string]interface{}{
					"service": sel.Name,
					"port":    sel.Port,
					"tech":    sel.Tech,
				},
			}
		}, true
	case "p":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenPM2, ServerID: m.ctx.ServerID}
		}, true
	case "l":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenLogs, ServerID: m.ctx.ServerID}
		}, true
	case "d":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDashboard, ServerID: m.ctx.ServerID}
		}, true
	}
	return nil, false
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Server Map (AI profile)")

	meta := ""
	if m.profile != nil {
		meta = theme.MutedText().Render(
			fmt.Sprintf("  Scanned: %s", m.profile.ScannedAt.Format("2006-01-02 15:04")),
		)
	}

	errLine := ""
	if m.parseErr != "" {
		errLine = "\n " + theme.WarningText().Render(m.parseErr)
	}

	if m.profile == nil && m.parseErr == "" && len(m.services) == 0 {
		body := theme.MutedText().Render(
			"\n  No server profile in database. Run a scan or AI discovery for this host,\n  then open Server Map again.",
		)
		help := components.NewHelpBar(
			components.KeyBinding{Key: "r", Desc: "reload"},
			components.KeyBinding{Key: "b", Desc: "back"},
		)
		help.Width = m.width
		return lipgloss.JoinVertical(lipgloss.Left, title, body, errLine, help.View())
	}

	help := components.NewHelpBar(
		components.KeyBinding{Key: "enter", Desc: "open docker context"},
		components.KeyBinding{Key: "p/l", Desc: "pm2/logs"},
		components.KeyBinding{Key: "d", Desc: "dashboard"},
		components.KeyBinding{Key: "r", Desc: "reload"},
		components.KeyBinding{Key: "b", Desc: "back"},
	)
	help.Width = m.width

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title+meta,
		m.svcTable.View()+errLine,
		help.View(),
	)
}

func statusActive(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "", "unknown", "stopped", "down", "exited", "inactive":
		return false
	}
	return strings.Contains(s, "run") || strings.Contains(s, "up") || strings.Contains(s, "active") || strings.Contains(s, "healthy")
}

type profileEnvelope struct {
	Services []profileService `json:"services"`
}

type profileService struct {
	Name   string   `json:"name"`
	Tech   []string `json:"tech"`
	Tags   []string `json:"tags"`
	Status string   `json:"status"`
	State  string   `json:"state"`
	Port   int      `json:"port"`
}

func parseProfileJSON(raw string) ([]mapService, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var env profileEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err == nil && len(env.Services) > 0 {
		return normalizeServices(env.Services), nil
	}

	var list []profileService
	if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
		return normalizeServices(list), nil
	}

	var envAlt struct {
		Services []profileService `json:"detected_services"`
	}
	if err := json.Unmarshal([]byte(raw), &envAlt); err == nil && len(envAlt.Services) > 0 {
		return normalizeServices(envAlt.Services), nil
	}

	if err := json.Unmarshal([]byte(raw), &env); err == nil && len(env.Services) == 0 {
		return nil, fmt.Errorf("profile JSON has no services array")
	}
	return nil, fmt.Errorf("could not parse profile JSON")
}

func normalizeServices(in []profileService) []mapService {
	out := make([]mapService, 0, len(in))
	for _, s := range in {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		tech := append([]string{}, s.Tech...)
		for _, t := range s.Tags {
			t = strings.TrimSpace(t)
			if t != "" {
				tech = append(tech, t)
			}
		}
		tech = dedupeStrings(tech)
		status := strings.TrimSpace(s.Status)
		if status == "" {
			status = strings.TrimSpace(s.State)
		}
		if status == "" {
			status = "unknown"
		}
		out = append(out, mapService{
			Name:   name,
			Tech:   tech,
			Status: status,
			Port:   s.Port,
		})
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}
