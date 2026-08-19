package ftp

import (
	"strings"

	bspinner "github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xmftp "github.com/lyracorp/xmanager/internal/ftp"
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
	modePassword
	modeConfirmDelete
)

type loadedMsg struct {
	users   []storage.FTPUser
	enabled bool
	err     string
}

type actionDoneMsg struct {
	ok  string
	err string
}

type Model struct {
	ctx      *shared.AppContext
	users    []storage.FTPUser
	enabled  bool
	tbl      components.ListTable
	mode     mode
	userIn   textinput.Model
	passIn   textinput.Model
	homeIn   textinput.Model
	formIdx  int
	deleteID uint
	passID   uint
	message  string
	busy     bool
	spinner  components.LoadingSpinner
	loading  bool
	width    int
	height   int
}

func New(ctx *shared.AppContext) *Model {
	userIn := textinput.New()
	userIn.Placeholder = "alice"
	userIn.Prompt = "Username: "
	userIn.CharLimit = 32
	userIn.Width = 28

	passIn := textinput.New()
	passIn.Placeholder = "password"
	passIn.Prompt = "Password: "
	passIn.EchoMode = textinput.EchoPassword
	passIn.EchoCharacter = '•'
	passIn.Width = 28

	homeIn := textinput.New()
	homeIn.Placeholder = "/srv/ftp/alice"
	homeIn.Prompt = "Home: "
	homeIn.Width = 40

	return &Model{ctx: ctx, userIn: userIn, passIn: passIn, homeIn: homeIn}
}

func (m *Model) Name() string { return "FTP" }

func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) OnNavigate(_ map[string]interface{}) {
	m.mode = modeList
	m.message = ""
	m.busy = false
}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeAdd, modePassword:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "enter", Desc: "save"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeConfirmDelete:
		return []components.KeyBinding{
			{Key: "y", Desc: "confirm delete"},
			{Key: "n/esc", Desc: "cancel"},
		}
	default:
		return []components.KeyBinding{
			{Key: "e", Desc: "toggle vsftpd"},
			{Key: "a", Desc: "add user"},
			{Key: "p", Desc: "reset password"},
			{Key: "t", Desc: "toggle login"},
			{Key: "enter", Desc: "browse files"},
			{Key: "d", Desc: "delete"},
			{Key: "r", Desc: "refresh"},
			{Key: "b/esc", Desc: "back"},
		}
	}
}

func (m *Model) Init() tea.Cmd { return m.startLoad() }

func (m *Model) startLoad() tea.Cmd {
	m.loading = true
	m.spinner = components.NewLoadingSpinner("Loading…")
	return tea.Batch(m.spinner.Tick(), m.load())
}

func (m *Model) mgr() (*xmftp.Manager, bool) {
	if m.ctx.Pool == nil {
		return nil, false
	}
	ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
	if !ok {
		return nil, false
	}
	return xmftp.New(ex), true
}

