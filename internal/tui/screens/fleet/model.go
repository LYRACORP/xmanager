package fleet

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/services/webpanel"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeGrid mode = iota
	modeAdd
	modeConfirmDelete
	modeConfirmWebInstall
	modeConfirmWebUninstall
)

type formField int

const (
	fieldName formField = iota
	fieldHost
	fieldPort
	fieldUser
	fieldKeyPath
	fieldPassword
	fieldTags
	fieldCount
)

type serversLoadedMsg struct {
	servers   []storage.Server
	snapshots map[uint]storage.ServerMetricSnapshot
	webPanels map[uint]bool
}

type connectResultMsg struct {
	serverID uint
	ok       bool
	err      error
}

type webPanelResultMsg struct {
	serverID  uint
	installed bool
	url       string
	err       error
}

type Model struct {
	ctx       *shared.AppContext
	servers   []storage.Server
	snaps     map[uint]storage.ServerMetricSnapshot
	webPanels map[uint]bool
	cursor    int
	mode      mode
	form      [fieldCount]textinput.Model
	formIdx   int
	deleteID  uint
	message   string
	width     int
	height    int
	busy      bool
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{
		ctx:       ctx,
		snaps:     make(map[uint]storage.ServerMetricSnapshot),
		webPanels: make(map[uint]bool),
	}
	m.initForm()
	return m
}

func (m *Model) Name() string     { return "Fleet Overview" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeAdd:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "shift+tab", Desc: "prev field"},
			{Key: "enter", Desc: "save"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeConfirmDelete, modeConfirmWebInstall, modeConfirmWebUninstall:
		return []components.KeyBinding{
			{Key: "y", Desc: "confirm"},
			{Key: "n/esc", Desc: "cancel"},
		}
	default:
		return []components.KeyBinding{
			{Key: "enter", Desc: "connect"},
			{Key: "w", Desc: "web panel"},
			{Key: "a", Desc: "add"},
			{Key: "d", Desc: "delete"},
			{Key: "r", Desc: "refresh"},
			{Key: "j/k", Desc: "navigate"},
		}
	}
}

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var servers []storage.Server
		m.ctx.DB.Order("name asc").Find(&servers)
		snaps, _ := poller.LatestSnapshots(m.ctx.DB)
		web := make(map[uint]bool)
		var instances []storage.ServiceInstance
		m.ctx.DB.Where("service_type = ? AND enabled = ?", webpanel.ServiceType, true).Find(&instances)
		for _, si := range instances {
			web[si.ServerID] = true
		}
		return serversLoadedMsg{servers: servers, snapshots: snaps, webPanels: web}
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case serversLoadedMsg:
		m.servers = msg.servers
		if msg.snapshots != nil {
			m.snaps = msg.snapshots
		}
		if msg.webPanels != nil {
			m.webPanels = msg.webPanels
		}
		if m.cursor >= len(m.servers) && len(m.servers) > 0 {
			m.cursor = len(m.servers) - 1
		}
		m.busy = false
		return m, nil

	case poller.MetricsUpdatedMsg:
		m.snaps[msg.ServerID] = msg.Snapshot
		return m, nil

	case connectResultMsg:
		if msg.ok {
			m.message = "Connected — opening dashboard…"
			return m, func() tea.Msg {
				return shared.ConnectServerMsg{ServerID: msg.serverID}
			}
		}
		m.message = fmt.Sprintf("Connection failed: %v", msg.err)
		m.busy = false
		return m, nil

	case webPanelResultMsg:
		m.busy = false
		if msg.err != nil {
			m.message = fmt.Sprintf("Web panel failed: %v", msg.err)
			return m, nil
		}
		m.webPanels[msg.serverID] = msg.installed
		if msg.installed {
			m.message = fmt.Sprintf("Web panel installed — %s", msg.url)
		} else {
			m.message = "Web panel uninstalled"
		}
		return m, m.load()

	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		switch m.mode {
		case modeGrid:
			return m.updateGrid(msg)
		case modeAdd:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateDelete(msg)
		case modeConfirmWebInstall, modeConfirmWebUninstall:
			return m.updateWebConfirm(msg)
		}
	}
	return m, nil
}

