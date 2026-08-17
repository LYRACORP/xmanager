package datetime

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/hosttime"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeList mode = iota
	modeFilter
	modeClock
)

type loadedMsg struct {
	status hosttime.Status
	zones  []string
	err    string
}

type doneMsg struct {
	ok  string
	err string
}

type Model struct {
	ctx     *shared.AppContext
	status  hosttime.Status
	zones   []string
	shown   []string
	tbl     components.ListTable
	filter  textinput.Model
	dateIn  textinput.Model
	timeIn  textinput.Model
	mode    mode
	formIdx int
	message string
	loading bool
	width   int
	height  int
}

func New(ctx *shared.AppContext) *Model {
	f := textinput.New()
	f.Placeholder = "filter timezones"
	f.Prompt = "/ "
	f.Width = 40
	d := textinput.New()
	d.Placeholder = "YYYY-MM-DD"
	d.Prompt = "Date: "
	d.Width = 16
	ti := textinput.New()
	ti.Placeholder = "HH:MM:SS"
	ti.Prompt = "Time: "
	ti.Width = 12
	return &Model{ctx: ctx, filter: f, dateIn: d, timeIn: ti}
}

func (m *Model) Name() string { return "Date & time" }
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.rebuildTable()
}
func (m *Model) OnNavigate(_ map[string]interface{}) { m.loading = true }

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeFilter:
		return []components.KeyBinding{
			{Key: "enter", Desc: "apply zone"},
			{Key: "esc", Desc: "stop filter"},
		}
	case modeClock:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "enter", Desc: "set clock"},
			{Key: "esc", Desc: "cancel"},
		}
	default:
		return []components.KeyBinding{
			{Key: "/", Desc: "filter zones"},
			{Key: "enter", Desc: "set timezone"},
			{Key: "t", Desc: "set clock"},
			{Key: "n", Desc: "toggle NTP"},
			{Key: "s", Desc: "sync NTP"},
			{Key: "r", Desc: "refresh"},
			{Key: "b/esc", Desc: "back"},
		}
	}
}

func (m *Model) Init() tea.Cmd {
	m.loading = true
	return m.load()
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return loadedMsg{err: "not connected"}
		}
		st, err := hosttime.ReadStatus(ex)
		if err != nil {
			return loadedMsg{err: err.Error()}
		}
		zones, err := hosttime.ListTimezones(ex)
		if err != nil {
			return loadedMsg{status: st, err: err.Error()}
		}
		return loadedMsg{status: st, zones: zones}
	}
}

func selectedZone(row table.Row) string {
	if len(row) == 0 {
		return ""
	}
	s := strings.TrimSpace(row[0])
	s = strings.TrimPrefix(s, "●")
	return strings.TrimSpace(s)
}

func filterZones(zones []string, q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return zones
	}
	var out []string
	for _, z := range zones {
		if strings.Contains(strings.ToLower(z), q) {
			out = append(out, z)
		}
	}
	return out
}

