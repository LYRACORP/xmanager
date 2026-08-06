package cronjobs

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	modeConfirmDelete
)

type jobsLoadedMsg struct{ jobs []storage.CronJob }

// Model lists and manages cron jobs for the current server.
type Model struct {
	ctx    *shared.AppContext
	jobs   []storage.CronJob
	tbl    components.ListTable
	mode   mode
	nameIn textinput.Model
	exprIn textinput.Model
	cmdIn  textinput.Model
	formIdx int
	deleteID uint
	message string
	width   int
	height  int
}

func New(ctx *shared.AppContext) *Model {
	nameIn := textinput.New()
	nameIn.Placeholder = "daily-backup"
	nameIn.Prompt = "Name: "
	nameIn.Width = 36

	exprIn := textinput.New()
	exprIn.Placeholder = "0 2 * * *"
	exprIn.Prompt = "Cron expr: "
	exprIn.Width = 36

	cmdIn := textinput.New()
	cmdIn.Placeholder = "/usr/local/bin/backup.sh"
	cmdIn.Prompt = "Command: "
	cmdIn.Width = 60

	return &Model{ctx: ctx, nameIn: nameIn, exprIn: exprIn, cmdIn: cmdIn}
}

func (m *Model) Name() string     { return "Cron Jobs" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeAdd:
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
			{Key: "a", Desc: "add"},
			{Key: "d", Desc: "delete"},
			{Key: "t", Desc: "toggle enable"},
			{Key: "r", Desc: "refresh"},
			{Key: "b/esc", Desc: "back"},
		}
	}
}

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var jobs []storage.CronJob
		m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("name asc").Find(&jobs)
		return jobsLoadedMsg{jobs: jobs}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "On", Width: 3},
		{Title: "Name", Width: 20},
		{Title: "Expression", Width: 15},
		{Title: "Status", Width: 10},
		{Title: "Last Run", Width: 0},
	}
	rows := make([]table.Row, len(m.jobs))
	for i, j := range m.jobs {
		enabled := "✓"
		if !j.Enabled {
			enabled = "–"
		}
		lastRun := "never"
		if j.LastRun != nil {
			lastRun = j.LastRun.Format("2006-01-02 15:04")
		}
		rows[i] = table.Row{enabled, j.Name, j.Expression, j.Status, lastRun}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(m.mode == modeList)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case jobsLoadedMsg:
		m.jobs = msg.jobs
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeAdd:
			return m.updateAdd(msg)
		case modeConfirmDelete:
			if msg.String() == "y" || msg.String() == "Y" {
				m.ctx.DB.Delete(&storage.CronJob{}, m.deleteID)
				m.mode = modeList
				return m, m.load()
			}
			m.mode = modeList
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
	case "a":
		m.mode = modeAdd
		m.nameIn.SetValue("")
		m.exprIn.SetValue("")
		m.cmdIn.SetValue("")
		m.formIdx = 0
		m.nameIn.Focus()
		m.tbl = m.tbl.SetFocused(false)
		return m, nil
	case "d", "delete":
		if idx := m.tbl.Cursor(); idx < len(m.jobs) {
			m.deleteID = m.jobs[idx].ID
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "t":
		if idx := m.tbl.Cursor(); idx < len(m.jobs) {
			j := m.jobs[idx]
			m.ctx.DB.Model(&j).Update("enabled", !j.Enabled)
			return m, m.load()
		}
	case "r":
		return m, m.load()
	case "b", "esc":
		return m, func() tea.Msg { return shared.GoBackMsg{} }
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *Model) updateAdd(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.nameIn, &m.exprIn, &m.cmdIn}
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
		return m, m.saveJob()
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) saveJob() tea.Cmd {
	return func() tea.Msg {
		job := storage.CronJob{
			ServerID:   m.ctx.ServerID,
			Name:       m.nameIn.Value(),
			Expression: m.exprIn.Value(),
			Command:    m.cmdIn.Value(),
			Enabled:    true,
			Status:     "idle",
		}
		m.ctx.DB.Create(&job)
		m.mode = modeList
		var jobs []storage.CronJob
		m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("name asc").Find(&jobs)
		return jobsLoadedMsg{jobs: jobs}
	}
}

func (m *Model) View() string {
	if m.mode == modeAdd {
		return m.viewForm()
	}
	frame := components.ScreenFrame{
		Title:       "Cron Jobs",
		Subtitle:    fmt.Sprintf("scheduled tasks for server #%d", m.ctx.ServerID),
		Width:       m.width,
		Body:        m.tbl.View(),
		LocalChrome: components.FrameChromeRows(true) + 1,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	if m.mode == modeConfirmDelete {
		parts = append(parts, "", " "+theme.WarningText().Render("Delete this cron job? (y/n)"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Add Cron Job", "schedule a task", m.width)
	var b strings.Builder
	inputs := []textinput.Model{m.nameIn, m.exprIn, m.cmdIn}
	for i, ti := range inputs {
		styled := components.ApplyInputTheme(ti, m.width, i == m.formIdx)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, i == m.formIdx))
		b.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), footer)
}
