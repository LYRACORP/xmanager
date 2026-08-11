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
	modeConfirmHostKey
	modeConfirmWebInstall
	modeManageWebPanel
	modeConfirmWebUpgrade
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

type Model struct {
	ctx            *shared.AppContext
	servers        []storage.Server
	snaps          map[uint]storage.ServerMetricSnapshot
	webPanels      map[uint]bool
	cursor         int
	mode           mode
	form           [fieldCount]textinput.Model
	formIdx        int
	deleteID       uint
	hostKeyPending uint
	hostKeyHint    string
	message        string
	width          int
	height         int
	busy           bool
	webProgress    shared.WebPanelProgressState
	webProgCh      <-chan tea.Msg
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

func (m *Model) Name() string                        { return "Fleet Overview" }
func (m *Model) SetSize(w, h int)                    { m.width = w; m.height = h }
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
	case modeManageWebPanel:
		return []components.KeyBinding{
			{Key: "r", Desc: "reinstall/upgrade"},
			{Key: "u", Desc: "uninstall"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeConfirmDelete, modeConfirmHostKey, modeConfirmWebInstall, modeConfirmWebUpgrade, modeConfirmWebUninstall:
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
			m.hostKeyPending = 0
			m.hostKeyHint = ""
			return m, func() tea.Msg {
				return shared.ConnectServerMsg{ServerID: msg.serverID}
			}
		}
		m.busy = false
		if ssh.IsHostKeyMismatch(msg.err) {
			m.hostKeyPending = msg.serverID
			m.hostKeyHint = hostKeyConfirmHint(msg.err)
			m.mode = modeConfirmHostKey
			m.message = ""
			return m, nil
		}
		m.message = fmt.Sprintf("Connection failed: %v", msg.err)
		return m, nil

	case shared.WebPanelProgressMsg:
		m.webProgress.Apply(msg)
		m.message = msg.Detail
		return m, shared.WaitMsg(m.webProgCh)

	case shared.WebPanelDoneMsg:
		m.busy = false
		m.webProgCh = nil
		m.webProgress.Reset()
		if msg.Err != nil {
			m.message = fmt.Sprintf("Web panel failed: %v", msg.Err)
			return m, nil
		}
		m.webPanels[msg.ServerID] = msg.Installed
		if msg.Installed {
			m.message = fmt.Sprintf("Node web panel ready — %s", msg.URL)
		} else {
			m.message = "Web panel uninstalled"
		}
		return m, tea.Batch(shared.PlayFinishSound(), m.load())

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
		case modeConfirmHostKey:
			return m.updateHostKeyConfirm(msg)
		case modeManageWebPanel:
			return m.updateWebManage(msg)
		case modeConfirmWebInstall, modeConfirmWebUpgrade, modeConfirmWebUninstall:
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
		return m, m.connect(s, false)
	case "w":
		if count == 0 {
			return m, nil
		}
		s := m.servers[m.cursor]
		if m.webPanels[s.ID] {
			m.mode = modeManageWebPanel
			m.message = fmt.Sprintf("Node panel on %s: [r] reinstall/upgrade  [u] uninstall  [esc] cancel", s.Name)
		} else {
			m.mode = modeConfirmWebInstall
			m.message = ""
		}
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

func (m *Model) updateWebManage(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "r", "R":
		m.mode = modeConfirmWebUpgrade
		m.message = ""
		return m, nil
	case "u", "U":
		m.mode = modeConfirmWebUninstall
		m.message = ""
		return m, nil
	case "esc", "n", "N":
		m.mode = modeGrid
		m.message = ""
		return m, nil
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
		action := "install"
		switch m.mode {
		case modeConfirmWebUpgrade:
			action = "upgrade"
		case modeConfirmWebUninstall:
			action = "uninstall"
		}
		m.mode = modeGrid
		m.busy = true
		m.webProgress.Start(action)
		switch action {
		case "install":
			m.message = fmt.Sprintf("Installing node web panel on %s…", s.Name)
		case "upgrade":
			m.message = fmt.Sprintf("Upgrading node web panel on %s…", s.Name)
		default:
			m.message = fmt.Sprintf("Uninstalling web panel from %s…", s.Name)
		}
		return m, m.runWebPanel(s, action)
	case "n", "N", "esc":
		m.mode = modeGrid
		m.message = ""
		return m, nil
	}
	return m, nil
}

func (m *Model) runWebPanel(s storage.Server, action string) tea.Cmd {
	ch := make(chan tea.Msg, 16)
	m.webProgCh = ch
	go func() {
		defer close(ch)
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
			ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Err: fmt.Errorf("reconnect: %w", err)}
			return
		}
		exec, ok := m.ctx.Pool.GetExecutor(s.ID)
		if !ok {
			ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Err: fmt.Errorf("executor unavailable")}
			return
		}
		svc := webpanel.New(m.ctx.DB, s.ID)
		svc.SetHost(s.Host)
		svc.SetSSH(cfg)
		svc.SetPool(m.ctx.Pool)
		svc.SetProgress(func(pct float64, detail string) {
			ch <- shared.WebPanelProgressMsg{Pct: pct, Detail: detail}
		})
		switch action {
		case "install":
			if err := svc.Enable(exec, map[string]string{"port": "8080"}); err != nil {
				ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Err: err}
				return
			}
			ch <- shared.WebPanelDoneMsg{
				ServerID:  s.ID,
				Action:    action,
				Installed: true,
				URL:       fmt.Sprintf("http://%s:8080", s.Host),
			}
		case "upgrade":
			if err := svc.Upgrade(exec, "8080"); err != nil {
				ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Err: err}
				return
			}
			ch <- shared.WebPanelDoneMsg{
				ServerID:  s.ID,
				Action:    action,
				Installed: true,
				URL:       fmt.Sprintf("http://%s:8080", s.Host),
			}
		default:
			if err := svc.Disable(exec); err != nil {
				ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Err: err}
				return
			}
			ch <- shared.WebPanelDoneMsg{ServerID: s.ID, Action: action, Installed: false}
		}
	}()
	return shared.WaitMsg(ch)
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

