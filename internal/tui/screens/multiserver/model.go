package multiserver

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type serversLoadedMsg struct {
	servers []storage.Server
	err     error
}

type Model struct {
	ctx     *shared.AppContext
	width   int
	height  int
	message string
	servers []storage.Server
	table   components.ListTable
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "Multi-Server" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "enter", Desc: "open dashboard"},
		{Key: "r", Desc: "refresh"},
	}
}

func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) Init() tea.Cmd {
	return m.loadServers
}

func (m *Model) loadServers() tea.Msg {
	if m.ctx.DB == nil {
		return serversLoadedMsg{err: fmt.Errorf("database not available")}
	}
	var list []storage.Server
	if err := m.ctx.DB.Order("name asc").Find(&list).Error; err != nil {
		return serversLoadedMsg{err: err}
	}
	return serversLoadedMsg{servers: list}
}

func (m *Model) summary(s storage.Server) string {
	if m.ctx.Pool != nil {
		if _, ok := m.ctx.Pool.GetClient(s.ID); ok {
			return theme.SuccessText().Render("SSH live") + " · :" + fmt.Sprintf("%d", s.Port)
		}
	}
	if s.LastSeen != nil {
		return "seen " + humanSince(*s.LastSeen) + " · :" + fmt.Sprintf("%d", s.Port)
	}
	return "never seen · :" + fmt.Sprintf("%d", s.Port)
}

func humanSince(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "<1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 48*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (m *Model) rebuildTable() {
	localChrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, localChrome, 5)
	cols := []table.Column{
		{Title: "  ", Width: 3},
		{Title: "Name", Width: 18},
		{Title: "Host", Width: 22},
		{Title: "User", Width: 12},
		{Title: "Summary", Width: 0},
	}
	rows := make([]table.Row, len(m.servers))
	for i, s := range m.servers {
		rows[i] = table.Row{
			theme.StatusDot(s.IsActive),
			s.Name,
			s.Host,
			s.User,
			m.summary(s),
		}
	}
	m.table = m.table.SetData(m.width, cols, rows, h)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case serversLoadedMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.servers = msg.servers
			m.message = ""
		}
		m.rebuildTable()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			return m, m.loadServers
		case "enter":
			idx := m.table.Cursor()
			if idx >= 0 && idx < len(m.servers) {
				sid := m.servers[idx].ID
				return m, func() tea.Msg {
					return shared.NavigateMsg{
						Screen:   shared.ScreenDashboard,
						ServerID: sid,
					}
				}
			}
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	localChrome := components.FrameChromeRows(true) + 1
	var body string
	if len(m.servers) == 0 {
		body = theme.EmptyStateText()
	} else {
		body = m.table.View()
	}

	out := components.ScreenFrame{
		Title:       "All servers",
		Subtitle:    "multi-server overview",
		Width:       m.width,
		Body:        body,
		LocalChrome: localChrome,
	}.View()

	if m.message != "" {
		return lipgloss.JoinVertical(lipgloss.Left, out, "\n "+m.message)
	}
	return out
}

var _ shared.Screen = (*Model)(nil)