func (m *Model) enabledNames() []string {
	var out []string
	for _, u := range m.users {
		if u.Enabled {
			out = append(out, u.Username)
		}
	}
	return out
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var users []storage.FTPUser
		if m.ctx.DB != nil {
			m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("username asc").Find(&users)
		}
		enabled := false
		errStr := ""
		mgr, ok := m.mgr()
		if !ok {
			errStr = "not connected"
		} else {
			enabled = mgr.IsEnabled()
		}
		return loadedMsg{users: users, enabled: enabled, err: errStr}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 2
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "On", Width: 3},
		{Title: "User", Width: 16},
		{Title: "Home", Width: 0},
	}
	rows := make([]table.Row, len(m.users))
	for i, u := range m.users {
		on := "✓"
		if !u.Enabled {
			on = "–"
		}
		rows[i] = table.Row{on, u.Username, u.Home}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(m.mode == modeList && !m.busy)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case bspinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loadedMsg:
		m.users = msg.users
		m.enabled = msg.enabled
		m.busy = false
		m.loading = false
		m.spinner = m.spinner.SetActive(false)
		if msg.err != "" && m.message == "" {
			m.message = msg.err
		}
		m.rebuildTable()
		return m, nil
	case actionDoneMsg:
		m.busy = false
		m.mode = modeList
		if msg.err != "" {
			m.message = msg.err
		} else {
			m.message = msg.ok
		}
		return m, m.startLoad()
	case tea.KeyMsg:
		if m.busy || m.loading {
			return m, nil
		}
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeAdd:
			return m.updateAdd(msg)
		case modePassword:
			return m.updatePassword(msg)
		case modeConfirmDelete:
			if msg.String() == "y" || msg.String() == "Y" {
				return m, m.deleteSelected()
			}
			m.mode = modeList
			m.rebuildTable()
			return m, nil
		}
	}
	if m.mode == modeList {
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "e":
		return m, m.toggleDaemon()
	case "a":
		m.mode = modeAdd
		m.userIn.SetValue("")
		m.passIn.SetValue("")
		m.homeIn.SetValue("")
		m.formIdx = 0
		m.userIn.Focus()
		m.passIn.Blur()
		m.homeIn.Blur()
		m.tbl = m.tbl.SetFocused(false)
		return m, nil
	case "p":
		if idx := m.tbl.Cursor(); idx < len(m.users) {
			m.passID = m.users[idx].ID
			m.mode = modePassword
			m.passIn.SetValue("")
			m.passIn.Focus()
			m.tbl = m.tbl.SetFocused(false)
		}
		return m, nil
	case "t":
		if idx := m.tbl.Cursor(); idx < len(m.users) {
			return m, m.toggleUser(m.users[idx])
		}
	case "enter":
		if idx := m.tbl.Cursor(); idx < len(m.users) {
			home := m.users[idx].Home
			return m, func() tea.Msg {
				return shared.NavigateMsg{
					Screen:   shared.ScreenDashboard,
					ServerID: m.ctx.ServerID,
					Params:   map[string]interface{}{"tab": "files", "path": home},
				}
			}
		}
	case "d", "delete":
		if idx := m.tbl.Cursor(); idx < len(m.users) {
			m.deleteID = m.users[idx].ID
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "r":
		return m, m.startLoad()
	case "b", "esc":
		return m, func() tea.Msg { return shared.GoBackMsg{} }
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *Model) updateAdd(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.userIn, &m.passIn, &m.homeIn}
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.tbl = m.tbl.SetFocused(true)
		return m, nil
	case "tab", "down":
		inputs[m.formIdx].Blur()
		m.formIdx = (m.formIdx + 1) % len(inputs)
		inputs[m.formIdx].Focus()
		return m, nil
	case "shift+tab", "up":
		inputs[m.formIdx].Blur()
		m.formIdx = (m.formIdx - 1 + len(inputs)) % len(inputs)
		inputs[m.formIdx].Focus()
		return m, nil
	case "enter":
		if m.formIdx < len(inputs)-1 {
			inputs[m.formIdx].Blur()
			m.formIdx++
			inputs[m.formIdx].Focus()
			return m, nil
		}
		return m, m.saveUser()
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) updatePassword(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.tbl = m.tbl.SetFocused(true)
		return m, nil
	case "enter":
		return m, m.savePassword()
	}
	var cmd tea.Cmd
	m.passIn, cmd = m.passIn.Update(msg)
	return m, cmd
}

func (m *Model) toggleDaemon() tea.Cmd {
	m.busy = true
	enable := !m.enabled
	names := m.enabledNames()
	return func() tea.Msg {
		mgr, ok := m.mgr()
		if !ok {
			return actionDoneMsg{err: "not connected"}
		}
		if enable {
			if err := mgr.Enable(names); err != nil {
				return actionDoneMsg{err: err.Error()}
			}
			return actionDoneMsg{ok: "vsftpd enabled"}
		}
		if err := mgr.Disable(); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{ok: "vsftpd disabled"}
	}
}

