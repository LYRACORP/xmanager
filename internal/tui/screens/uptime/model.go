package uptime

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
	modeAdd
	modeConfirmDelete
)

type monitorsLoadedMsg struct{ monitors []storage.UptimeMonitor }

// Model lists uptime monitors and allows adding/removing them.
type Model struct {
	ctx      *shared.AppContext
	monitors []storage.UptimeMonitor
	tbl      components.ListTable
	mode     mode
	nameIn   textinput.Model
	urlIn    textinput.Model
	hostIn   textinput.Model
	portIn   textinput.Model
	formIdx  int
	deleteID uint
	message  string
	width    int
	height   int
	spinner  components.LoadingSpinner
	loading  bool
}

func New(ctx *shared.AppContext) *Model {
	nameIn := textinput.New()
	nameIn.Placeholder = "my-site"
	nameIn.Prompt = "Name: "
	nameIn.Width = 36

	urlIn := textinput.New()
	urlIn.Placeholder = "https://example.com  (leave blank for TCP)"
	urlIn.Prompt = "URL: "
	urlIn.Width = 60

	hostIn := textinput.New()
	hostIn.Placeholder = "192.168.1.1  (TCP check host)"
	hostIn.Prompt = "Host: "
	hostIn.Width = 36

	portIn := textinput.New()
	portIn.Placeholder = "80"
	portIn.Prompt = "Port: "
	portIn.Width = 10
	portIn.SetValue("80")

	return &Model{ctx: ctx, nameIn: nameIn, urlIn: urlIn, hostIn: hostIn, portIn: portIn}
}

func (m *Model) Name() string                        { return "Uptime" }
func (m *Model) SetSize(w, h int)                    { m.width = w; m.height = h; m.rebuildTable() }
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
			{Key: "a", Desc: "add monitor"},
			{Key: "d", Desc: "delete"},
			{Key: "t", Desc: "toggle"},
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
		var monitors []storage.UptimeMonitor
		m.ctx.DB.Order("name asc").Find(&monitors)
		return monitorsLoadedMsg{monitors: monitors}
	}
}

func (m *Model) rebuildTable() {
	chrome := components.FrameChromeRows(true) + 1
	h := layout.BodyHeight(m.height, chrome, 5)
	cols := []table.Column{
		{Title: "On", Width: 3},
		{Title: "Name", Width: 20},
		{Title: "URL / Host", Width: 30},
		{Title: "Status", Width: 10},
		{Title: "Interval", Width: 0},
	}
	rows := make([]table.Row, len(m.monitors))
	for i, mon := range m.monitors {
		enabled := "✓"
		if !mon.Enabled {
			enabled = "–"
		}
		target := mon.URL
		if target == "" {
			target = fmt.Sprintf("%s:%d", mon.Host, mon.Port)
		}
		rows[i] = table.Row{
			enabled, mon.Name, target, mon.LastStatus,
			fmt.Sprintf("%ds", mon.IntervalSec),
		}
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
	case monitorsLoadedMsg:
		m.loading = false
		m.spinner = m.spinner.SetActive(false)
		m.monitors = msg.monitors
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
				id := m.deleteID
				m.mode = modeList
				m.loading = true
				m.spinner = components.NewLoadingSpinner("Deleting…")
				return m, tea.Batch(m.spinner.Tick(), func() tea.Msg {
					m.ctx.DB.Delete(&storage.UptimeMonitor{}, id)
					var monitors []storage.UptimeMonitor
					m.ctx.DB.Order("name asc").Find(&monitors)
					return monitorsLoadedMsg{monitors: monitors}
				})
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
		m.urlIn.SetValue("")
		m.hostIn.SetValue("")
		m.portIn.SetValue("80")
		m.formIdx = 0
		m.nameIn.Focus()
		m.tbl = m.tbl.SetFocused(false)
		return m, nil
	case "d", "delete":
		if idx := m.tbl.Cursor(); idx < len(m.monitors) {
			m.deleteID = m.monitors[idx].ID
			m.mode = modeConfirmDelete
		}
		return m, nil
	case "t":
		if idx := m.tbl.Cursor(); idx < len(m.monitors) {
			mon := m.monitors[idx]
			id := mon.ID
			enabled := !mon.Enabled
			m.loading = true
			m.spinner = components.NewLoadingSpinner("Updating…")
			return m, tea.Batch(m.spinner.Tick(), func() tea.Msg {
				m.ctx.DB.Model(&storage.UptimeMonitor{}).Where("id = ?", id).Update("enabled", enabled)
				var monitors []storage.UptimeMonitor
				m.ctx.DB.Order("name asc").Find(&monitors)
				return monitorsLoadedMsg{monitors: monitors}
			})
		}
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
	inputs := []*textinput.Model{&m.nameIn, &m.urlIn, &m.hostIn, &m.portIn}
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
		return m, m.saveMonitor()
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) saveMonitor() tea.Cmd {
	return func() tea.Msg {
		port := 80
		_, _ = fmt.Sscanf(m.portIn.Value(), "%d", &port)
		mon := storage.UptimeMonitor{
			Name:        m.nameIn.Value(),
			URL:         m.urlIn.Value(),
			Host:        m.hostIn.Value(),
			Port:        port,
			IntervalSec: 60,
			Enabled:     true,
			LastStatus:  "unknown",
		}
		m.ctx.DB.Create(&mon)
		m.mode = modeList
		var monitors []storage.UptimeMonitor
		m.ctx.DB.Order("name asc").Find(&monitors)
		return monitorsLoadedMsg{monitors: monitors}
	}
}

func (m *Model) View() string {
	if m.mode == modeAdd {
		return m.viewForm()
	}
	body := m.tbl.View()
	if m.loading {
		body = m.spinner.View()
	}
	frame := components.ScreenFrame{
		Title:       "Uptime Monitors",
		Subtitle:    "HTTP and TCP availability checks",
		Width:       m.width,
		Body:        body,
		LocalChrome: components.FrameChromeRows(true) + 1,
	}
	parts := []string{frame.View()}
	if m.message != "" {
		parts = append(parts, " "+m.message)
	}
	if m.mode == modeConfirmDelete {
		parts = append(parts, "", " "+theme.WarningText().Render("Delete this monitor? (y/n)"))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Add Monitor", "URL or TCP host check", m.width)
	var b strings.Builder
	inputs := []textinput.Model{m.nameIn, m.urlIn, m.hostIn, m.portIn}
	for i, ti := range inputs {
		styled := components.ApplyInputTheme(ti, m.width, i == m.formIdx)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, i == m.formIdx))
		b.WriteByte('\n')
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), footer)
}
