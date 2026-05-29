package database

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type engine int

const (
	engPostgres engine = iota
	engMySQL
	engMongo
)

type viewTab int

const (
	tabDatabases viewTab = iota
	tabUsers
)

type dbMode int

const (
	dbBrowse dbMode = iota
	dbInputName
	dbConfirmDrop
	dbOutput
)

type enginesDetectedMsg struct {
	available map[dbmanager.DBType]bool
	err       string
}

type dbLoadedMsg struct {
	eng   engine
	dbs   []dbmanager.Database
	err   string
	brief string
}

type usersLoadedMsg struct {
	eng   engine
	users []dbmanager.DBUser
	err   string
	brief string
}

type actionDoneMsg struct {
	out string
	err string
}

type Model struct {
	ctx       *shared.AppContext
	eng       engine
	view      viewTab
	mode      dbMode
	pending   string
	width     int
	height    int
	nameInput textinput.Model
	lastOut   string

	available map[dbmanager.DBType]bool
	databases []dbmanager.Database
	users     []dbmanager.DBUser
	table     table.Model

	busy        bool
	status      string
	err         string
	loadAttempt bool
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "database_name"
	ti.Prompt = "Name: "
	ti.Width = 40
	return &Model{ctx: ctx, nameInput: ti, view: tabDatabases}
}

func (m *Model) Name() string { return "Database" }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.rebuildTable()
}

func (m *Model) Init() tea.Cmd {
	m.mode = dbBrowse
	m.pending = ""
	m.nameInput.SetValue("")
	m.available = nil
	m.databases = nil
	m.users = nil
	m.busy = false
	m.status = ""
	m.err = ""
	m.loadAttempt = false
	return tea.Batch(m.detectEnginesCmd(), m.reloadCmd())
}

func (m *Model) engLabel() string {
	switch m.eng {
	case engMySQL:
		return "MySQL"
	case engMongo:
		return "MongoDB"
	default:
		return "PostgreSQL"
	}
}

func (m *Model) engDBType() dbmanager.DBType {
	switch m.eng {
	case engMySQL:
		return dbmanager.MySQL
	case engMongo:
		return dbmanager.MongoDB
	default:
		return dbmanager.PostgreSQL
	}
}

func (m *Model) engineInstalled() bool {
	if m.available == nil {
		return false
	}
	return m.available[m.engDBType()]
}

func (m *Model) managerOpts() dbmanager.ManagerOptions {
	return dbmanager.ManagerOptions{PostgresPassword: m.postgresPasswordFromServer()}
}

func (m *Model) postgresPasswordFromServer() string {
	if m.ctx.DB == nil || m.ctx.ServerID == 0 {
		return ""
	}
	var s storage.Server
	if err := m.ctx.DB.First(&s, m.ctx.ServerID).Error; err != nil {
		return ""
	}
	return tagValue(s.Tags, "postgres_password")
}

