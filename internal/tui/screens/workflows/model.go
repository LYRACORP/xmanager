package workflows

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type loadedMsg struct{ items []storage.Workflow }
type doneMsg struct {
	text string
	err  error
}

type Model struct {
	ctx     *shared.AppContext
	items   []storage.Workflow
	tbl     components.ListTable
	message string
	width   int
	height  int
}

func New(ctx *shared.AppContext) *Model { return &Model{ctx: ctx} }

func (m *Model) Name() string { return "Workflows" }
func (m *Model) OnNavigate(_ map[string]interface{}) {}
func (m *Model) SetSize(w, h int)                    { m.width, m.height = w, h; m.rebuild() }

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "enter", Desc: "run"},
		{Key: "e", Desc: "toggle enable"},
		{Key: "r", Desc: "refresh"},
	}
}

func (m *Model) rebuild() {
	h := layout.BodyHeight(m.height, components.FrameChromeRows(true)+1, 4)
	cols := []table.Column{
		{Title: "Name", Width: 28},
		{Title: "Trigger", Width: 12},
		{Title: "Enabled", Width: 10},
		{Title: "Risk", Width: 14},
	}
	rows := make([]table.Row, len(m.items))
	for i, w := range m.items {
		en := "no"
		if w.Enabled {
			en = "yes"
		}
		risk := "ok"
		if w.HasDestructive {
			risk = "destructive"
		}
		rows[i] = table.Row{w.Name, w.Trigger, en, risk}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(true)
}

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		var list []storage.Workflow
		if m.ctx != nil && m.ctx.DB != nil {
			_ = m.ctx.DB.Order("id desc").Find(&list).Error
		}
		return loadedMsg{items: list}
	}
}

func (m *Model) selected() (storage.Workflow, bool) {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.items) {
		return storage.Workflow{}, false
	}
	return m.items[i], true
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.items = msg.items
		m.rebuild()
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = msg.text
		}
		return m, m.load()
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			return m, m.load()
		case "enter":
			wf, ok := m.selected()
			if !ok || m.ctx.Workflows == nil {
				return m, nil
			}
			return m, func() tea.Msg {
				run, err := m.ctx.Workflows.Run(context.Background(), wf.ID, "tui")
				if err != nil {
					return doneMsg{err: err}
				}
				return doneMsg{text: fmt.Sprintf("run %d %s", run.ID, run.Status)}
			}
		case "e":
			wf, ok := m.selected()
			if !ok {
				return m, nil
			}
			wf.Enabled = !wf.Enabled
			_ = m.ctx.DB.Save(&wf).Error
			return m, m.load()
		}
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	title := theme.ScreenChrome("Workflows", "enter run · e enable · edit graphs in the web panel", m.width)
	body := m.tbl.View()
	if m.message != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, theme.MutedText().Render(m.message))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}
