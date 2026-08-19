package scripts

import (
	"fmt"
	"strings"

	bspinner "github.com/charmbracelet/bubbles/spinner"
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
	modeHistory mode = iota
	modeRun
)

type runsLoadedMsg struct{ runs []storage.ScriptRun }
type runResultMsg struct {
	name     string
	output   string
	exitCode int
}

// Model provides a script-runner UI: select type, enter content, choose targets.
type Model struct {
	ctx     *shared.AppContext
	runs    []storage.ScriptRun
	tbl     components.ListTable
	mode    mode
	nameIn  textinput.Model
	typeIn  textinput.Model
	bodyIn  textinput.Model
	targIn  textinput.Model
	formIdx int
	message string
	width   int
	height  int
	spinner components.LoadingSpinner
	loading bool
}

func New(ctx *shared.AppContext) *Model {
	nameIn := textinput.New()
	nameIn.Placeholder = "my-script"
	nameIn.Prompt = "Name: "
	nameIn.Width = 36

	typeIn := textinput.New()
	typeIn.Placeholder = "bash | python | node"
	typeIn.Prompt = "Type: "
	typeIn.Width = 20
	typeIn.SetValue("bash")

	bodyIn := textinput.New()
	bodyIn.Placeholder = "echo hello world"
	bodyIn.Prompt = "Script: "
	bodyIn.Width = 80

	targIn := textinput.New()
	targIn.Placeholder = "all  or  1,3,5  (server IDs)"
	targIn.Prompt = "Targets: "
	targIn.Width = 36
	targIn.SetValue("all")

	return &Model{ctx: ctx, nameIn: nameIn, typeIn: typeIn, bodyIn: bodyIn, targIn: targIn}
}

func (m *Model) Name() string                        { return "Scripts" }
func (m *Model) SetSize(w, h int)                    { m.width = w; m.height = h; m.rebuildTable() }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeRun:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "enter", Desc: "run"},
			{Key: "esc", Desc: "cancel"},
		}
	default:
		return []components.KeyBinding{
			{Key: "n", Desc: "new script"},
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

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var runs []storage.ScriptRun
		m.ctx.DB.Order("created_at desc").Limit(50).Find(&runs)
		return runsLoadedMsg{runs: runs}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Type", Width: 8},
		{Title: "Exit", Width: 5},
		{Title: "Targets", Width: 10},
		{Title: "Run At", Width: 0},
	}
	rows := make([]table.Row, len(m.runs))
	for i, r := range m.runs {
		rows[i] = table.Row{
			r.Name, r.ScriptType,
			fmt.Sprintf("%d", r.ExitCode),
			r.TargetIDs,
			r.StartedAt.Format("2006-01-02 15:04"),
		}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(m.mode == modeHistory)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case bspinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case runsLoadedMsg:
		m.loading = false
		m.spinner = m.spinner.SetActive(false)
		m.runs = msg.runs
		m.rebuildTable()
		return m, nil
	case runResultMsg:
		m.message = fmt.Sprintf("'%s' finished (exit %d)", msg.name, msg.exitCode)
		return m, m.startLoad()
	case tea.KeyMsg:
		switch m.mode {
		case modeHistory:
			return m.updateHistory(msg)
		case modeRun:
			return m.updateRunForm(msg)
		}
	}
	if m.mode == modeHistory {
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updateHistory(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "n":
		m.mode = modeRun
		m.nameIn.SetValue("")
		m.bodyIn.SetValue("")
		m.formIdx = 0
		m.nameIn.Focus()
		m.tbl = m.tbl.SetFocused(false)
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

func (m *Model) updateRunForm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.nameIn, &m.typeIn, &m.bodyIn, &m.targIn}
	switch msg.String() {
	case "esc":
		m.mode = modeHistory
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
		return m, m.runScript()
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) runScript() tea.Cmd {
	name := m.nameIn.Value()
	scriptType := m.typeIn.Value()
	body := m.bodyIn.Value()
	targets := m.targIn.Value()
	return func() tea.Msg {
		run := storage.ScriptRun{
			Name:       name,
			ScriptType: scriptType,
			Content:    body,
			TargetIDs:  targets,
		}
		m.ctx.DB.Create(&run)
		m.mode = modeHistory
		return runResultMsg{name: name, exitCode: 0}
	}
}

func (m *Model) View() string {
	if m.mode == modeRun {
		return m.viewForm()
	}
	body := m.tbl.View()
	if m.loading {
		body = m.spinner.View()
	}
	frame := components.ScreenFrame{
		Title:       "Scripts",
		Subtitle:    "run and track script executions",
		Width:       m.width,
		Body:        body,
		LocalChrome: components.FrameChromeRows(true) + 1,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Run Script", "execute on selected targets", m.width)
	var b strings.Builder
	inputs := []textinput.Model{m.nameIn, m.typeIn, m.bodyIn, m.targIn}
	for i, ti := range inputs {
		styled := components.ApplyInputTheme(ti, m.width, i == m.formIdx)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, i == m.formIdx))
		b.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: run  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), footer)
}
