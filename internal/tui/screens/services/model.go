package services

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/services/bugsink"
	"github.com/lyracorp/xmanager/internal/services/databasus"
	"github.com/lyracorp/xmanager/internal/services/gitea"
	"github.com/lyracorp/xmanager/internal/services/kafka"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/mattermost"
	"github.com/lyracorp/xmanager/internal/services/netdata"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/services/rabbitmq"
	"github.com/lyracorp/xmanager/internal/services/registry"
	"github.com/lyracorp/xmanager/internal/services/rustfs"
	"github.com/lyracorp/xmanager/internal/services/umami"
	"github.com/lyracorp/xmanager/internal/services/uptimekuma"
	"github.com/lyracorp/xmanager/internal/services/webpanel"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
)

var optionalServices = []string{
	"webpanel",
	"registry", "gitea", "rustfs", "rabbitmq", "kafka",
	"mattermost", "bugsink", "netdata", "umami", "powerdns",
	"mailinbox", "uptimekuma", "databasus",
}

type instancesLoadedMsg struct{ instances []storage.ServiceInstance }
type toggleResultMsg struct {
	serviceType string
	enabled     bool
	status      string
	err         error
}

type Model struct {
	ctx       *shared.AppContext
	instances []storage.ServiceInstance
	tbl       components.ListTable
	message   string
	width     int
	height    int
	busy      bool
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string                        { return "Services" }
func (m *Model) SetSize(w, h int)                    { m.width = w; m.height = h; m.rebuildTable() }
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
		{Title: "Status", Width: 24},
		{Title: "Notes", Width: 0},
	}
	rows := make([]table.Row, len(m.instances))
	for i, si := range m.instances {
		enabled := "[ ]"
		if si.Enabled {
			enabled = "[✓]"
		}
		note := ""
		if si.ServiceType == "webpanel" {
			note = "node metrics UI on :8080"
		}
		rows[i] = table.Row{enabled, si.ServiceType, si.Status, note}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(true)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case instancesLoadedMsg:
		m.instances = msg.instances
		m.busy = false
		m.rebuildTable()
		return m, nil
	case toggleResultMsg:
		m.busy = false
		if msg.err != nil {
			m.message = fmt.Sprintf("%s failed: %v", msg.serviceType, msg.err)
			return m, m.load()
		}
		action := "disabled"
		if msg.enabled {
			action = "enabled"
		}
		m.message = fmt.Sprintf("%s %s (%s)", msg.serviceType, action, msg.status)
		return m, m.load()
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		switch msg.String() {
		case "enter", " ":
			m.busy = true
			m.message = "Working…"
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
	enable := !si.Enabled
	serverID := m.ctx.ServerID
	svcType := si.ServiceType

	return func() tea.Msg {
		exec, err := m.ensureExec()
		if err != nil {
			return toggleResultMsg{serviceType: svcType, err: err}
		}
		svc := lookupService(svcType, m.ctx, serverID)
		if svc == nil {
			return toggleResultMsg{serviceType: svcType, err: fmt.Errorf("unknown service %s", svcType)}
		}
		if enable {
			cfg := map[string]string{"port": "8080"}
			if err := svc.Enable(exec, cfg); err != nil {
				return toggleResultMsg{serviceType: svcType, err: err}
			}
			return toggleResultMsg{serviceType: svcType, enabled: true, status: svc.Status(exec)}
		}
		if err := svc.Disable(exec); err != nil {
			return toggleResultMsg{serviceType: svcType, err: err}
		}
		return toggleResultMsg{serviceType: svcType, enabled: false, status: "stopped"}
	}
}

func (m *Model) ensureExec() (*ssh.Executor, error) {
	if exec, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID); ok {
		return exec, nil
	}
	var srv storage.Server
	if err := m.ctx.DB.First(&srv, m.ctx.ServerID).Error; err != nil {
		return nil, err
	}
	_, err := m.ctx.Pool.Connect(srv.ID, ssh.ClientConfig{
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
	exec, ok := m.ctx.Pool.GetExecutor(srv.ID)
	if !ok {
		return nil, fmt.Errorf("executor unavailable")
	}
	return exec, nil
}

func lookupService(svcType string, ctx *shared.AppContext, serverID uint) svcs.Service {
	db := ctx.DB
	switch svcType {
	case "webpanel":
		wp := webpanel.New(db, serverID)
		var srv storage.Server
		if ctx.DB.First(&srv, serverID).Error == nil {
			wp.SetHost(srv.Host)
			wp.SetSSH(ssh.ClientConfig{
				Host:     srv.Host,
				Port:     srv.Port,
				User:     srv.User,
				KeyPath:  srv.SSHKeyPath,
				Password: srv.Password,
				JumpHost: srv.JumpHost,
			})
		}
		wp.SetPool(ctx.Pool)
		return wp
	case "gitea":
		return gitea.New(db, serverID)
	case "kafka":
		return kafka.New(db, serverID)
	case "mattermost":
		return mattermost.New(db, serverID)
	case "rabbitmq":
		return rabbitmq.New(db, serverID)
	case "registry":
		return registry.New(db, serverID)
	case "rustfs":
		return rustfs.New(db, serverID)
	case "bugsink":
		return bugsink.New(db, serverID)
	case "netdata":
		return netdata.New(db, serverID)
	case "umami":
		return umami.New(db, serverID)
	case "powerdns":
		return powerdns.New(db, serverID)
	case "mailinbox":
		return mailinbox.New(db, serverID)
	case "uptimekuma":
		return uptimekuma.New(db, serverID)
	case "databasus":
		return databasus.New(db, serverID)
	default:
		return nil
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