func (m *Model) updateGrid(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	count := len(m.servers)
	cols := m.columns()
	switch msg.String() {
	case "j", "down":
		if m.cursor+cols < count {
			m.cursor += cols
		}
	case "k", "up":
		if m.cursor-cols >= 0 {
			m.cursor -= cols
		}
	case "l", "right":
		if m.cursor+1 < count {
			m.cursor++
		}
	case "h", "left":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if count == 0 {
			return m, nil
		}
		s := m.servers[m.cursor]
		m.message = "Connecting…"
		m.busy = true
		return m, m.connect(s)
	case "w":
		if count == 0 {
			return m, nil
		}
		s := m.servers[m.cursor]
		if m.webPanels[s.ID] {
			m.mode = modeConfirmWebUninstall
		} else {
			m.mode = modeConfirmWebInstall
		}
		m.message = ""
		return m, nil
	case "a":
		m.mode = modeAdd
		m.initForm()
		m.formIdx = 0
		m.form[0].Focus()
		m.message = ""
		return m, nil
	case "d", "delete":
		if count > 0 {
			m.deleteID = m.servers[m.cursor].ID
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "r":
		m.message = "Refreshing…"
		return m, m.load()
	}
	return m, nil
}

func (m *Model) updateWebConfirm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.cursor < 0 || m.cursor >= len(m.servers) {
			m.mode = modeGrid
			return m, nil
		}
		s := m.servers[m.cursor]
		install := m.mode == modeConfirmWebInstall
		m.mode = modeGrid
		m.busy = true
		if install {
			m.message = fmt.Sprintf("Installing web panel on %s…", s.Name)
		} else {
			m.message = fmt.Sprintf("Uninstalling web panel from %s…", s.Name)
		}
		return m, m.toggleWebPanel(s, install)
	case "n", "N", "esc":
		m.mode = modeGrid
		return m, nil
	}
	return m, nil
}

func (m *Model) toggleWebPanel(s storage.Server, install bool) tea.Cmd {
	return func() tea.Msg {
		cfg := ssh.ClientConfig{
			Host:     s.Host,
			Port:     s.Port,
			User:     s.User,
			KeyPath:  s.SSHKeyPath,
			Password: s.Password,
			JumpHost: s.JumpHost,
		}
		_, err := m.ctx.Pool.Reconnect(s.ID, cfg)
		if err != nil {
			return webPanelResultMsg{serverID: s.ID, err: fmt.Errorf("reconnect: %w", err)}
		}
		exec, ok := m.ctx.Pool.GetExecutor(s.ID)
		if !ok {
			return webPanelResultMsg{serverID: s.ID, err: fmt.Errorf("executor unavailable")}
		}
		svc := webpanel.New(m.ctx.DB, s.ID)
		svc.SetHost(s.Host)
		svc.SetSSH(cfg)
		if install {
			if err := svc.Enable(exec, map[string]string{"port": "8080"}); err != nil {
				return webPanelResultMsg{serverID: s.ID, err: err}
			}
			return webPanelResultMsg{
				serverID:  s.ID,
				installed: true,
				url:       fmt.Sprintf("http://%s:8080", s.Host),
			}
		}
		if err := svc.Disable(exec); err != nil {
			return webPanelResultMsg{serverID: s.ID, err: err}
		}
		return webPanelResultMsg{serverID: s.ID, installed: false}
	}
}

func (m *Model) ensureExec(s storage.Server) (*ssh.Executor, error) {
	if exec, ok := m.ctx.Pool.GetExecutor(s.ID); ok {
		return exec, nil
	}
	_, err := m.ctx.Pool.Connect(s.ID, ssh.ClientConfig{
		Host:     s.Host,
		Port:     s.Port,
		User:     s.User,
		KeyPath:  s.SSHKeyPath,
		Password: s.Password,
		JumpHost: s.JumpHost,
	})
	if err != nil {
		return nil, err
	}
	exec, ok := m.ctx.Pool.GetExecutor(s.ID)
	if !ok {
		return nil, fmt.Errorf("executor unavailable after connect")
	}
	return exec, nil
}