func tagValue(tags, key string) string {
	for _, part := range strings.Split(tags, ",") {
		part = strings.TrimSpace(part)
		if k, v, ok := strings.Cut(part, "="); ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (m *Model) managerFor(eng engine, ex *ssh.Executor) dbmanager.Manager {
	return dbmanager.NewManagerWithOptions(m.engDBTypeFor(eng), ex, m.managerOpts())
}

func (m *Model) engDBTypeFor(eng engine) dbmanager.DBType {
	switch eng {
	case engMySQL:
		return dbmanager.MySQL
	case engMongo:
		return dbmanager.MongoDB
	default:
		return dbmanager.PostgreSQL
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case enginesDetectedMsg:
		if msg.err != "" {
			m.err = msg.err
			return m, nil
		}
		m.available = msg.available
		return m, nil

	case dbLoadedMsg:
		m.busy = false
		if msg.eng != m.eng {
			return m, nil
		}
		m.loadAttempt = true
		m.databases = msg.dbs
		m.err = msg.err
		m.status = msg.brief
		if msg.err != "" {
			m.status = ""
		}
		m.rebuildTable()
		return m, nil

	case usersLoadedMsg:
		m.busy = false
		if msg.eng != m.eng {
			return m, nil
		}
		m.loadAttempt = true
		m.users = msg.users
		m.err = msg.err
		m.status = msg.brief
		if msg.err != "" {
			m.status = ""
		}
		m.rebuildTable()
		return m, nil

	case actionDoneMsg:
		m.busy = false
		m.lastOut = msg.out
		if msg.err != "" {
			if m.lastOut != "" {
				m.lastOut += "\n"
			}
			m.lastOut += msg.err
		}
		m.mode = dbOutput
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.mode == dbInputName {
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd
	}
	if m.mode == dbBrowse && !m.busy {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch m.mode {
	case dbOutput:
		switch msg.String() {
		case "esc", "enter", " ":
			m.mode = dbBrowse
			return m, m.reloadCmd()
		}
		return m, nil

	case dbConfirmDrop:
		switch msg.String() {
		case "y", "Y":
			name := m.nameInput.Value()
			m.mode = dbBrowse
			if err := validateDBName(name); err != nil {
				return m, m.failMsg(err.Error())
			}
			return m, m.runDrop(name)
		case "n", "N", "esc":
			m.mode = dbBrowse
			return m, nil
		}
		return m, nil

	case dbInputName:
		switch msg.String() {
		case "esc":
			m.mode = dbBrowse
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.nameInput.Value())
			if err := validateDBName(name); err != nil {
				return m, m.failMsg(err.Error())
			}
			switch m.pending {
			case "create":
				m.mode = dbBrowse
				return m, m.runCreate(name)
			case "drop":
				m.mode = dbConfirmDrop
				return m, nil
			case "backup":
				m.mode = dbBrowse
				return m, m.runBackup(name)
			}
		}
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd

	default:
		if m.busy {
			return m, nil
		}
		if m.ctx.ServerID == 0 {
			switch msg.String() {
			case "esc", "q":
				return m, func() tea.Msg { return shared.GoBackMsg{} }
			}
			return m, nil
		}
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "tab", "right":
			m.eng = (m.eng + 1) % 3
			return m, m.onEngineChange()
		case "shift+tab", "left":
			m.eng = (m.eng - 1 + 3) % 3
			return m, m.onEngineChange()
		case "1":
			m.eng = engPostgres
			return m, m.onEngineChange()
		case "2":
			m.eng = engMySQL
			return m, m.onEngineChange()
		case "3":
			m.eng = engMongo
			return m, m.onEngineChange()
		case "4":
			return m, m.setView(tabDatabases)
		case "5", "u":
			return m, m.setView(tabUsers)
		case "l", "ctrl+r", "f5":
			return m, m.reloadCmd()
		case "c":
			if !m.engineInstalled() {
				return m, nil
			}
			m.pending = "create"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		case "d":
			if !m.engineInstalled() {
				return m, nil
			}
			m.pending = "drop"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		case "b":
			if !m.engineInstalled() {
				return m, nil
			}
			m.pending = "backup"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}
}

func (m *Model) onEngineChange() tea.Cmd {
	m.err = ""
	m.status = ""
	m.loadAttempt = false
	return m.reloadCmd()
}

func (m *Model) setView(v viewTab) tea.Cmd {
	if m.view == v {
		return m.reloadCmd()
	}
	m.view = v
	m.err = ""
	m.loadAttempt = false
	return m.reloadCmd()
}

func (m *Model) failMsg(s string) tea.Cmd {
	return func() tea.Msg {
		return actionDoneMsg{err: s}
	}
}

func validateDBName(name string) error {
	if name == "" {
		return fmt.Errorf("name required")
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			continue
		}
		return fmt.Errorf("only letters, digits, underscore")
	}
	return nil
}

func (m *Model) detectEnginesCmd() tea.Cmd {
	sid := m.ctx.ServerID
	return func() tea.Msg {
		if sid == 0 {
			return enginesDetectedMsg{}
		}
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return enginesDetectedMsg{err: "No SSH session. Connect to a server first."}
		}
		return enginesDetectedMsg{available: dbmanager.DetectAvailable(ex)}
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	if m.ctx.ServerID == 0 {
		return nil
	}
	m.busy = true
	m.status = "Loading…"
	if m.view == tabUsers {
		return m.loadUsersCmd()
	}
	return m.loadDatabasesCmd()
}

func (m *Model) loadDatabasesCmd() tea.Cmd {
	eng := m.eng
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return dbLoadedMsg{eng: eng, err: "No SSH session. Connect to a server first."}
		}
		mgr := m.managerFor(eng, ex)
		if !mgr.IsAvailable() {
			return dbLoadedMsg{
				eng: eng,
				err: fmt.Sprintf("%s client not found on server", engineLabel(eng)),
			}
		}
		dbs, err := mgr.ListDatabases()
		if err != nil {
			return dbLoadedMsg{eng: eng, err: err.Error()}
		}
		return dbLoadedMsg{
			eng:   eng,
			dbs:   dbs,
			brief: fmt.Sprintf("%d databases", len(dbs)),
		}
	}
}

