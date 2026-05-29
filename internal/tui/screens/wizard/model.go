package wizard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type wizMode int

const (
	wizBrowse wizMode = iota
	wizConfirm
	wizOutput
)

type stepDef struct {
	title string
	cmds  []string
}

type execDoneMsg struct {
	stepIdx int
	output  string
	ok      bool
}

var wizardSteps = []stepDef{
	{
		title: "System Update",
		cmds: []string{
			"DEBIAN_FRONTEND=noninteractive apt-get update -y && apt-get upgrade -y",
		},
	},
	{
		title: "Install Docker",
		cmds: []string{
			"apt-get install -y ca-certificates curl gnupg",
			"install -m 0755 -d /etc/apt/keyrings",
			"curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg",
			`echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list`,
			"apt-get update -y && apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		},
	},
	{
		title: "Install Nginx",
		cmds: []string{
			"apt-get install -y nginx",
			"systemctl enable nginx",
		},
	},
	{
		title: "Configure Firewall (UFW)",
		cmds: []string{
			"apt-get install -y ufw",
			"ufw allow OpenSSH",
			"ufw allow 'Nginx Full'",
			"ufw --force enable",
		},
	},
	{
		title: "SSH Hardening",
		cmds: []string{
			"sed -i 's/^#\\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config",
			"grep -q '^PasswordAuthentication' /etc/ssh/sshd_config && sed -i 's/^PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config || echo 'PasswordAuthentication no' >> /etc/ssh/sshd_config",
			"systemctl reload sshd || systemctl reload ssh",
		},
	},
	{
		title: "Setup Swap",
		cmds: []string{
			"fallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048",
			"chmod 600 /swapfile",
			"mkswap /swapfile",
			"swapon /swapfile",
			"grep -q '/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab",
		},
	},
}

type Model struct {
	ctx       *shared.AppContext
	mode      wizMode
	stepIdx   int
	completed []bool
	width     int
	height    int
	lastOut   string
	lastOK    bool
}

func New(ctx *shared.AppContext) *Model {
	return &Model{
		ctx:       ctx,
		completed: make([]bool, len(wizardSteps)),
	}
}

func (m *Model) Name() string { return "Setup Wizard" }

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case wizConfirm:
		return []components.KeyBinding{
			{Key: "y", Desc: "run"},
			{Key: "n/esc", Desc: "cancel"},
		}
	case wizOutput:
		return []components.KeyBinding{
			{Key: "enter/esc", Desc: "return"},
		}
	default:
		return []components.KeyBinding{
			{Key: "↑↓", Desc: "step"},
			{Key: "enter", Desc: "review & run"},
		}
	}
}

func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case execDoneMsg:
		m.lastOut = msg.output
		m.lastOK = msg.ok
		if msg.ok {
			m.completed[msg.stepIdx] = true
		}
		m.mode = wizOutput
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch m.mode {
	case wizOutput:
		switch msg.String() {
		case "esc", "enter", " ":
			m.mode = wizBrowse
			return m, nil
		}
		return m, nil
	case wizConfirm:
		switch msg.String() {
		case "y", "Y":
			m.mode = wizBrowse
			return m, m.runStep(m.stepIdx)
		case "n", "N", "esc":
			m.mode = wizBrowse
			return m, nil
		}
		return m, nil
	default:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "up", "k":
			if m.stepIdx > 0 {
				m.stepIdx--
			}
			return m, nil
		case "down", "j":
			if m.stepIdx < len(wizardSteps)-1 {
				m.stepIdx++
			}
			return m, nil
		case "enter":
			if m.ctx.ServerID == 0 {
				return m, nil
			}
			if m.completed[m.stepIdx] {
				return m, nil
			}
			m.mode = wizConfirm
			return m, nil
		}
		return m, nil
	}
}

func (m *Model) runStep(idx int) tea.Cmd {
	return func() tea.Msg {
		exec, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return execDoneMsg{stepIdx: idx, output: "No SSH session: connect to a server first.", ok: false}
		}
		var b strings.Builder
		allOK := true
		for _, line := range wizardSteps[idx].cmds {
			r, err := exec.Run(line)
			if err != nil {
				fmt.Fprintf(&b, "$ %s\nerr: %v\n\n", line, err)
				allOK = false
				break
			}
			fmt.Fprintf(&b, "$ %s\n", line)
			if r.Stdout != "" {
				b.WriteString(r.Stdout)
				b.WriteByte('\n')
			}
			if r.Stderr != "" {
				b.WriteString(r.Stderr)
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "exit: %d\n\n", r.ExitCode)
			if r.ExitCode != 0 {
				allOK = false
				break
			}
		}
		return execDoneMsg{stepIdx: idx, output: strings.TrimSpace(b.String()), ok: allOK}
	}
}

func (m *Model) frame(subtitle, body string) string {
	return components.ScreenFrame{
		Title:    "Setup Wizard",
		Subtitle: subtitle,
		Width:    m.width,
		Body:     body,
	}.View()
}

func (m *Model) View() string {
	if m.ctx.ServerID == 0 {
		return m.frame(m.progressLine(), theme.WarningText().Render("Connect to a server first."))
	}

	switch m.mode {
	case wizOutput:
		return m.viewOutput()
	case wizConfirm:
		return m.viewConfirm()
	default:
		return m.viewBrowse()
	}
}

func (m *Model) progressLine() string {
	done := 0
	for _, c := range m.completed {
		if c {
			done++
		}
	}
	total := len(wizardSteps)
	if total == 0 {
		return "0/0 steps"
	}
	pct := float64(done) / float64(total)
	g := components.NewGauge("PROG", pct)
	g.Width = layout.Clamp(20, m.width/3, 40)
	g.ShowPct = true
	return g.View() + theme.MutedText().Render(fmt.Sprintf("  %d/%d steps", done, total))
}

func (m *Model) viewBrowse() string {
	var lines []string
	for i, s := range wizardSteps {
		mark := theme.MutedText().Render("○")
		if m.completed[i] {
			mark = theme.SuccessText().Render("✓")
		}
		line := fmt.Sprintf("  %s  %s", mark, s.title)
		if i == m.stepIdx {
			line = theme.KeyStyle().Render("> ") + strings.TrimPrefix(line, "  ")
		}
		lines = append(lines, line)
	}
	return m.frame(m.progressLine(), strings.Join(lines, "\n"))
}

func (m *Model) viewConfirm() string {
	s := wizardSteps[m.stepIdx]
	var cmdBlock strings.Builder
	for _, c := range s.cmds {
		cmdBlock.WriteString(theme.MutedText().Render("  "))
		cmdBlock.WriteString(c)
		cmdBlock.WriteByte('\n')
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		theme.TitleStyle().Render(s.title),
		"",
		cmdBlock.String(),
		"",
		theme.WarningText().Render("Run these commands on the remote host?"),
		theme.MutedText().Render("  y: run  n/esc: cancel"),
	)
	return m.frame(m.progressLine(), body)
}

func (m *Model) viewOutput() string {
	status := theme.ErrorText().Render("Failed")
	if m.lastOK {
		status = theme.SuccessText().Render("Completed")
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		status,
		"",
		m.lastOut,
		"",
		theme.MutedText().Render("  Enter / Esc: return"),
	)
	return m.frame(m.progressLine(), body)
}

var _ shared.Screen = (*Model)(nil)
