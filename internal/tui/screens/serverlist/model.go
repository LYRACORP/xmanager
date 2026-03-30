package serverlist

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeList mode = iota
	modeAdd
	modeEdit
	modeConfirmDelete
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
	ctx     *shared.AppContext
	table   table.Model
	servers []storage.Server
	mode    mode
	form    [fieldCount]textinput.Model
	formIdx int
	width   int
	height  int
	message string
	editID  uint
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

func (m *Model) Name() string        { return "Server List" }
func (m *Model) SetSize(w, h int)    { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.loadServers
}

func (m *Model) loadServers() tea.Msg {
	var servers []storage.Server
	m.ctx.DB.Order("name asc").Find(&servers)
	return serversLoadedMsg{servers: servers}
}

func (m *Model) rebuildTable() {
	cols := []table.Column{
		{Title: "  ", Width: 3},
		{Title: "Name", Width: 18},
		{Title: "Host", Width: 20},
		{Title: "Port", Width: 6},
		{Title: "User", Width: 12},
		{Title: "Tags", Width: 15},
		{Title: "Last Seen", Width: 20},
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
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	m.table = components.StyledTable(cols, rows, h)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case serversLoadedMsg:
		m.servers = msg.servers
		m.rebuildTable()
		return m, nil
	case testResultMsg:
		if msg.ok {
			m.message = "Connection successful!"
			now := time.Now()
			m.ctx.DB.Model(&storage.Server{}).Where("id = ?", msg.serverID).Updates(map[string]interface{}{
				"is_active": true, "last_seen": &now,
			})
		} else {
			m.message = fmt.Sprintf("Connection failed: %v", msg.err)
		}
		return m, m.loadServers
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeAdd, modeEdit:
			return m.updateForm(msg)
		case modeConfirmDelete:
			return m.updateDelete(msg)
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
		m.mode = modeAdd
		m.initForm()
		m.formIdx = 0
		m.form[0].Focus()
		return m, nil
	case "e":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.mode = modeEdit
			m.editID = m.servers[idx].ID
			m.populateForm(m.servers[idx])
			m.formIdx = 0
			m.form[0].Focus()
		}
		return m, nil
	case "d", "delete":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.mode = modeConfirmDelete
			m.editID = m.servers[idx].ID
		}
		return m, nil
	case "t":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			m.message = "Testing connection..."
			return m, m.testConn(m.servers[idx])
		}
	case "enter":
		if idx := m.table.Cursor(); idx < len(m.servers) {
			s := m.servers[idx]
			return m, tea.Batch(
				m.testConn(s),
				func() tea.Msg {
					return shared.ConnectServerMsg{ServerID: s.ID}
				},
			)
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
		m.mode = modeList
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
		m.ctx.DB.Delete(&storage.Server{}, m.editID)
		m.ctx.Pool.Disconnect(m.editID)
		m.mode = modeList
		return m, m.loadServers
	default:
		m.mode = modeList
	}
	return m, nil
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
		if _, err := fmt.Sscanf(m.form[fieldPort].Value(), "%d", &port); err != nil {
			// fallback to 22 if invalid
		}
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
		m.mode = modeList
		var servers []storage.Server
		m.ctx.DB.Order("name asc").Find(&servers)
		return serversLoadedMsg{servers: servers}
	}
}

func (m *Model) testConn(s storage.Server) tea.Cmd {
	return func() tea.Msg {
		_, err := m.ctx.Pool.Connect(s.ID, ssh.ClientConfig{
			Host: s.Host, Port: s.Port, User: s.User,
			KeyPath: s.SSHKeyPath, Password: s.Password, JumpHost: s.JumpHost,
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
	title := theme.HeaderStyle().Render("Servers")
	help := components.NewHelpBar(
		components.KeyBinding{Key: "a", Desc: "add"},
		components.KeyBinding{Key: "e", Desc: "edit"},
		components.KeyBinding{Key: "d", Desc: "delete"},
		components.KeyBinding{Key: "t", Desc: "test"},
		components.KeyBinding{Key: "enter", Desc: "connect"},
	)
	help.Width = m.width
	msg := ""
	if m.message != "" {
		msg = "\n " + m.message
	}
	if m.mode == modeConfirmDelete {
		msg += "\n\n " + theme.WarningText().Render("Delete this server? (y/n)")
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), msg, help.View())
}

func (m *Model) viewForm() string {
	title := "Add Server"
	if m.mode == modeEdit {
		title = "Edit Server"
	}
	header := theme.HeaderStyle().Render(title)
	form := ""
	for i := range m.form {
		cursor := "  "
		if i == m.formIdx {
			cursor = theme.KeyStyle().Render("> ")
		}
		form += cursor + m.form[i].View() + "\n"
	}
	footer := theme.MutedText().Render("  Tab: next  Shift+Tab: prev  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", form, footer)
}
