package serverlist

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeList mode = iota
	modeAdd
	modeEdit
	modeConfirmDelete
	modeConfirmHostKey
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
	fieldJumpHost
	fieldCount
)

type serversLoadedMsg struct{ servers []storage.Server }
type testResultMsg struct {
	serverID uint
	ok       bool
	err      error
}

type Model struct {
	ctx            *shared.AppContext
	table          components.ListTable
	servers        []storage.Server
	mode           mode
	form           [fieldCount]textinput.Model
	formIdx        int
	width          int
	height         int
	message        string
	editID         uint
	connectPending uint // navigate to dashboard after successful connect
	hostKeyPending uint
	hostKeyHint    string
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{ctx: ctx}
	m.initForm()
	return m
}

func (m *Model) initForm() {
	labels := [fieldCount]string{"Name", "Host", "Port", "User", "SSH Key Path", "Password", "Tags", "Jump Host"}
	hints := [fieldCount]string{"my-server", "192.168.1.1", "22", "root", "~/.ssh/id_rsa", "", "web,prod", "bastion:22"}
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

func (m *Model) Name() string     { return "Server List" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeList:
		return []components.KeyBinding{
			{Key: "a", Desc: "add"},
			{Key: "e", Desc: "edit"},
			{Key: "d", Desc: "delete"},
			{Key: "t", Desc: "test"},
			{Key: "enter", Desc: "connect"},
			{Key: "q", Desc: "quit"},
		}
	case modeAdd, modeEdit:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "shift+tab", Desc: "prev field"},
			{Key: "enter", Desc: "save"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeConfirmDelete:
		return []components.KeyBinding{
			{Key: "y", Desc: "confirm delete"},
			{Key: "n", Desc: "cancel"},
		}
	case modeConfirmHostKey:
		return []components.KeyBinding{
			{Key: "y", Desc: "replace key"},
			{Key: "n", Desc: "cancel"},
		}
	default:
		return nil
	}
}

func (m *Model) Init() tea.Cmd {
	return m.loadServers
}

func (m *Model) loadServers() tea.Msg {
	var servers []storage.Server
	m.ctx.DB.Order("name asc").Find(&servers)
	return serversLoadedMsg{servers: servers}
}

func (m *Model) listLocalChrome() int {
	return components.FrameChromeRows(true) + 1 // +1 status/message line
}

func (m *Model) rebuildTable() {
	localChrome := m.listLocalChrome()
	h := layout.BodyHeight(m.height, localChrome, 5)
	cols := []table.Column{
		{Title: "  ", Width: 3},
		{Title: "Name", Width: 18},
		{Title: "Host", Width: 20},
		{Title: "Port", Width: 6},
		{Title: "User", Width: 12},
		{Title: "Tags", Width: 15},
		{Title: "Last Seen", Width: 0},
	}
	rows := make([]table.Row, len(m.servers))
	for i, s := range m.servers {
		status := theme.StatusDot(s.IsActive)
		lastSeen := "Never"
		if s.LastSeen != nil {
			lastSeen = s.LastSeen.Format("2006-01-02 15:04")
		}
		rows[i] = table.Row{status, s.Name, s.Host, fmt.Sprintf("%d", s.Port), s.User, s.Tags, lastSeen}
	}
	m.table = m.table.SetData(m.width, cols, rows, h)
	m.table = m.table.SetFocused(m.mode == modeList)
}

func (m *Model) setMode(next mode) {
	m.mode = next
	m.table = m.table.SetFocused(next == modeList)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case serversLoadedMsg:
		m.servers = msg.servers
		m.rebuildTable()
		return m, nil
	case testResultMsg:
		var cmds []tea.Cmd
		if msg.ok {
			if m.connectPending == msg.serverID {
				m.message = "Connected — opening dashboard..."
				m.connectPending = 0
				cmds = append(cmds, func() tea.Msg {
					return shared.ConnectServerMsg{ServerID: msg.serverID}
				})
			} else {
				m.message = "Connection successful!"
			}
			m.hostKeyPending = 0
			m.hostKeyHint = ""
			sid := msg.serverID
			cmds = append(cmds, func() tea.Msg {
				now := time.Now()
				m.ctx.DB.Model(&storage.Server{}).Where("id = ?", sid).Updates(map[string]interface{}{
					"is_active": true, "last_seen": &now,
				})
				return nil
			})
		} else {
			if ssh.IsHostKeyMismatch(msg.err) {
				m.hostKeyPending = msg.serverID
				m.hostKeyHint = ""
				if mismatch, ok := ssh.AsHostKeyMismatch(msg.err); ok {
					if fp := mismatch.Fingerprint(); fp != "" {
						m.hostKeyHint = "new fingerprint " + fp
					}
				}
				// Keep connectPending so a successful replace still opens the dashboard.
				if m.connectPending != msg.serverID {
					m.connectPending = 0
				}
				m.setMode(modeConfirmHostKey)
				m.message = ""
				return m, nil
			}
			if m.connectPending == msg.serverID {
				m.connectPending = 0
			}
			m.message = fmt.Sprintf("Connection failed: %v", msg.err)
		}
		cmds = append(cmds, m.loadServers)
		return m, tea.Batch(cmds...)
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeAdd, modeEdit:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateDelete(msg)
		case modeConfirmHostKey:
			return m.updateHostKeyConfirm(msg)
		}
	}
	if m.mode == modeList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "a":
		m.setMode(modeAdd)
		m.initForm()
		m.formIdx = 0
		m.form[0].Focus()
		return m, nil
	case "e":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.setMode(modeEdit)
			m.editID = m.servers[idx].ID
			m.populateForm(m.servers[idx])
			m.formIdx = 0
			m.form[0].Focus()
		}
		return m, nil
	case "d", "delete":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.setMode(modeConfirmDelete)
			m.editID = m.servers[idx].ID
		}
		return m, nil
	case "t":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.message = "Testing connection..."
			return m, m.testConn(m.servers[idx], false)
		}
	case "enter":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			s := m.servers[idx]
			m.connectPending = s.ID
			m.message = "Connecting..."
			return m, m.testConn(s, false)
		}
	case "q", "esc":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) updateForm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.setMode(modeList)
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
		id := m.editID
		m.setMode(modeList)
		m.message = "Deleting…"
		return m, func() tea.Msg {
			m.ctx.DB.Delete(&storage.Server{}, id)
			m.ctx.Pool.Disconnect(id)
			var servers []storage.Server
			m.ctx.DB.Order("name asc").Find(&servers)
			return serversLoadedMsg{servers: servers}
		}
	default:
		m.setMode(modeList)
	}
	return m, nil
}