func (m *Model) loadUsersCmd() tea.Cmd {
	eng := m.eng
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return usersLoadedMsg{eng: eng, err: "No SSH session. Connect to a server first."}
		}
		mgr := m.managerFor(eng, ex)
		if !mgr.IsAvailable() {
			return usersLoadedMsg{
				eng: eng,
				err: fmt.Sprintf("%s client not found on server", engineLabel(eng)),
			}
		}
		users, err := mgr.ListUsers()
		if err != nil {
			return usersLoadedMsg{eng: eng, err: err.Error()}
		}
		return usersLoadedMsg{
			eng:   eng,
			users: users,
			brief: fmt.Sprintf("%d users", len(users)),
		}
	}
}

func engineLabel(eng engine) string {
	switch eng {
	case engMySQL:
		return "MySQL"
	case engMongo:
		return "MongoDB"
	default:
		return "PostgreSQL"
	}
}

func (m *Model) runCreate(name string) tea.Cmd {
	eng := m.eng
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "No SSH session. Connect to a server first."}
		}
		mgr := m.managerFor(eng, ex)
		if err := mgr.CreateDatabase(name); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{out: fmt.Sprintf("Created database %q on %s.", name, engineLabel(eng))}
	}
}

func (m *Model) runDrop(name string) tea.Cmd {
	eng := m.eng
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "No SSH session. Connect to a server first."}
		}
		mgr := m.managerFor(eng, ex)
		if err := mgr.DropDatabase(name); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{out: fmt.Sprintf("Dropped database %q on %s.", name, engineLabel(eng))}
	}
}

func (m *Model) runBackup(name string) tea.Cmd {
	eng := m.eng
	sid := m.ctx.ServerID
	path := fmt.Sprintf("/tmp/xmanager_%s_backup", name)
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "No SSH session. Connect to a server first."}
		}
		mgr := m.managerFor(eng, ex)
		if err := mgr.Backup(name, path); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{out: fmt.Sprintf("Backup of %q written under %s (engine-specific extension).", name, path)}
	}
}

func (m *Model) rebuildTable() {
	h := layout.TableHeight(m.height, 14, 4)

	switch m.view {
	case tabUsers:
		cols := layout.AdaptiveColumns(m.width, []table.Column{
			{Title: "User", Width: 24},
			{Title: "Roles / Host", Width: 0},
		})
		rows := make([]table.Row, len(m.users))
		for i, u := range m.users {
			rows[i] = table.Row{u.Name, u.Roles}
		}
		m.table = components.StyledTable(cols, rows, h)
	default:
		cols := layout.AdaptiveColumns(m.width, []table.Column{
			{Title: "Name", Width: 22},
			{Title: "Owner", Width: 16},
			{Title: "Size", Width: 0},
		})
		rows := make([]table.Row, len(m.databases))
		for i, d := range m.databases {
			rows[i] = table.Row{d.Name, d.Owner, d.Size}
		}
		m.table = components.StyledTable(cols, rows, h)
	}
}