func (m *Model) saveUser() tea.Cmd {
	username := strings.ToLower(strings.TrimSpace(m.userIn.Value()))
	password := m.passIn.Value()
	home := strings.TrimSpace(m.homeIn.Value())
	m.busy = true
	return func() tea.Msg {
		mgr, ok := m.mgr()
		if !ok {
			return actionDoneMsg{err: "not connected"}
		}
		gotHome, err := mgr.CreateUser(username, password, home)
		if err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		row := storage.FTPUser{
			ServerID: m.ctx.ServerID,
			Username: username,
			Home:     gotHome,
			Enabled:  true,
		}
		if err := m.ctx.DB.Create(&row).Error; err != nil {
			return actionDoneMsg{err: "host user created, DB failed: " + err.Error()}
		}
		return actionDoneMsg{ok: "created " + username}
	}
}

func (m *Model) savePassword() tea.Cmd {
	pass := m.passIn.Value()
	id := m.passID
	m.busy = true
	return func() tea.Msg {
		var u storage.FTPUser
		if err := m.ctx.DB.Where("id = ? AND server_id = ?", id, m.ctx.ServerID).First(&u).Error; err != nil {
			return actionDoneMsg{err: "user not found"}
		}
		mgr, ok := m.mgr()
		if !ok {
			return actionDoneMsg{err: "not connected"}
		}
		if err := mgr.SetPassword(u.Username, pass); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{ok: "password updated for " + u.Username}
	}
}

func (m *Model) toggleUser(u storage.FTPUser) tea.Cmd {
	m.busy = true
	next := !u.Enabled
	return func() tea.Msg {
		mgr, ok := m.mgr()
		if !ok {
			return actionDoneMsg{err: "not connected"}
		}
		if err := mgr.SetEnabled(u.Username, next); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		m.ctx.DB.Model(&u).Update("enabled", next)
		state := "disabled"
		if next {
			state = "enabled"
		}
		return actionDoneMsg{ok: u.Username + " " + state}
	}
}

func (m *Model) deleteSelected() tea.Cmd {
	id := m.deleteID
	m.busy = true
	m.mode = modeList
	return func() tea.Msg {
		var u storage.FTPUser
		if err := m.ctx.DB.Where("id = ? AND server_id = ?", id, m.ctx.ServerID).First(&u).Error; err != nil {
			return actionDoneMsg{err: "user not found"}
		}
		mgr, ok := m.mgr()
		if !ok {
			return actionDoneMsg{err: "not connected"}
		}
		if err := mgr.DeleteUser(u.Username, false); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		m.ctx.DB.Delete(&u)
		return actionDoneMsg{ok: "deleted " + u.Username}
	}
}

func (m *Model) View() string {
	if m.mode == modeAdd {
		return m.viewForm("Add FTP user", []*textinput.Model{&m.userIn, &m.passIn, &m.homeIn})
	}
	if m.mode == modePassword {
		return m.viewForm("Reset password", []*textinput.Model{&m.passIn})
	}
	status := "vsftpd stopped"
	if m.enabled {
		status = "vsftpd active · :21"
	}
	if m.busy {
		status += " · working…"
	}
	body := m.tbl.View()
	if m.loading {
		body = m.spinner.View()
	}
	frame := components.ScreenFrame{
		Title:       "FTP",
		Subtitle:    status,
		Width:       m.width,
		Body:        body,
		LocalChrome: components.FrameChromeRows(true) + 2,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	if m.mode == modeConfirmDelete {
		parts = append(parts, "", " "+theme.WarningText().Render("Delete this FTP user? Home is kept. (y/n)"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm(title string, inputs []*textinput.Model) string {
	header := theme.ScreenChrome(title, "jailed vsftpd account", m.width)
	var b strings.Builder
	for i, ti := range inputs {
		focused := false
		if m.mode == modePassword {
			focused = true
		} else {
			focused = i == m.formIdx
		}
		styled := components.ApplyInputTheme(*ti, m.width, focused)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, focused))
		b.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), footer)
}