func (m *Model) updateHostKeyConfirm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		s, ok := m.serverByID(m.hostKeyPending)
		m.setMode(modeList)
		m.hostKeyHint = ""
		if !ok {
			m.hostKeyPending = 0
			m.connectPending = 0
			m.message = "Server not found"
			return m, nil
		}
		m.message = "Replacing host key and connecting..."
		return m, m.testConn(s, true)
	case "n", "N", "esc":
		m.setMode(modeList)
		m.hostKeyPending = 0
		m.hostKeyHint = ""
		m.connectPending = 0
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

func (m *Model) populateForm(s storage.Server) {
	m.form[fieldName].SetValue(s.Name)
	m.form[fieldHost].SetValue(s.Host)
	m.form[fieldPort].SetValue(fmt.Sprintf("%d", s.Port))
	m.form[fieldUser].SetValue(s.User)
	m.form[fieldKeyPath].SetValue(s.SSHKeyPath)
	m.form[fieldPassword].SetValue(s.Password)
	m.form[fieldTags].SetValue(s.Tags)
	m.form[fieldJumpHost].SetValue(s.JumpHost)
}

func (m *Model) saveServer() tea.Cmd {
	return func() tea.Msg {
		port := 22
		_, _ = fmt.Sscanf(m.form[fieldPort].Value(), "%d", &port)
		srv := storage.Server{
			Name: m.form[fieldName].Value(), Host: m.form[fieldHost].Value(),
			Port: port, User: m.form[fieldUser].Value(),
			SSHKeyPath: m.form[fieldKeyPath].Value(), Password: m.form[fieldPassword].Value(),
			Tags: m.form[fieldTags].Value(), JumpHost: m.form[fieldJumpHost].Value(),
		}
		if m.mode == modeEdit {
			m.ctx.DB.Model(&storage.Server{}).Where("id = ?", m.editID).Updates(srv)
		} else {
			m.ctx.DB.Create(&srv)
		}
		m.setMode(modeList)
		var servers []storage.Server
		m.ctx.DB.Order("name asc").Find(&servers)
		return serversLoadedMsg{servers: servers}
	}
}

func (m *Model) testConn(s storage.Server, replaceHostKey bool) tea.Cmd {
	return func() tea.Msg {
		_, err := m.ctx.Pool.Connect(s.ID, ssh.ClientConfig{
			Host: s.Host, Port: s.Port, User: s.User,
			KeyPath: s.SSHKeyPath, Password: s.Password, JumpHost: s.JumpHost,
			ReplaceChangedHostKey: replaceHostKey,
		})
		return testResultMsg{serverID: s.ID, ok: err == nil, err: err}
	}
}

func (m *Model) View() string {
	switch m.mode {
	case modeAdd, modeEdit:
		return m.viewForm()
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	frame := components.ScreenFrame{
		Title:       "Servers",
		Subtitle:    "manage SSH connections",
		Width:       m.width,
		Body:        m.table.View(),
		LocalChrome: m.listLocalChrome(),
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	if m.mode == modeConfirmDelete {
		parts = append(parts, "", " "+theme.WarningText().Render("Delete this server? (y/n)"))
	}
	if m.mode == modeConfirmHostKey {
		name := "server"
		if s, ok := m.serverByID(m.hostKeyPending); ok {
			name = s.Name
		}
		parts = append(parts, "",
			" "+theme.WarningText().Render(fmt.Sprintf(
				"Host key for %s changed (common after OS reinstall). Replace old key and connect? (y/n)", name)))
		if m.hostKeyHint != "" {
			parts = append(parts, " "+theme.MutedText().Render(m.hostKeyHint))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm() string {
	title := "Add Server"
	subtitle := "new SSH target"
	if m.mode == modeEdit {
		title = "Edit Server"
		subtitle = "update connection details"
	}
	header := theme.ScreenChrome(title, subtitle, m.width)
	var form strings.Builder
	for i := range m.form {
		ti := components.ApplyInputTheme(m.form[i], m.width, i == m.formIdx)
		form.WriteString(components.RenderFormField("", ti.View(), m.width, i == m.formIdx))
		form.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Shift+Tab: prev  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", form.String(), footer)
}
