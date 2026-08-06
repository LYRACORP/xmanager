package services

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
)

// optionalServices lists all toggleable optional service types.
var optionalServices = []string{
	"registry", "gitea", "rustfs", "rabbitmq", "kafka",
	"mattermost", "sentry", "netdata", "umami", "powerdns",
	"mailinbox",
}

type instancesLoadedMsg struct{ instances []storage.ServiceInstance }
type toggleResultMsg struct{ serviceType string; enabled bool }

// Model shows optional services for the current server and lets the user toggle them.
type Model struct {
	ctx       *shared.AppContext
	instances []storage.ServiceInstance
	tbl       components.ListTable
	message   string
	width     int
	height    int
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "Services" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "enter/space", Desc: "toggle"},
		{Key: "r", Desc: "refresh"},
		{Key: "b/esc", Desc: "back"},
	}
}

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var existing []storage.ServiceInstance
		m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Find(&existing)
		byType := make(map[string]storage.ServiceInstance, len(existing))
		for _, si := range existing {
			byType[si.ServiceType] = si
		}
		instances := make([]storage.ServiceInstance, len(optionalServices))
		for i, svcType := range optionalServices {
			if si, ok := byType[svcType]; ok {
				instances[i] = si
			} else {
				instances[i] = storage.ServiceInstance{
					ServerID:    m.ctx.ServerID,
					ServiceType: svcType,
					Enabled:     false,
					Status:      "stopped",
				}
			}
		}
		return instancesLoadedMsg{instances: instances}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "Enabled", Width: 8},
		{Title: "Service", Width: 18},
		{Title: "Status", Width: 10},
		{Title: "Notes", Width: 0},
	}
	rows := make([]table.Row, len(m.instances))
	for i, si := range m.instances {
		enabled := "[ ]"
		if si.Enabled {
			enabled = "[✓]"
		}
		rows[i] = table.Row{enabled, si.ServiceType, si.Status, ""}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(true)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case instancesLoadedMsg:
		m.instances = msg.instances
		m.rebuildTable()
		return m, nil
	case toggleResultMsg:
		action := "disabled"
		if msg.enabled {
			action = "enabled"
		}
		m.message = fmt.Sprintf("%s %s", msg.serviceType, action)
		return m, m.load()
	case tea.KeyMsg:
		switch msg.String() {
		case "enter", " ":
			return m, m.toggle()
		case "r":
			return m, m.load()
		case "b", "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		}
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *Model) toggle() tea.Cmd {
	idx := m.tbl.Cursor()
	if idx < 0 || idx >= len(m.instances) {
		return nil
	}
	si := m.instances[idx]
	newEnabled := !si.Enabled
	return func() tea.Msg {
		if si.ID == 0 {
			si.Enabled = newEnabled
			si.Status = "stopped"
			if newEnabled {
				si.Status = "running"
			}
			m.ctx.DB.Create(&si)
		} else {
			status := "stopped"
			if newEnabled {
				status = "running"
			}
			m.ctx.DB.Model(&si).Updates(map[string]interface{}{"enabled": newEnabled, "status": status})
		}
		return toggleResultMsg{serviceType: si.ServiceType, enabled: newEnabled}
	}
}

func (m *Model) View() string {
	frame := components.ScreenFrame{
		Title:       "Optional Services",
		Subtitle:    fmt.Sprintf("toggle services for server #%d", m.ctx.ServerID),
		Width:       m.width,
		Body:        m.tbl.View(),
		LocalChrome: components.FrameChromeRows(true) + 1,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