func (m *Model) updateForm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeGrid
		return m, nil
	case "tab", "down":
		m.form[m.formIdx].Blur()
		m.formIdx = (m.formIdx + 1) % int(fieldCount)
		m.form[m.formIdx].Focus()
		return m, nil
	case "shift+tab", "up":
		m.form[m.formIdx].Blur()
		m.formIdx = (m.formIdx - 1 + int(fieldCount)) % int(fieldCount)
		m.form[m.formIdx].Focus()
		return m, nil
	case "enter":
		if m.formIdx < int(fieldCount)-1 {
			m.form[m.formIdx].Blur()
			m.formIdx++
			m.form[m.formIdx].Focus()
			return m, nil
		}
		return m, m.saveServer()
	}
	var cmd tea.Cmd
	m.form[m.formIdx], cmd = m.form[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) updateDelete(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.ctx.DB.Delete(&storage.Server{}, m.deleteID)
		m.ctx.Pool.Disconnect(m.deleteID)
		if m.cursor > 0 {
			m.cursor--
		}
		m.mode = modeGrid
		return m, m.load()
	default:
		m.mode = modeGrid
	}
	return m, nil
}

func (m *Model) connect(s storage.Server) tea.Cmd {
	return func() tea.Msg {
		_, err := m.ctx.Pool.Connect(s.ID, ssh.ClientConfig{
			Host:     s.Host,
			Port:     s.Port,
			User:     s.User,
			KeyPath:  s.SSHKeyPath,
			Password: s.Password,
			JumpHost: s.JumpHost,
		})
		return connectResultMsg{serverID: s.ID, ok: err == nil, err: err}
	}
}

func (m *Model) initForm() {
	labels := [fieldCount]string{"Name", "Host", "Port", "User", "SSH Key Path", "Password", "Tags"}
	hints := [fieldCount]string{"my-server", "192.168.1.1", "22", "root", "~/.ssh/id_rsa", "", "web,prod"}
	for i := range m.form {
		ti := textinput.New()
		ti.Placeholder = hints[i]
		ti.Prompt = labels[i] + ": "
		ti.Width = 40
		if formField(i) == fieldPassword {
			ti.EchoMode = textinput.EchoPassword
		}
		m.form[i] = ti
	}
	m.form[fieldPort].SetValue("22")
	m.form[fieldUser].SetValue("root")
	m.form[fieldKeyPath].SetValue("~/.ssh/id_rsa")
}

func (m *Model) saveServer() tea.Cmd {
	return func() tea.Msg {
		port := 22
		_, _ = fmt.Sscanf(m.form[fieldPort].Value(), "%d", &port)
		srv := storage.Server{
			Name:       m.form[fieldName].Value(),
			Host:       m.form[fieldHost].Value(),
			Port:       port,
			User:       m.form[fieldUser].Value(),
			SSHKeyPath: m.form[fieldKeyPath].Value(),
			Password:   m.form[fieldPassword].Value(),
			Tags:       m.form[fieldTags].Value(),
		}
		m.ctx.DB.Create(&srv)
		m.mode = modeGrid
		var servers []storage.Server
		m.ctx.DB.Order("name asc").Find(&servers)
		snaps, _ := poller.LatestSnapshots(m.ctx.DB)
		web := make(map[uint]bool)
		var instances []storage.ServiceInstance
		m.ctx.DB.Where("service_type = ? AND enabled = ?", webpanel.ServiceType, true).Find(&instances)
		for _, si := range instances {
			web[si.ServerID] = true
		}
		return serversLoadedMsg{servers: servers, snapshots: snaps, webPanels: web}
	}
}

func (m *Model) columns() int {
	if m.width >= 100 {
		return 2
	}
	return 1
}