func (m *Model) updateHostKeyConfirm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		s, ok := m.serverByID(m.hostKeyPending)
		m.mode = modeGrid
		m.hostKeyHint = ""
		if !ok {
			m.hostKeyPending = 0
			m.message = "Server not found"
			return m, nil
		}
		m.message = "Replacing host key and connecting…"
		m.busy = true
		return m, m.connect(s, true)
	case "n", "N", "esc":
		m.mode = modeGrid
		m.hostKeyPending = 0
		m.hostKeyHint = ""
		m.message = "Host key not replaced"
		return m, nil
	}
	return m, nil
}

func (m *Model) serverByID(id uint) (storage.Server, bool) {
	for _, s := range m.servers {
		if s.ID == id {
			return s, true
		}
	}
	return storage.Server{}, false
}

func (m *Model) connect(s storage.Server, replaceHostKey bool) tea.Cmd {
	return func() tea.Msg {
		_, err := m.ctx.Pool.Connect(s.ID, ssh.ClientConfig{
			Host:                  s.Host,
			Port:                  s.Port,
			User:                  s.User,
			KeyPath:               s.SSHKeyPath,
			Password:              s.Password,
			JumpHost:              s.JumpHost,
			ReplaceChangedHostKey: replaceHostKey,
		})
		return connectResultMsg{serverID: s.ID, ok: err == nil, err: err}
	}
}

func hostKeyConfirmHint(err error) string {
	if mismatch, ok := ssh.AsHostKeyMismatch(err); ok {
		if fp := mismatch.Fingerprint(); fp != "" {
			return "new fingerprint " + fp
		}
	}
	return ""
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
	case modeConfirmHostKey:
		name := m.selectedName()
		if s, ok := m.serverByID(m.hostKeyPending); ok {
			name = s.Name
		}
		lines := []string{
			m.viewGrid(),
			"",
			" " + theme.WarningText().Render(fmt.Sprintf(
				"Host key for %s changed (common after OS reinstall). Replace old key and connect? (y/n)", name)),
		}
		if m.hostKeyHint != "" {
			lines = append(lines, " "+theme.MutedText().Render(m.hostKeyHint))
		}
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	case modeConfirmWebInstall:
		name := m.selectedName()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render(fmt.Sprintf("Install node web panel on %s? (y/n)", name)),
			" "+theme.MutedText().Render("Metrics for this host only at :8080 (not a multi-server fleet)"),
		)
	case modeManageWebPanel:
		name := m.selectedName()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render(fmt.Sprintf("Node panel on %s already installed", name)),
			" "+theme.MutedText().Render("[r] reinstall/upgrade  [u] uninstall  [esc] cancel"),
		)
	case modeConfirmWebUpgrade:
		name := m.selectedName()
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewGrid(),
			"",
			" "+theme.WarningText().Render(fmt.Sprintf("Reinstall/upgrade node panel on %s? (y/n)", name)),
			" "+theme.MutedText().Render("Uploads new binary, sets web.role: node, restarts service"),
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
	parts := []string{header, "", body}
	if m.webProgress.Active {
		parts = append(parts, "", m.webProgress.View(m.width))
	} else if m.message != "" {
		parts = append(parts, "\n "+m.message)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
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