func (m *Model) View() string {
	title := theme.ScreenChrome("Database Manager", m.engLabel()+" · "+m.viewLabel(), m.width)
	if m.ctx.ServerID == 0 {
		help := components.NewHelpBar(components.KeyBinding{Key: "esc", Desc: "back"})
		help.Width = m.width
		return lipgloss.JoinVertical(lipgloss.Left, title, "", theme.WarningText().Render("  Connect to a server first."), "", help.View())
	}

	engTabs := m.renderEngineTabs()
	viewTabs := m.renderViewTabs()
	enginesBar := m.renderEnginesBar()

	switch m.mode {
	case dbOutput:
		panel := theme.ViewportStyle().Width(layout.PanelWidth(m.width)).MaxHeight(m.height - 8).Render(m.lastOut)
		foot := theme.MutedText().Render("  Esc: return")
		return lipgloss.JoinVertical(lipgloss.Left, title, engTabs, viewTabs, enginesBar, "", panel, "", foot)

	case dbConfirmDrop:
		q := theme.WarningText().Render(fmt.Sprintf("  Drop database %q on %s? (y/n)", m.nameInput.Value(), m.engLabel()))
		help := theme.MutedText().Render("  This is destructive.")
		return lipgloss.JoinVertical(lipgloss.Left, title, engTabs, viewTabs, enginesBar, "", q, "", help)

	case dbInputName:
		input := components.RenderInputPanel(
			components.ApplyInputTheme(m.nameInput, m.width, true).View(),
			m.width, true,
		)
		hint := theme.MutedText().Render(fmt.Sprintf("  %s — enter name, Esc cancel", m.pending))
		return lipgloss.JoinVertical(lipgloss.Left, title, engTabs, viewTabs, enginesBar, "", input, "", hint)

	default:
		body := theme.PanelStyle().Width(layout.PanelWidth(m.width)).Render(m.table.View())
		if !m.engineInstalled() && m.available != nil {
			body = theme.WarningText().Render(fmt.Sprintf("  %s is not installed on this server.", m.engLabel()))
		} else if m.loadAttempt && !m.busy && m.err == "" && m.rowCount() == 0 {
			body = theme.PanelStyle().Width(layout.PanelWidth(m.width)).Render(
				"  " + components.TableEmptyMessage(),
			)
		} else if !m.loadAttempt && !m.busy {
			body = theme.PanelStyle().Width(layout.PanelWidth(m.width)).Render(
				"  " + theme.MutedText().Render("Loading…"),
			)
		}

		msg := ""
		if m.err != "" {
			msg = "\n " + theme.ErrorText().Render(m.err)
		} else if m.status != "" {
			msg = "\n " + theme.MutedText().Render(m.status)
		}
		if m.busy {
			msg = "\n " + theme.MutedText().Render("Loading…")
		}

		help := components.NewHelpBar(m.helpBindings()...)
		help.Width = m.width

		return lipgloss.JoinVertical(lipgloss.Left, title, engTabs, viewTabs, enginesBar, body, msg, help.View())
	}
}

func (m *Model) rowCount() int {
	if m.view == tabUsers {
		return len(m.users)
	}
	return len(m.databases)
}

func (m *Model) viewLabel() string {
	if m.view == tabUsers {
		return "Users"
	}
	return "Databases"
}

func (m *Model) helpBindings() []components.KeyBinding {
	base := []components.KeyBinding{
		{Key: "1-3", Desc: "engine"},
		{Key: "tab", Desc: "next engine"},
		{Key: "4/5", Desc: "databases/users"},
		{Key: "l", Desc: "refresh"},
		{Key: "esc", Desc: "back"},
	}
	if !m.engineInstalled() {
		return base
	}
	return append([]components.KeyBinding{
		{Key: "c", Desc: "create"},
		{Key: "d", Desc: "drop"},
		{Key: "b", Desc: "backup"},
	}, base...)
}

func (m *Model) renderEngineTabs() string {
	names := []string{"PostgreSQL", "MySQL", "MongoDB"}
	var parts []string
	for i, n := range names {
		st := theme.MutedText()
		if engine(i) == m.eng {
			st = theme.HeaderStyle()
		}
		label := fmt.Sprintf("%d:%s", i+1, n)
		if m.available != nil {
			if m.available[engineDBType(engine(i))] {
				label += " ✓"
			} else {
				label += " ✗"
			}
		}
		parts = append(parts, st.Render(" "+label+" "))
	}
	return lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(parts, ""))
}

func engineDBType(eng engine) dbmanager.DBType {
	switch eng {
	case engMySQL:
		return dbmanager.MySQL
	case engMongo:
		return dbmanager.MongoDB
	default:
		return dbmanager.PostgreSQL
	}
}

func (m *Model) renderViewTabs() string {
	names := []string{"Databases", "Users"}
	var parts []string
	for i, n := range names {
		st := theme.SubtitleStyle()
		if viewTab(i) == m.view {
			st = theme.TitleStyle().Underline(true)
		}
		parts = append(parts, st.Render(fmt.Sprintf("%d:%s", i+4, n)))
	}
	return lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(parts, "  "))
}

func (m *Model) renderEnginesBar() string {
	if m.available == nil {
		return ""
	}
	var bits []string
	order := []struct {
		t dbmanager.DBType
		l string
	}{
		{dbmanager.PostgreSQL, "PostgreSQL"},
		{dbmanager.MySQL, "MySQL"},
		{dbmanager.MongoDB, "MongoDB"},
	}
	for _, e := range order {
		if m.available[e.t] {
			bits = append(bits, theme.SuccessText().Render(e.l+" installed"))
		} else {
			bits = append(bits, theme.MutedText().Render(e.l+" not found"))
		}
	}
	return lipgloss.NewStyle().PaddingLeft(1).Render("Engines on server: " + strings.Join(bits, "  ·  "))
}

var _ shared.Screen = (*Model)(nil)
