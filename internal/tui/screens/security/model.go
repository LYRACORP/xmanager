package security

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
	"gorm.io/gorm"
)

type tab int

const (
	tabOverview tab = iota
	tabFirewall
	tabSSH
	tabSSL
	tabEvents
	tabDump
)

type loadMsg struct {
	fw     security.FirewallStatus
	ssh    security.SSHConfig
	certs  []security.SSLCert
	f2b    security.Fail2banStatus
	fails  []security.AuthFail
	events []string
	dump   reqdump.Config
	err    error
}

type Model struct {
	ctx     *shared.AppContext
	tab     tab
	width   int
	height  int
	loading bool
	err     string
	fw      security.FirewallStatus
	ssh     security.SSHConfig
	certs   []security.SSLCert
	f2b     security.Fail2banStatus
	fails   []security.AuthFail
	events  []string
	dump    reqdump.Config
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx, tab: tabOverview}
}

func (m *Model) Name() string     { return "Security" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }
func (m *Model) OnNavigate(_ map[string]interface{}) {
	m.loading = true
}

func (m *Model) Init() tea.Cmd {
	return m.load()
}

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return loadMsg{err: fmt.Errorf("not connected")}
		}
		return buildLoadMsg(ex, m.ctx.DB, m.ctx.ServerID)
	}
}

func buildLoadMsg(ex *ssh.Executor, db *gorm.DB, serverID uint) loadMsg {
	fw := security.ReadFirewall(ex)
	sshCfg := security.SSHConfigRead(ex)
	certs, _, _ := security.SSLCerts(ex)
	f2b := security.ReadFail2ban(ex)
	fails := security.AuthFailures(ex, 40)
	var evLines []string
	if db != nil {
		rows, _ := securityevents.List(db, securityevents.Filter{ServerID: serverID, Limit: 30})
		for _, e := range rows {
			evLines = append(evLines, fmt.Sprintf("%s %s %s %s", e.CreatedAt.Format("01-02 15:04"), e.Kind, e.IP, truncate(e.Detail, 60)))
		}
	}
	dumpCfg := reqdump.LoadConfig(db, serverID)
	return loadMsg{fw: fw, ssh: sshCfg, certs: certs, f2b: f2b, fails: fails, events: evLines, dump: dumpCfg}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.fw = msg.fw
		m.ssh = msg.ssh
		m.certs = msg.certs
		m.f2b = msg.f2b
		m.fails = msg.fails
		m.events = msg.events
		m.dump = msg.dump
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "b":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			m.loading = true
			return m, m.load()
		case "1":
			m.tab = tabOverview
		case "2":
			m.tab = tabFirewall
		case "3":
			m.tab = tabSSH
		case "4":
			m.tab = tabSSL
		case "5":
			m.tab = tabEvents
		case "6":
			m.tab = tabDump
		case "h":
			if m.tab == tabSSH {
				return m, m.hardenSSH()
			}
		case "u":
			if m.tab == tabSSL {
				return m, m.renewSSL()
			}
		}
	}
	return m, nil
}

func (m *Model) hardenSSH() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return loadMsg{err: fmt.Errorf("not connected")}
		}
		if err := security.ApplySSHHardening(ex); err != nil {
			return loadMsg{err: err}
		}
		return buildLoadMsg(ex, m.ctx.DB, m.ctx.ServerID)
	}
}

func (m *Model) renewSSL() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return loadMsg{err: fmt.Errorf("not connected")}
		}
		_, err := security.RenewSSL(ex)
		if err != nil {
			return loadMsg{err: err}
		}
		return buildLoadMsg(ex, m.ctx.DB, m.ctx.ServerID)
	}
}

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "1-6", Desc: "tabs"},
		{Key: "h", Desc: "harden ssh (tab 3)"},
		{Key: "u", Desc: "renew ssl (tab 4)"},
		{Key: "r", Desc: "refresh"},
		{Key: "b", Desc: "back"},
	}
}

func (m *Model) View() string {
	header := theme.ScreenChrome("Security", "firewall · ssh · ssl · dumps", m.width)
	tabs := []string{"Overview", "Firewall", "SSH", "SSL", "Events", "Dump"}
	var tabBar strings.Builder
	for i, name := range tabs {
		if tab(i) == m.tab {
			tabBar.WriteString("[" + name + "] ")
		} else {
			tabBar.WriteString(name + " ")
		}
	}
	if m.loading {
		return header + "\n\n  Loading…\n"
	}
	if m.err != "" {
		return header + "\n\n  Error: " + m.err + "\n"
	}
	var body strings.Builder
	switch m.tab {
	case tabOverview:
		body.WriteString(fmt.Sprintf("  Firewall: %s (%s)\n", onOff(m.fw.Active), m.fw.Backend))
		body.WriteString(fmt.Sprintf("  SSH port %s · root %s · password %s\n", m.ssh.Port, m.ssh.PermitRootLogin, m.ssh.PasswordAuthentication))
		body.WriteString(fmt.Sprintf("  SSL certs: %d\n", len(m.certs)))
		body.WriteString(fmt.Sprintf("  fail2ban: %s\n", onOff(m.f2b.Installed)))
		body.WriteString(fmt.Sprintf("  Request dump: %s\n", onOff(m.dump.Enabled)))
	case tabFirewall:
		body.WriteString("  " + strings.ReplaceAll(truncate(m.fw.Raw, 2000), "\n", "\n  "))
	case tabSSH:
		body.WriteString(fmt.Sprintf("  Port %s PermitRootLogin=%s PasswordAuthentication=%s\n", m.ssh.Port, m.ssh.PermitRootLogin, m.ssh.PasswordAuthentication))
		body.WriteString("  Press h to apply hardening (PermitRootLogin no, PasswordAuthentication no)\n")
	case tabSSL:
		for _, c := range m.certs {
			body.WriteString(fmt.Sprintf("  %s — %s\n", c.Name, truncate(c.Expiry, 40)))
		}
		body.WriteString("  Press u to run certbot renew\n")
	case tabEvents:
		for _, f := range m.fails {
			body.WriteString(fmt.Sprintf("  ssh %s %s %s\n", f.Time, f.IP, truncate(f.Detail, 50)))
		}
		for _, e := range m.events {
			body.WriteString("  " + e + "\n")
		}
	case tabDump:
		body.WriteString(fmt.Sprintf("  Dump enabled: %s\n", onOff(m.dump.Enabled)))
		body.WriteString(fmt.Sprintf("  panel=%v nginx=%v honeypot=%v\n", m.dump.DumpPanel, m.dump.DumpNginx, m.dump.DumpHoneypot))
		body.WriteString("  Honeypot ports: " + m.dump.HoneypotPorts + "\n")
		body.WriteString("\n  Request dump listeners run in the node web panel process.\n")
		body.WriteString("  Toggle via web /security or /apps on the node panel host.\n")
	}
	return header + "\n" + tabBar.String() + "\n\n" + body.String()
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}
