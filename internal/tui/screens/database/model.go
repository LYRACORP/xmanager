package database

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type engine int

const (
	engPostgres engine = iota
	engMySQL
	engMongo
)

type dbMode int

const (
	dbBrowse dbMode = iota
	dbInputName
	dbConfirmDrop
	dbOutput
)

type execMsg struct {
	out string
	err string
}

type Model struct {
	ctx       *shared.AppContext
	eng       engine
	mode      dbMode
	pending   string
	width     int
	height    int
	nameInput textinput.Model
	lastOut   string
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "database_name"
	ti.Prompt = "Name: "
	ti.Width = 40
	return &Model{ctx: ctx, nameInput: ti}
}

func (m *Model) Name() string     { return "Database" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m *Model) Init() tea.Cmd {
	m.mode = dbBrowse
	m.pending = ""
	m.nameInput.SetValue("")
	return nil
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

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case execMsg:
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
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch m.mode {
	case dbOutput:
		switch msg.String() {
		case "esc", "enter", " ":
			m.mode = dbBrowse
			return m, nil
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
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "tab", "right":
			m.eng = (m.eng + 1) % 3
			return m, nil
		case "shift+tab", "left":
			m.eng = (m.eng - 1 + 3) % 3
			return m, nil
		case "1":
			m.eng = engPostgres
			return m, nil
		case "2":
			m.eng = engMySQL
			return m, nil
		case "3":
			m.eng = engMongo
			return m, nil
		case "l":
			return m, m.runList()
		case "c":
			m.pending = "create"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		case "d":
			m.pending = "drop"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		case "u":
			return m, m.runUsers()
		case "b":
			m.pending = "backup"
			m.mode = dbInputName
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}
}

func (m *Model) failMsg(s string) tea.Cmd {
	return func() tea.Msg {
		return execMsg{err: s}
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

func (m *Model) runList() tea.Cmd {
	return m.execShell(m.listCmd())
}

func (m *Model) listCmd() string {
	switch m.eng {
	case engMySQL:
		return `mysql -N -e 'SHOW DATABASES'`
	case engMongo:
		return `mongosh --quiet --eval 'JSON.stringify(db.getMongo().getDBNames())'`
	default:
		return `psql -l`
	}
}

func (m *Model) runCreate(name string) tea.Cmd {
	var cmd string
	switch m.eng {
	case engMySQL:
		cmd = fmt.Sprintf(`mysql -e 'CREATE DATABASE %s'`, name)
	case engMongo:
		cmd = fmt.Sprintf(`mongosh --quiet --eval 'db.getSiblingDB("%s").createCollection("_xm_init")'`, name)
	default:
		cmd = fmt.Sprintf("createdb %s", name)
	}
	return m.execShell(cmd)
}

func (m *Model) runDrop(name string) tea.Cmd {
	var cmd string
	switch m.eng {
	case engMySQL:
		cmd = fmt.Sprintf(`mysql -e 'DROP DATABASE %s'`, name)
	case engMongo:
		cmd = fmt.Sprintf(`mongosh --quiet --eval 'db.getSiblingDB("%s").dropDatabase()'`, name)
	default:
		cmd = fmt.Sprintf("dropdb %s", name)
	}
	return m.execShell(cmd)
}

func (m *Model) runUsers() tea.Cmd {
	var cmd string
	switch m.eng {
	case engMySQL:
		cmd = `mysql -e "SELECT user,host FROM mysql.user"`
	case engMongo:
		cmd = `mongosh --quiet --eval 'db.getUsers()'`
	default:
		cmd = `psql -c '\du'`
	}
	return m.execShell(cmd)
}

func (m *Model) runBackup(name string) tea.Cmd {
	path := fmt.Sprintf("/tmp/xmanager_%s_backup", name)
	var cmd string
	switch m.eng {
	case engMySQL:
		cmd = fmt.Sprintf("mysqldump %s > %s.sql", name, path)
	case engMongo:
		cmd = fmt.Sprintf("mongodump --db %s --archive=%s.archive", name, path)
	default:
		cmd = fmt.Sprintf("pg_dump -Fc %s -f %s.dump", name, path)
	}
	return m.execShell(cmd)
}

func (m *Model) execShell(cmd string) tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return execMsg{err: "No SSH session. Connect to a server first."}
		}
		res, err := ex.Run(cmd)
		if err != nil {
			return execMsg{err: err.Error()}
		}
		out := res.Stdout
		if res.Stderr != "" {
			if out != "" {
				out += "\n"
			}
			out += res.Stderr
		}
		if res.ExitCode != 0 {
			return execMsg{out: out, err: fmt.Sprintf("exit code %d", res.ExitCode)}
		}
		return execMsg{out: out}
	}
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Database Manager")
	if m.ctx.ServerID == 0 {
		help := components.NewHelpBar(components.KeyBinding{Key: "esc", Desc: "back"})
		help.Width = m.width
		return lipgloss.JoinVertical(lipgloss.Left, title, "", theme.WarningText().Render("  Connect to a server first."), "", help.View())
	}
	tabs := m.renderTabs()
	switch m.mode {
	case dbOutput:
		panel := theme.PanelStyle().Width(m.width - 2).MaxHeight(m.height - 6).Render(m.lastOut)
		foot := theme.MutedText().Render("  Esc: return")
		return lipgloss.JoinVertical(lipgloss.Left, title, tabs, "", panel, "", foot)
	case dbConfirmDrop:
		q := theme.WarningText().Render(fmt.Sprintf("  Drop database %q on %s? (y/n)", m.nameInput.Value(), m.engLabel()))
		help := theme.MutedText().Render("  This is destructive.")
		return lipgloss.JoinVertical(lipgloss.Left, title, tabs, "", q, "", help)
	case dbInputName:
		hint := theme.MutedText().Render(fmt.Sprintf("  %s — enter name, Esc cancel", m.pending))
		return lipgloss.JoinVertical(lipgloss.Left, title, tabs, "", "  "+m.nameInput.View(), "", hint)
	default:
		help := components.NewHelpBar(
			components.KeyBinding{Key: "1-3", Desc: "engine"},
			components.KeyBinding{Key: "tab", Desc: "next eng"},
			components.KeyBinding{Key: "l", Desc: "list"},
			components.KeyBinding{Key: "c", Desc: "create"},
			components.KeyBinding{Key: "d", Desc: "drop"},
			components.KeyBinding{Key: "u", Desc: "users"},
			components.KeyBinding{Key: "b", Desc: "backup"},
			components.KeyBinding{Key: "esc", Desc: "back"},
		)
		help.Width = m.width
		cmd := theme.SubtitleStyle().Render("  Command: " + m.listCmd())
		return lipgloss.JoinVertical(lipgloss.Left, title, tabs, cmd, "", help.View())
	}
}

func (m *Model) renderTabs() string {
	names := []string{"PostgreSQL", "MySQL", "MongoDB"}
	var parts []string
	for i, n := range names {
		st := theme.MutedText()
		if engine(i) == m.eng {
			st = theme.HeaderStyle()
		}
		parts = append(parts, st.Render(fmt.Sprintf(" %d:%s ", i+1, n)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

var _ shared.Screen = (*Model)(nil)
