package projects

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
	modeList mode = iota
	modeCreate
	modeConfirmDeploy
)

type projectsLoadedMsg struct{ projects []storage.Project }
type deployResultMsg struct {
	name string
	err  error
}

// Model lists projects for the current server and lets users create/deploy them.
type Model struct {
	ctx      *shared.AppContext
	projects []storage.Project
	tbl      components.ListTable
	mode     mode
	nameIn   textinput.Model
	typeIn   textinput.Model
	srcIn    textinput.Model
	formIdx  int
	message  string
	width    int
	height   int
	spinner  components.LoadingSpinner
	loading  bool
}

func New(ctx *shared.AppContext) *Model {
	nameIn := textinput.New()
	nameIn.Placeholder = "my-app"
	nameIn.Prompt = "Name: "
	nameIn.Width = 36

	typeIn := textinput.New()
	typeIn.Placeholder = "compose | image | git | dockerfile"
	typeIn.Prompt = "Type: "
	typeIn.Width = 36

	srcIn := textinput.New()
	srcIn.Placeholder = "nginx:latest  /path/docker-compose.yml  https://..."
	srcIn.Prompt = "Source: "
	srcIn.Width = 60

	return &Model{ctx: ctx, nameIn: nameIn, typeIn: typeIn, srcIn: srcIn}
}

func (m *Model) Name() string                        { return "Projects" }
func (m *Model) SetSize(w, h int)                    { m.width = w; m.height = h; m.rebuildTable() }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeCreate:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "enter", Desc: "save"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeConfirmDeploy:
		return []components.KeyBinding{
			{Key: "y", Desc: "confirm deploy"},
			{Key: "n/esc", Desc: "cancel"},
		}
	default:
		return []components.KeyBinding{
			{Key: "c", Desc: "create"},
			{Key: "enter", Desc: "deploy"},
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
		var ps []storage.Project
		m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("name asc").Find(&ps)
		return projectsLoadedMsg{projects: ps}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Type", Width: 12},
		{Title: "Status", Width: 12},
		{Title: "Domain", Width: 0},
	}
	rows := make([]table.Row, len(m.projects))
	for i, p := range m.projects {
		rows[i] = table.Row{p.Name, p.Type, p.DeployStatus, p.Domain}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(m.mode == modeList)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case bspinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case projectsLoadedMsg:
		m.loading = false
		m.spinner = m.spinner.SetActive(false)
		m.projects = msg.projects
		m.rebuildTable()
		return m, nil
	case deployResultMsg:
		if msg.err != nil {
			m.message = fmt.Sprintf("Deploy failed: %v", msg.err)
		} else {
			m.message = fmt.Sprintf("Deployed %s", msg.name)
		}
		return m, m.startLoad()
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeCreate:
			return m.updateCreate(msg)
		case modeConfirmDeploy:
			if msg.String() == "y" || msg.String() == "Y" {
				idx := m.tbl.Cursor()
				if idx < len(m.projects) {
					p := m.projects[idx]
					return m, m.deploy(p)
				}
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
	case "c":
		m.mode = modeCreate
		m.nameIn.SetValue("")
		m.typeIn.SetValue("")
		m.srcIn.SetValue("")
		m.formIdx = 0
		m.nameIn.Focus()
		m.tbl = m.tbl.SetFocused(false)
		return m, nil
	case "enter":
		if len(m.projects) > 0 {
			m.mode = modeConfirmDeploy
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

func (m *Model) updateCreate(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.nameIn, &m.typeIn, &m.srcIn}
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
		return m, m.saveProject()
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) saveProject() tea.Cmd {
	return func() tea.Msg {
		p := storage.Project{
			ServerID: m.ctx.ServerID,
			Name:     m.nameIn.Value(),
			Type:     m.typeIn.Value(),
			Source:   m.srcIn.Value(),
		}
		m.ctx.DB.Create(&p)
		m.mode = modeList
		var ps []storage.Project
		m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("name asc").Find(&ps)
		return projectsLoadedMsg{projects: ps}
	}
}

func (m *Model) deploy(p storage.Project) tea.Cmd {
	return func() tea.Msg {
		m.mode = modeList
		// Record a deploy history entry; actual deploy is out-of-scope for this UI stub.
		hist := storage.DeployHistory{
			ServerID:  p.ServerID,
			ProjectID: p.ID,
			Service:   p.Name,
			Method:    p.Type,
			Status:    "triggered",
		}
		m.ctx.DB.Create(&hist)
		m.ctx.DB.Model(&p).Update("deploy_status", "deploying")
		return deployResultMsg{name: p.Name}
	}
}

func (m *Model) View() string {
	if m.mode == modeCreate {
		return m.viewForm()
	}

	body := m.tbl.View()
	if m.loading {
		body = m.spinner.View()
	}
	frame := components.ScreenFrame{
		Title:       "Projects",
		Subtitle:    "deploy and manage applications",
		Width:       m.width,
		Body:        body,
		LocalChrome: components.FrameChromeRows(true) + 1,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	if m.mode == modeConfirmDeploy {
		parts = append(parts, "", " "+theme.WarningText().Render("Trigger deploy for this project? (y/n)"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Create Project", "new application", m.width)
	var b strings.Builder
	inputs := []textinput.Model{m.nameIn, m.typeIn, m.srcIn}
	for i, ti := range inputs {
		styled := components.ApplyInputTheme(ti, m.width, i == m.formIdx)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, i == m.formIdx))
		b.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), footer)
}