func (m *Model) rebuildTable() {
	m.shown = filterZones(m.zones, m.filter.Value())
	chrome := components.FrameChromeRows(true) + 8
	h := layout.BodyHeight(m.height, chrome, 6)
	cols := []table.Column{{Title: "Timezone", Width: 0}}
	rows := make([]table.Row, len(m.shown))
	for i, z := range m.shown {
		mark := "  "
		if z == m.status.Timezone {
			mark = "● "
		}
		rows[i] = table.Row{mark + z}
	}
	m.tbl = m.tbl.SetData(layout.ContentWidth(m.width), cols, rows, h)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.loading = false
		m.status = msg.status
		if msg.zones != nil {
			m.zones = msg.zones
		}
		if msg.err != "" {
			m.message = msg.err
		}
		if m.status.LocalTime != "" && m.mode != modeClock {
			parts := strings.Fields(m.status.LocalTime)
			if len(parts) >= 2 {
				m.dateIn.SetValue(parts[0])
				m.timeIn.SetValue(parts[1])
			}
		}
		m.rebuildTable()
		return m, nil
	case doneMsg:
		if msg.err != "" {
			m.message = msg.err
		} else {
			m.message = msg.ok
		}
		m.mode = modeList
		m.tbl = m.tbl.SetFocused(true)
		return m, m.load()
	case tea.KeyMsg:
		switch m.mode {
		case modeFilter:
			return m.updateFilter(msg)
		case modeClock:
			return m.updateClock(msg)
		default:
			return m.updateList(msg)
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
	case "/":
		m.mode = modeFilter
		m.filter.Focus()
		m.tbl = m.tbl.SetFocused(false)
		return m, textinput.Blink
	case "n":
		on := !m.status.NTP
		return m, m.run(func() error {
			ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
			if !ok {
				return fmt.Errorf("not connected")
			}
			return hosttime.SetNTP(ex, on)
		}, fmt.Sprintf("NTP %v", on))
	case "s":
		return m, m.run(func() error {
			ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
			if !ok {
				return fmt.Errorf("not connected")
			}
			_, err := hosttime.SyncNTP(ex)
			return err
		}, "NTP sync")
	case "t":
		m.mode = modeClock
		m.formIdx = 0
		m.dateIn.Focus()
		m.timeIn.Blur()
		m.tbl = m.tbl.SetFocused(false)
		return m, textinput.Blink
	case "enter":
		if zone := selectedZone(m.tbl.SelectedRow()); zone != "" {
			return m, m.applyZone(zone)
		}
	case "r":
		m.loading = true
		return m, m.load()
	case "b", "esc", "q":
		return m, func() tea.Msg { return shared.GoBackMsg{} }
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *Model) updateFilter(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.filter.Blur()
		m.tbl = m.tbl.SetFocused(true)
		return m, nil
	case "enter":
		if zone := selectedZone(m.tbl.SelectedRow()); zone != "" {
			return m, m.applyZone(zone)
		}
		return m, nil
	case "up", "down":
		var cmd tea.Cmd
		m.tbl, cmd = m.tbl.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.rebuildTable()
	return m, cmd
}

func (m *Model) updateClock(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.dateIn, &m.timeIn}
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.dateIn.Blur()
		m.timeIn.Blur()
		m.tbl = m.tbl.SetFocused(true)
		return m, nil
	case "tab":
		inputs[m.formIdx].Blur()
		m.formIdx = (m.formIdx + 1) % len(inputs)
		inputs[m.formIdx].Focus()
		return m, nil
	case "enter":
		clock := strings.TrimSpace(m.dateIn.Value()) + " " + strings.TrimSpace(m.timeIn.Value())
		return m, m.run(func() error {
			ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
			if !ok {
				return fmt.Errorf("not connected")
			}
			return hosttime.SetTime(ex, clock)
		}, "clock set")
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func (m *Model) applyZone(zone string) tea.Cmd {
	return m.run(func() error {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return fmt.Errorf("not connected")
		}
		return hosttime.SetTimezone(ex, zone)
	}, "timezone "+zone)
}

func (m *Model) run(fn func() error, ok string) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return doneMsg{err: err.Error()}
		}
		return doneMsg{ok: ok}
	}
}

func (m *Model) View() string {
	var b strings.Builder
	if m.loading {
		b.WriteString(theme.MutedText().Render("  loading…") + "\n")
	} else {
		ntp := "off"
		if m.status.NTP {
			ntp = "on"
		}
		if m.status.NTPSynchronized {
			ntp += " · synced"
		}
		b.WriteString("  " + theme.SuccessText().Render(m.status.LocalTime) + "\n")
		b.WriteString("  zone " + m.status.Timezone + "  ·  NTP " + ntp + "\n")
	}
	if m.message != "" {
		b.WriteString("  " + theme.WarningText().Render(m.message) + "\n")
	}
	if m.mode == modeClock {
		b.WriteString("\n  " + m.dateIn.View() + "  " + m.timeIn.View() + "\n")
		b.WriteString(theme.MutedText().Render("  NTP must be off to set the clock manually") + "\n")
	} else {
		b.WriteString("\n  " + m.filter.View() + "\n")
		b.WriteString(m.tbl.View())
	}
	return components.ScreenFrame{
		Title:       "Date & time",
		Subtitle:    "timezone · clock · NTP",
		Width:       m.width,
		LocalChrome: components.FrameChromeRows(true),
		Body:        b.String(),
	}.View()
}