func (m *Model) View() string {
	switch m.mode {
	case modeAdd:
		return m.viewForm()
	case modeConfirmDelete:
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render("Delete this server? (y/n)"),
		)
	case modeConfirmWebInstall:
		name := m.selectedName()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render(fmt.Sprintf("Install XManager web panel on %s? (y/n)", name)),
			" "+theme.MutedText().Render("Deploys binary/systemd (or Docker) on :8080"),
		)
	case modeConfirmWebUninstall:
		name := m.selectedName()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render(fmt.Sprintf("Uninstall web panel from %s? (y/n)", name)),
		)
	default:
		return m.viewGrid()
	}
}

func (m *Model) selectedName() string {
	if m.cursor >= 0 && m.cursor < len(m.servers) {
		return m.servers[m.cursor].Name
	}
	return "server"
}

func (m *Model) viewGrid() string {
	header := theme.ScreenChrome("Fleet Overview", "all servers at a glance", m.width)
	var rows []string

	cols := m.columns()
	cardW := (m.width - 4) / cols
	if cardW < 30 {
		cardW = 30
	}

	for i := 0; i < len(m.servers); i += cols {
		var cards []string
		for c := 0; c < cols && i+c < len(m.servers); c++ {
			idx := i + c
			cards = append(cards, m.renderCard(m.servers[idx], idx == m.cursor, cardW))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}

	if len(m.servers) == 0 {
		rows = append(rows, "  "+theme.MutedText().Render("No servers yet — press 'a' to add one"))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	msg := ""
	if m.message != "" {
		msg = "\n " + m.message
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, msg)
}

func (m *Model) renderCard(s storage.Server, selected bool, width int) string {
	snap := m.snaps[s.ID]

	online := snap.Online || s.IsActive
	badge := theme.StatusDot(online)
	badgeLabel := "offline"
	if online {
		badgeLabel = "online"
	}

	nameLine := lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Text).
		Render(s.Name) + "  " + badge + " " + theme.MutedText().Render(badgeLabel)

	if m.webPanels[s.ID] {
		nameLine += "  " + theme.SuccessBadge().Render(" web ")
	}

	hostLine := theme.MutedText().Render(fmt.Sprintf("%s:%d  user:%s", s.Host, s.Port, s.User))

	tagsLine := ""
	if s.Tags != "" {
		tagsLine = theme.MutedText().Render("tags: " + s.Tags)
	}

	gW := width - 14
	if gW < 10 {
		gW = 10
	}
	cpuGauge := components.Gauge{Label: "CPU", Value: snap.CPUPct / 100, Width: gW, ShowPct: true}
	ramGauge := components.Gauge{Label: "RAM", Value: snap.RAMPct / 100, Width: gW, ShowPct: true}
	dskGauge := components.Gauge{Label: "DSK", Value: snap.DiskPct / 100, Width: gW, ShowPct: true}

	netLine := theme.MutedText().Render(fmt.Sprintf(
		"net ↓%.1f ↑%.1f KB/s  containers:%d",
		snap.NetRxKBps, snap.NetTxKBps, snap.ContainerCount,
	))

	lastSeen := "never"
	if s.LastSeen != nil {
		lastSeen = humanDuration(time.Since(*s.LastSeen))
	}
	uptimeLine := theme.MutedText().Render(fmt.Sprintf("last seen: %s  status: %s", lastSeen, snap.UptimeStatus))

	lines := []string{nameLine, hostLine}
	if tagsLine != "" {
		lines = append(lines, tagsLine)
	}
	lines = append(lines,
		"",
		cpuGauge.View(),
		ramGauge.View(),
		dskGauge.View(),
		"",
		netLine,
		uptimeLine,
	)
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)

	style := theme.PanelStyle().Width(width - 2).MarginRight(1)
	if selected {
		style = theme.ActivePanelStyle().Width(width - 2).MarginRight(1)
	}
	return style.Render(body)
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Add Server", "new SSH target", m.width)
	var form strings.Builder
	for i := range m.form {
		ti := components.ApplyInputTheme(m.form[i], m.width, i == m.formIdx)
		form.WriteString(components.RenderFormField("", ti.View(), m.width, i == m.formIdx))
		form.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Shift+Tab: prev  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", form.String(), footer)
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
