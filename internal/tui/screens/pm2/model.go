package pm2

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type pm2Proc struct {
	PMID  int    `json:"pm_id"`
	Name  string `json:"name"`
	Monit struct {
		Memory float64 `json:"memory"`
		CPU    float64 `json:"cpu"`
	} `json:"monit"`
	PM2Env struct {
		Status      string `json:"status"`
		RestartTime int    `json:"restart_time"`
	} `json:"pm2_env"`
}

type listLoadedMsg struct {
	err       string
	brief     string
	processes []pm2Proc
}

type actionDoneMsg struct {
	err   string
	brief string
}

type Model struct {
	ctx *shared.AppContext

	width  int
	height int
	table  table.Model

	processes []pm2Proc

	busy   bool
	status string
	err    string
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string     { return "PM2" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.reloadCmd()
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case listLoadedMsg:
		m.busy = false
		m.err = msg.err
		m.status = msg.brief
		m.processes = msg.processes
		m.rebuildTable()
		return m, nil

	case actionDoneMsg:
		m.busy = false
		m.err = msg.err
		m.status = msg.brief
		if msg.err == "" {
			return m, m.reloadCmd()
		}
		return m, nil

	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "ctrl+r", "f5":
			m.status = "Refreshing…"
			return m, m.reloadCmd()
		case "r":
			return m, m.pm2Restart()
		case "t":
			return m, m.pm2Stop()
		case "d":
			return m, m.pm2Delete()
		case "f":
			return m, m.pm2Flush()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("PM2")
	body := m.table.View()

	msg := ""
	if m.err != "" {
		msg = "\n " + theme.ErrorText().Render(m.err)
	} else if m.status != "" {
		msg = "\n " + theme.MutedText().Render(m.status)
	}
	if m.busy {
		msg = "\n " + theme.MutedText().Render("Working…")
	}

	help := components.NewHelpBar(
		components.KeyBinding{Key: "r", Desc: "restart"},
		components.KeyBinding{Key: "t", Desc: "stop"},
		components.KeyBinding{Key: "d", Desc: "delete"},
		components.KeyBinding{Key: "f", Desc: "flush logs"},
		components.KeyBinding{Key: "ctrl+r", Desc: "refresh"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width

	return lipgloss.JoinVertical(lipgloss.Left, title, body, msg, help.View())
}

func (m *Model) rebuildTable() {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	cols := []table.Column{
		{Title: "ID", Width: 4},
		{Title: "Name", Width: 22},
		{Title: "Status", Width: 12},
		{Title: "CPU%", Width: 8},
		{Title: "Memory", Width: 12},
		{Title: "Restarts", Width: 10},
	}
	rows := make([]table.Row, len(m.processes))
	for i, p := range m.processes {
		rows[i] = table.Row{
			strconv.Itoa(p.PMID),
			p.Name,
			p.PM2Env.Status,
			fmtCPU(p.Monit.CPU),
			formatPM2Memory(p.Monit.Memory),
			strconv.Itoa(p.PM2Env.RestartTime),
		}
	}
	m.table = components.StyledTable(cols, rows, h)
}

func fmtCPU(v float64) string {
	return fmt.Sprintf("%.1f", v)
}

func formatPM2Memory(bytes float64) string {
	if bytes <= 0 {
		return "—"
	}
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GiB", bytes/gb)
	case bytes >= mb:
		return fmt.Sprintf("%.1f MiB", bytes/mb)
	case bytes >= kb:
		return fmt.Sprintf("%.1f KiB", bytes/kb)
	default:
		return fmt.Sprintf("%.0f B", bytes)
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return listLoadedMsg{err: "Not connected to a server (open from dashboard after connecting)."}
		}
		return m.loadList(ex)
	}
}

func (m *Model) loadList(ex *ssh.Executor) tea.Msg {
	r, err := ex.Run(`pm2 jlist`)
	if err != nil {
		return listLoadedMsg{err: err.Error()}
	}
	if r.ExitCode != 0 {
		out := strings.TrimSpace(r.Stderr)
		if out == "" {
			out = strings.TrimSpace(r.Stdout)
		}
		return listLoadedMsg{err: out}
	}
	raw := strings.TrimSpace(r.Stdout)
	var procs []pm2Proc
	if err := json.Unmarshal([]byte(raw), &procs); err != nil {
		return listLoadedMsg{err: fmt.Sprintf("pm2 jlist: %v", err)}
	}
	return listLoadedMsg{
		processes: procs,
		brief:     fmt.Sprintf("%d processes", len(procs)),
	}
}

func (m *Model) pm2Restart() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Restarting…"
	sid := m.ctx.ServerID
	id := strconv.Itoa(p.PMID)
	return func() tea.Msg {
		return m.runPM2(sid, fmt.Sprintf("pm2 restart %s", shellQuoteArg(id)))
	}
}

func (m *Model) pm2Stop() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Stopping…"
	sid := m.ctx.ServerID
	id := strconv.Itoa(p.PMID)
	return func() tea.Msg {
		return m.runPM2(sid, fmt.Sprintf("pm2 stop %s", shellQuoteArg(id)))
	}
}

func (m *Model) pm2Delete() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Deleting…"
	sid := m.ctx.ServerID
	id := strconv.Itoa(p.PMID)
	return func() tea.Msg {
		return m.runPM2(sid, fmt.Sprintf("pm2 delete %s", shellQuoteArg(id)))
	}
}

func (m *Model) pm2Flush() tea.Cmd {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Flushing logs…"
	sid := m.ctx.ServerID
	id := strconv.Itoa(p.PMID)
	return func() tea.Msg {
		return m.runPM2(sid, fmt.Sprintf("pm2 flush %s", shellQuoteArg(id)))
	}
}

func (m *Model) runPM2(serverID uint, cmd string) tea.Msg {
	ex, ok := m.ctx.Pool.GetExecutor(serverID)
	if !ok {
		return actionDoneMsg{err: "Not connected."}
	}
	res, err := ex.Run(cmd)
	if err != nil {
		return actionDoneMsg{err: err.Error()}
	}
	if res.ExitCode != 0 {
		out := strings.TrimSpace(res.Stderr)
		if out == "" {
			out = strings.TrimSpace(res.Stdout)
		}
		return actionDoneMsg{err: out}
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		out = "OK"
	}
	return actionDoneMsg{brief: out}
}

func (m *Model) selected() (pm2Proc, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.processes) {
		return pm2Proc{}, false
	}
	return m.processes[idx], true
}

func shellQuoteArg(s string) string {
	if s == "" {
		return "''"
	}
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}
