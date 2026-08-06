package recon

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type portResult struct {
	port   int
	open   bool
	banner string
}

type scanDoneMsg struct {
	host    string
	results []portResult
	pingMs  int64
	err     error
}

type mode int

const (
	modeForm mode = iota
	modeResults
)

// Model provides a port-scan / ping UI for any host.
type Model struct {
	ctx     *shared.AppContext
	mode    mode
	hostIn  textinput.Model
	portsIn textinput.Model
	formIdx int
	results []portResult
	pingMs  int64
	scanHost string
	scanning bool
	err     string
	message string
	width   int
	height  int
}

func New(ctx *shared.AppContext) *Model {
	hostIn := textinput.New()
	hostIn.Placeholder = "192.168.1.1  or  example.com"
	hostIn.Prompt = "Host: "
	hostIn.Width = 40

	portsIn := textinput.New()
	portsIn.Placeholder = "22,80,443,3306,8080"
	portsIn.Prompt = "Ports: "
	portsIn.Width = 40
	portsIn.SetValue("22,80,443,3306,5432,6379,8080,8443")

	return &Model{ctx: ctx, hostIn: hostIn, portsIn: portsIn}
}

func (m *Model) Name() string     { return "Recon" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeResults:
		return []components.KeyBinding{
			{Key: "n", Desc: "new scan"},
			{Key: "b/esc", Desc: "back"},
		}
	default:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next field"},
			{Key: "enter", Desc: "scan"},
			{Key: "b/esc", Desc: "back"},
		}
	}
}

func (m *Model) Init() tea.Cmd {
	m.formIdx = 0
	m.hostIn.Focus()
	return nil
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case scanDoneMsg:
		m.scanning = false
		if msg.err != nil {
			m.err = msg.err.Error()
		} else {
			m.err = ""
			m.results = msg.results
			m.pingMs = msg.pingMs
			m.scanHost = msg.host
			m.mode = modeResults
		}
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeForm:
			return m.updateForm(msg)
		case modeResults:
			switch msg.String() {
			case "n":
				m.mode = modeForm
				m.formIdx = 0
				m.hostIn.Focus()
				return m, nil
			case "b", "esc":
				return m, func() tea.Msg { return shared.GoBackMsg{} }
			}
		}
	}
	return m, nil
}

func (m *Model) updateForm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	inputs := []*textinput.Model{&m.hostIn, &m.portsIn}
	switch msg.String() {
	case "esc", "b":
		return m, func() tea.Msg { return shared.GoBackMsg{} }
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
		host := m.hostIn.Value()
		ports := m.portsIn.Value()
		if host == "" {
			m.err = "host is required"
			return m, nil
		}
		m.scanning = true
		m.err = ""
		m.message = fmt.Sprintf("Scanning %s…", host)
		return m, doScan(host, ports)
	}
	var cmd tea.Cmd
	*inputs[m.formIdx], cmd = inputs[m.formIdx].Update(msg)
	return m, cmd
}

func doScan(host, portsStr string) tea.Cmd {
	return func() tea.Msg {
		// Ping via TCP connect to port 80 as a proxy for latency.
		start := time.Now()
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, "80"), 3*time.Second)
		pingMs := time.Since(start).Milliseconds()
		if err == nil {
			_ = c.Close()
		} else {
			pingMs = -1
		}

		var results []portResult
		for _, p := range parsePorts(portsStr) {
			addr := net.JoinHostPort(host, fmt.Sprintf("%d", p))
			t := time.Now()
			conn, connErr := net.DialTimeout("tcp", addr, 2*time.Second)
			_ = t
			pr := portResult{port: p, open: connErr == nil}
			if connErr == nil {
				conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				buf := make([]byte, 64)
				n, _ := conn.Read(buf)
				if n > 0 {
					pr.banner = strings.TrimSpace(string(buf[:n]))
				}
				conn.Close()
			}
			results = append(results, pr)
		}
		return scanDoneMsg{host: host, results: results, pingMs: pingMs}
	}
}

func parsePorts(s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		var p int
		if _, err := fmt.Sscanf(part, "%d", &p); err == nil && p > 0 && p < 65536 {
			out = append(out, p)
		}
	}
	return out
}

func (m *Model) View() string {
	switch m.mode {
	case modeResults:
		return m.viewResults()
	default:
		return m.viewForm()
	}
}

func (m *Model) viewForm() string {
	header := theme.ScreenChrome("Recon", "port scan & ping", m.width)
	var b strings.Builder
	inputs := []textinput.Model{m.hostIn, m.portsIn}
	for i, ti := range inputs {
		styled := components.ApplyInputTheme(ti, m.width, i == m.formIdx)
		b.WriteString(components.RenderFormField("", styled.View(), m.width, i == m.formIdx))
		b.WriteByte('\n')
	}
	var extra string
	if m.scanning {
		extra = "\n " + theme.MutedText().Render(m.message)
	}
	if m.err != "" {
		extra = "\n " + theme.ErrorText().Render("Error: "+m.err)
	}
	footer := theme.MutedText().Render("  Tab: next  Enter: scan  Esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), extra, footer)
}

func (m *Model) viewResults() string {
	header := theme.ScreenChrome("Recon Results", m.scanHost, m.width)

	pingStr := fmt.Sprintf("%dms", m.pingMs)
	if m.pingMs < 0 {
		pingStr = "unreachable (port 80)"
	}
	summary := fmt.Sprintf("  ping (tcp:80): %s", pingStr)

	var rows []string
	rows = append(rows, summary, "")

	openStyle := theme.SuccessText()
	closedStyle := theme.MutedText()
	for _, r := range m.results {
		portStr := fmt.Sprintf("  %-6d", r.port)
		if r.open {
			status := openStyle.Render("OPEN")
			banner := ""
			if r.banner != "" {
				banner = "  " + theme.MutedText().Render(truncate(r.banner, 40))
			}
			rows = append(rows, portStr+status+banner)
		} else {
			rows = append(rows, closedStyle.Render(portStr+"closed"))
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	footer := theme.MutedText().Render("  n: new scan  esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
