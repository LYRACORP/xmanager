package errtrack

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeTable mode = iota
	modeDetail
	modeConfirmMute
	modeConfirmResolve
)

type groupedErr struct {
	fingerprint string
	service     string
	message     string
	severity    string
	count       int
	firstSeen   time.Time
	lastSeen    time.Time
	anyMuted    bool
	anyResolved bool
	repID       uint
}

type loadedMsg struct {
	rows []groupedErr
}

type Model struct {
	ctx    *shared.AppContext
	table  table.Model
	rows   []groupedErr
	mode   mode
	width  int
	height int
	detail storage.ErrorEvent
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "Error Tracker" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.load
}

func (m *Model) load() tea.Msg {
	if m.ctx.ServerID == 0 || m.ctx.DB == nil {
		return loadedMsg{rows: nil}
	}
	var events []storage.ErrorEvent
	m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("last_seen desc").Find(&events)
	byFP := make(map[string]*groupedErr)
	order := []string{}
	for _, e := range events {
		k := e.Fingerprint
		if k == "" {
			k = fmt.Sprintf("_id_%d", e.ID)
		}
		g, ok := byFP[k]
		if !ok {
			g = &groupedErr{
				fingerprint: k,
				service:     e.Service,
				message:     e.Message,
				severity:    e.Severity,
				count:       0,
				firstSeen:   e.FirstSeen,
				lastSeen:    e.LastSeen,
				repID:       e.ID,
			}
			byFP[k] = g
			order = append(order, k)
		}
		add := e.Count
		if add < 1 {
			add = 1
		}
		g.count += add
		if e.Service != "" {
			g.service = e.Service
		}
		if e.FirstSeen.Before(g.firstSeen) || g.firstSeen.IsZero() {
			g.firstSeen = e.FirstSeen
		}
		if e.LastSeen.After(g.lastSeen) {
			g.lastSeen = e.LastSeen
		}
		if e.Muted {
			g.anyMuted = true
		}
		if e.Resolved {
			g.anyResolved = true
		}
		sevRank := severityRank(e.Severity)
		if sevRank > severityRank(g.severity) {
			g.severity = e.Severity
			g.message = e.Message
			g.repID = e.ID
		}
	}
	out := make([]groupedErr, 0, len(order))
	for _, k := range order {
		out = append(out, *byFP[k])
	}
	return loadedMsg{rows: out}
}

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 4
	case "error":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func (m *Model) rebuildTable() {
	cols := []table.Column{
		{Title: "Severity", Width: 10},
		{Title: "Svc", Width: 10},
		{Title: "Message", Width: 28},
		{Title: "#", Width: 5},
		{Title: "First", Width: 16},
		{Title: "Last", Width: 16},
		{Title: "Flags", Width: 8},
	}
	trows := make([]table.Row, len(m.rows))
	for i, r := range m.rows {
		msg := r.message
		if len(msg) > 40 {
			msg = msg[:37] + "..."
		}
		flags := ""
		if r.anyMuted {
			flags += "M"
		}
		if r.anyResolved {
			flags += "R"
		}
		if flags == "" {
			flags = "—"
		}
		trows[i] = table.Row{
			severityBadge(r.severity),
			trunc(r.service, 10),
			trunc(msg, 28),
			fmt.Sprintf("%d", r.count),
			r.firstSeen.Format("01-02 15:04"),
			r.lastSeen.Format("01-02 15:04"),
			flags,
		}
	}
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	m.table = components.StyledTable(cols, trows, h)
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func severityBadge(s string) string {
	switch strings.ToLower(s) {
	case "critical":
		return theme.BadgeStyle(theme.Current.Critical).Render("CRIT")
	case "error":
		return theme.ErrorBadge().Render("ERR")
	case "warning":
		return theme.WarningBadge().Render("WARN")
	case "info":
		return theme.InfoBadge().Render("INFO")
	default:
		return theme.MutedText().Render(strings.ToUpper(trunc(s, 4)))
	}
}

func (m *Model) selected() (groupedErr, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.rows) {
		return groupedErr{}, false
	}
	return m.rows[i], true
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.rows = msg.rows
		m.rebuildTable()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	if m.mode == modeTable {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch m.mode {
	case modeDetail:
		switch msg.String() {
		case "esc", "enter", "q":
			m.mode = modeTable
			return m, nil
		}
		return m, nil
	case modeConfirmMute, modeConfirmResolve:
		switch msg.String() {
		case "y", "Y":
			if m.mode == modeConfirmMute {
				m.mode = modeTable
				return m, m.setMuted(true)
			}
			m.mode = modeTable
			return m, m.setResolved(true)
		case "n", "N", "esc":
			m.mode = modeTable
			return m, nil
		}
		return m, nil
	default:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			return m, m.load
		case "v":
			g, ok := m.selected()
			if !ok {
				return m, nil
			}
			var ev storage.ErrorEvent
			if m.ctx.DB.First(&ev, g.repID).Error != nil {
				return m, nil
			}
			m.detail = ev
			m.mode = modeDetail
			return m, nil
		case "m":
			if _, ok := m.selected(); ok {
				m.mode = modeConfirmMute
			}
			return m, nil
		case "x":
			if _, ok := m.selected(); ok {
				m.mode = modeConfirmResolve
			}
			return m, nil
		case "a":
			g, ok := m.selected()
			if !ok {
				return m, nil
			}
			prefill := fmt.Sprintf("Analyze and suggest fixes for this error (service %s): %s", g.service, g.message)
			return m, func() tea.Msg {
				return shared.NavigateMsg{
					Screen:   shared.ScreenChat,
					ServerID: m.ctx.ServerID,
					Params: map[string]interface{}{
						"prefill": prefill,
					},
				}
			}
		}
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
}

func (m *Model) setMuted(v bool) tea.Cmd {
	g, ok := m.selected()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		fp := g.fingerprint
		if strings.HasPrefix(fp, "_id_") {
			var id uint
			if _, err := fmt.Sscanf(fp, "_id_%d", &id); err == nil {
				m.ctx.DB.Model(&storage.ErrorEvent{}).Where("id = ? AND server_id = ?", id, m.ctx.ServerID).Update("muted", v)
			}
		} else {
			m.ctx.DB.Model(&storage.ErrorEvent{}).Where("fingerprint = ? AND server_id = ?", fp, m.ctx.ServerID).Update("muted", v)
		}
		return m.load()
	}
}

func (m *Model) setResolved(v bool) tea.Cmd {
	g, ok := m.selected()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		fp := g.fingerprint
		if strings.HasPrefix(fp, "_id_") {
			var id uint
			if _, err := fmt.Sscanf(fp, "_id_%d", &id); err == nil {
				m.ctx.DB.Model(&storage.ErrorEvent{}).Where("id = ? AND server_id = ?", id, m.ctx.ServerID).Update("resolved", v)
			}
		} else {
			m.ctx.DB.Model(&storage.ErrorEvent{}).Where("fingerprint = ? AND server_id = ?", fp, m.ctx.ServerID).Update("resolved", v)
		}
		return m.load()
	}
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Error Tracker")
	if m.ctx.ServerID == 0 {
		help := components.NewHelpBar(components.KeyBinding{Key: "esc", Desc: "back"})
		help.Width = m.width
		return lipgloss.JoinVertical(lipgloss.Left, title, "", theme.WarningText().Render("  Select a connected server."), "", help.View())
	}
	switch m.mode {
	case modeDetail:
		return m.viewDetail(title)
	case modeConfirmMute:
		return m.viewConfirm(title, "Mute all events with this fingerprint?")
	case modeConfirmResolve:
		return m.viewConfirm(title, "Mark all events with this fingerprint as resolved?")
	default:
		return m.viewTable(title)
	}
}

func (m *Model) viewTable(title string) string {
	help := components.NewHelpBar(
		components.KeyBinding{Key: "v", Desc: "details"},
		components.KeyBinding{Key: "m", Desc: "mute"},
		components.KeyBinding{Key: "x", Desc: "resolve"},
		components.KeyBinding{Key: "a", Desc: "ask AI"},
		components.KeyBinding{Key: "r", Desc: "refresh"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width
	if len(m.rows) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title, "", theme.MutedText().Render("  No error events."), "", help.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, m.table.View(), "", help.View())
}

func (m *Model) viewDetail(title string) string {
	sub := theme.SubtitleStyle().Render(fmt.Sprintf("Service: %s  Severity: %s", m.detail.Service, m.detail.Severity))
	body := theme.PanelStyle().Width(m.width - 2).Render(
		theme.TitleStyle().Render("Message") + "\n" + m.detail.Message + "\n\n" +
			theme.TitleStyle().Render("Stack") + "\n" + strings.TrimSpace(m.detail.StackTrace),
	)
	foot := theme.MutedText().Render("  Esc: close")
	return lipgloss.JoinVertical(lipgloss.Left, title, sub, "", body, "", foot)
}

func (m *Model) viewConfirm(title, q string) string {
	return lipgloss.JoinVertical(lipgloss.Left, title, "", theme.WarningText().Render("  "+q), "",
		theme.MutedText().Render("  y: yes  n/esc: cancel"))
}

var _ shared.Screen = (*Model)(nil)
