package proxy

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type proxyTab int

const (
	tabNginx proxyTab = iota
	tabTraefik
)

type screenMode int

const (
	modeList screenMode = iota
	modeViewConfig
	modeAddVhost
	modeConfirmRemove
	modeRenewSSLWait
)

type nginxVhost struct {
	File        string
	Domain      string
	SSL         string
	ProxyPass   string
	FullPath    string
	RawSnippet  string
}

type traefikRouter struct {
	Name       string
	Rule       string
	Service    string
	SourceFile string
}

type proxyDataMsg struct {
	tab      proxyTab
	nginx    []nginxVhost
	traefik  []traefikRouter
	err      error
	warning  string
}

type sshCmdMsg struct {
	ok     bool
	output string
	err    error
}

type configViewMsg struct {
	title string
	body  string
	err   error
}

type Model struct {
	ctx       *shared.AppContext
	tab       proxyTab
	mode      screenMode
	width     int
	height    int
	message   string

	nginxRows   []nginxVhost
	traefikRows []traefikRouter

	table table.Model

	viewingTitle string
	viewingBody  string

	addDomain   textinput.Model
	addUpstream textinput.Model
	addIdx      int

	removeTarget string
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{ctx: ctx}
	m.addDomain = textinput.New()
	m.addDomain.Placeholder = "app.example.com"
	m.addDomain.Prompt = "Domain: "
	m.addDomain.Width = 48

	m.addUpstream = textinput.New()
	m.addUpstream.Placeholder = "http://127.0.0.1:3000"
	m.addUpstream.Prompt = "proxy_pass: "
	m.addUpstream.Width = 48

	return m
}

func (m *Model) Name() string     { return "Proxy" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.refreshCurrentTab
}

func (m *Model) refreshCurrentTab() tea.Msg {
	return m.loadTabData(m.tab)
}

func (m *Model) loadTabData(t proxyTab) tea.Msg {
	if m.ctx.ServerID == 0 {
		return proxyDataMsg{tab: t, err: fmt.Errorf("no server selected")}
	}
	ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
	if !ok {
		return proxyDataMsg{tab: t, err: fmt.Errorf("not connected — connect from server list first")}
	}
	switch t {
	case tabNginx:
		vhosts, warn, err := listNginxVhosts(ex)
		return proxyDataMsg{tab: t, nginx: vhosts, warning: warn, err: err}
	default:
		rtrs, warn, err := listTraefikRouters(ex)
		return proxyDataMsg{tab: t, traefik: rtrs, warning: warn, err: err}
	}
}

var (
	reServerName = regexp.MustCompile(`(?i)server_name\s+([^;]+);`)
	reProxyPass  = regexp.MustCompile(`(?i)proxy_pass\s+([^;]+);`)
	reSSLCert    = regexp.MustCompile(`(?i)ssl_certificate\s+`)
	reListenSSL  = regexp.MustCompile(`(?i)listen\s+[^;]*ssl`)
	safeConfName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

// shellQuote wraps s in single quotes for POSIX sh (handles embedded ').
func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

func runRemoteBash(ex *ssh.Executor, script string) (*ssh.ExecResult, error) {
	payload := base64.StdEncoding.EncodeToString([]byte(script))
	inner := "echo " + shellQuote(payload) + " | base64 -d | bash"
	return ex.Run("bash -lc " + shellQuote(inner))
}

func listNginxVhosts(ex *ssh.Executor) ([]nginxVhost, string, error) {
	const script = `d="/etc/nginx/sites-enabled"; if [ ! -d "$d" ]; then echo "NODIR"; exit 0; fi
for f in "$d"/*; do
  [ -e "$f" ] || continue
  [ -f "$f" ] || continue
  bn=$(basename "$f")
  echo "FILE:$bn"
  cat "$f" 2>/dev/null || true
  echo "ENDFILE"
done`
	res, err := runRemoteBash(ex, script)
	if err != nil {
		return nil, "", err
	}
	if strings.Contains(res.Stdout, "NODIR") && !strings.Contains(res.Stdout, "FILE:") {
		return nil, "/etc/nginx/sites-enabled not found or empty", nil
	}
	var warn string
	if res.ExitCode != 0 && res.Stderr != "" {
		warn = res.Stderr
	}
	return parseNginxDump(res.Stdout), warn, nil
}

func parseNginxDump(s string) []nginxVhost {
	var out []nginxVhost
	parts := strings.Split(s, "FILE:")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		end := strings.Index(part, "ENDFILE")
		if end < 0 {
			continue
		}
		head := part[:end]
		lines := strings.SplitN(head, "\n", 2)
		if len(lines) < 2 {
			continue
		}
		name := strings.TrimSpace(lines[0])
		if name == "" {
			continue
		}
		body := lines[1]
		domain := "-"
		if sm := reServerName.FindStringSubmatch(body); len(sm) > 1 {
			domain = strings.TrimSpace(strings.Fields(strings.ReplaceAll(sm[1], "\n", " "))[0])
			domain = strings.Trim(domain, `"'`)
		}
		pp := "-"
		if pm := reProxyPass.FindStringSubmatch(body); len(pm) > 1 {
			pp = strings.TrimSpace(strings.Fields(strings.ReplaceAll(pm[1], "\n", " "))[0])
			pp = strings.Trim(pp, `"'`)
		}
		ssl := "no"
		if reSSLCert.MatchString(body) || reListenSSL.MatchString(body) {
			ssl = "yes"
		}
		out = append(out, nginxVhost{
			File:       name,
			Domain:     domain,
			SSL:        ssl,
			ProxyPass:  pp,
			FullPath:   "/etc/nginx/sites-enabled/" + name,
			RawSnippet: body,
		})
	}
	return out
}

func listTraefikRouters(ex *ssh.Executor) ([]traefikRouter, string, error) {
	const script = `out=""
for d in /etc/traefik/dynamic /etc/traefik/conf/dynamic; do
  [ -d "$d" ] || continue
  for f in "$d"/*.yml "$d"/*.yaml; do
    [ -f "$f" ] || continue
    out=1
    echo "FILE:$f"
    cat "$f" 2>/dev/null || true
    echo "ENDFILE"
  done
done
for f in /etc/traefik/traefik.yml /etc/traefik/traefik.yaml; do
  [ -f "$f" ] || continue
  out=1
  echo "FILE:$f"
  cat "$f" 2>/dev/null || true
  echo "ENDFILE"
done
if [ -z "$out" ]; then echo "NOTFOUND"; fi`
	res, err := runRemoteBash(ex, script)
	if err != nil {
		return nil, "", err
	}
	if strings.Contains(res.Stdout, "NOTFOUND") {
		return nil, "no Traefik YAML found under /etc/traefik", nil
	}
	var warn string
	if res.Stderr != "" {
		warn = res.Stderr
	}
	return parseTraefikDump(res.Stdout), warn, nil
}

func leadingIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func parseTraefikDump(s string) []traefikRouter {
	var all []traefikRouter
	parts := strings.Split(s, "FILE:")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		end := strings.Index(part, "ENDFILE")
		if end < 0 {
			continue
		}
		head := part[:end]
		lines := strings.SplitN(head, "\n", 2)
		if len(lines) < 2 {
			continue
		}
		path := strings.TrimSpace(lines[0])
		content := lines[1]
		all = append(all, extractRoutersFromYAML(content, path)...)
	}
	return all
}

func extractRoutersFromYAML(content, sourceFile string) []traefikRouter {
	var rows []traefikRouter
	lines := strings.Split(content, "\n")
	inRouters := false
	routersIndent := -1
	var currentName string

	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		ind := leadingIndent(line)
		if strings.HasSuffix(t, "routers:") {
			inRouters = true
			routersIndent = ind
			currentName = ""
			continue
		}
		if inRouters && routersIndent >= 0 && ind <= routersIndent && !strings.HasPrefix(t, "routers:") {
			inRouters = false
			routersIndent = -1
			currentName = ""
		}
		if !inRouters {
			continue
		}
		childIndent := routersIndent + 2
		if ind == childIndent && strings.HasSuffix(t, ":") && !strings.Contains(strings.TrimSuffix(t, ":"), " ") {
			name := strings.TrimSuffix(t, ":")
			if name != "tls" && name != "middlewares" {
				currentName = name
			}
			continue
		}
		if strings.HasPrefix(t, "rule:") {
			rule := strings.TrimSpace(strings.TrimPrefix(t, "rule:"))
			rule = strings.Trim(rule, `"'`)
			rows = append(rows, traefikRouter{Name: currentName, Rule: rule, SourceFile: sourceFile})
			continue
		}
		if strings.HasPrefix(t, "service:") && len(rows) > 0 && rows[len(rows)-1].SourceFile == sourceFile {
			svc := strings.TrimSpace(strings.TrimPrefix(t, "service:"))
			svc = strings.Trim(svc, `"'`)
			rows[len(rows)-1].Service = svc
		}
	}
	return rows
}

func (m *Model) rebuildTable() {
	h := m.height - 10
	if h < 5 {
		h = 5
	}
	switch m.tab {
	case tabNginx:
		cols := []table.Column{
			{Title: "File", Width: 18},
			{Title: "Domain", Width: 22},
			{Title: "SSL", Width: 5},
			{Title: "proxy_pass", Width: 28},
		}
		rows := make([]table.Row, len(m.nginxRows))
		for i, v := range m.nginxRows {
			rows[i] = table.Row{v.File, v.Domain, v.SSL, v.ProxyPass}
		}
		m.table = components.StyledTable(cols, rows, h)
	case tabTraefik:
		cols := []table.Column{
			{Title: "Router", Width: 16},
			{Title: "Rule", Width: 28},
			{Title: "Service", Width: 14},
			{Title: "Config", Width: 24},
		}
		rows := make([]table.Row, len(m.traefikRows))
		for i, r := range m.traefikRows {
			cfg := r.SourceFile
			if len(cfg) > 24 {
				cfg = "…" + cfg[len(cfg)-23:]
			}
			rows[i] = table.Row{r.Name, r.Rule, r.Service, cfg}
		}
		m.table = components.StyledTable(cols, rows, h)
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case proxyDataMsg:
		m.message = ""
		if msg.err != nil {
			m.message = msg.err.Error()
		} else if msg.warning != "" {
			m.message = msg.warning
		}
		if msg.tab == tabNginx {
			m.nginxRows = msg.nginx
		} else {
			m.traefikRows = msg.traefik
		}
		m.rebuildTable()
		return m, nil

	case sshCmdMsg:
		m.mode = modeList
		if msg.err != nil {
			m.message = msg.err.Error()
		} else if !msg.ok {
			m.message = msg.output
		} else {
			m.message = strings.TrimSpace(msg.output)
			if m.message == "" {
				m.message = "Done."
			}
		}
		return m, nil

	case configViewMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			m.mode = modeList
			return m, nil
		}
		m.viewingTitle = msg.title
		m.viewingBody = msg.body
		m.mode = modeViewConfig
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeViewConfig:
			if msg.String() == "esc" || msg.String() == "enter" {
				m.mode = modeList
				return m, nil
			}
			return m, nil
		case modeConfirmRemove:
			switch msg.String() {
			case "y", "Y":
				return m, m.runRemoveNginx()
			case "n", "N", "esc":
				m.mode = modeList
				return m, nil
			}
			return m, nil
		case modeAddVhost:
			return m.updateAddForm(msg)
		case modeRenewSSLWait:
			if msg.String() == "esc" {
				m.mode = modeList
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "1":
			m.tab = tabNginx
			return m, m.refreshCurrentTab
		case "2":
			m.tab = tabTraefik
			return m, m.refreshCurrentTab
		case "r":
			return m, m.refreshCurrentTab
		case "v":
			return m, m.openViewConfig()
		case "a":
			if m.tab == tabNginx {
				m.mode = modeAddVhost
				m.addIdx = 0
				m.addDomain.Blur()
				m.addUpstream.Blur()
				m.addDomain.Focus()
			}
			return m, textinput.Blink
		case "x":
			if m.tab != tabNginx {
				return m, nil
			}
			if idx := m.table.Cursor(); idx >= 0 && idx < len(m.nginxRows) {
				m.mode = modeConfirmRemove
				m.removeTarget = m.nginxRows[idx].FullPath
			}
			return m, nil
		case "s":
			if m.tab != tabNginx {
				return m, nil
			}
			m.mode = modeRenewSSLWait
			return m, m.runRenewSSL()
		}
	}

	if m.mode == modeList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) openViewConfig() tea.Cmd {
	if m.tab == tabNginx {
		idx := m.table.Cursor()
		if idx < 0 || idx >= len(m.nginxRows) {
			return nil
		}
		v := m.nginxRows[idx]
		m.mode = modeViewConfig
		m.viewingTitle = v.FullPath
		m.viewingBody = trimForView(v.RawSnippet, m.height-12)
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.traefikRows) {
		return nil
	}
	r := m.traefikRows[idx]
	return m.loadFilePreview(r.SourceFile)
}

func (m *Model) loadFilePreview(path string) tea.Cmd {
	h := m.height
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return configViewMsg{err: fmt.Errorf("not connected")}
		}
		res, err := ex.Run("sh -c " + shellQuote("cat "+path+" 2>/dev/null"))
		if err != nil {
			return configViewMsg{err: err}
		}
		return configViewMsg{title: path, body: trimForView(res.Stdout, h-12)}
	}
}

func trimForView(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if maxLines < 8 {
		maxLines = 8
	}
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n…"
}

func (m *Model) updateAddForm(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab", "down":
		m.addDomain.Blur()
		m.addUpstream.Blur()
		m.addIdx = 1 - m.addIdx
		if m.addIdx == 0 {
			m.addDomain.Focus()
		} else {
			m.addUpstream.Focus()
		}
		return m, textinput.Blink
	case "shift+tab", "up":
		m.addDomain.Blur()
		m.addUpstream.Blur()
		m.addIdx = 1 - m.addIdx
		if m.addIdx == 0 {
			m.addDomain.Focus()
		} else {
			m.addUpstream.Focus()
		}
		return m, textinput.Blink
	case "enter":
		if m.addIdx == 0 {
			m.addDomain.Blur()
			m.addIdx = 1
			m.addUpstream.Focus()
			return m, textinput.Blink
		}
		return m, m.runAddNginx()
	}
	var cmd tea.Cmd
	if m.addIdx == 0 {
		m.addDomain, cmd = m.addDomain.Update(msg)
	} else {
		m.addUpstream, cmd = m.addUpstream.Update(msg)
	}
	return m, cmd
}

func (m *Model) runAddNginx() tea.Cmd {
	domain := strings.TrimSpace(m.addDomain.Value())
	up := strings.TrimSpace(m.addUpstream.Value())
	if domain == "" || up == "" {
		return func() tea.Msg {
			return sshCmdMsg{err: fmt.Errorf("domain and proxy_pass are required")}
		}
	}
	base := safeConfName.ReplaceAllString(domain, "_")
	if base == "" {
		base = "vhost"
	}
	filename := base + ".conf"
	block := fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    location / {
        proxy_pass %s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`, domain, up)

	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return sshCmdMsg{err: fmt.Errorf("not connected")}
		}
		cmd := "sudo tee /etc/nginx/sites-enabled/" + filename + " > /dev/null <<'EOF'\n" +
			block + "\nEOF\nsudo nginx -t 2>&1"
		res, err := ex.Run(cmd)
		if err != nil {
			return sshCmdMsg{err: err}
		}
		okTest := res.ExitCode == 0 && !strings.Contains(strings.ToLower(res.Stdout+res.Stderr), "emerg")
		return sshCmdMsg{ok: okTest, output: res.Stdout + res.Stderr}
	}
}

func (m *Model) runRemoveNginx() tea.Cmd {
	target := m.removeTarget
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return sshCmdMsg{err: fmt.Errorf("not connected")}
		}
		res, err := ex.Run("sudo rm -f " + shellQuote(target))
		if err != nil {
			return sshCmdMsg{err: err}
		}
		return sshCmdMsg{ok: res.ExitCode == 0, output: res.Stdout + res.Stderr}
	}
}

func (m *Model) runRenewSSL() tea.Cmd {
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
		if !ok {
			return sshCmdMsg{err: fmt.Errorf("not connected")}
		}
		res, err := ex.Run("sudo certbot renew --no-random-sleep-on-renew 2>&1 || sudo certbot renew 2>&1")
		if err != nil {
			return sshCmdMsg{err: err}
		}
		return sshCmdMsg{ok: res.ExitCode == 0, output: res.Stdout + res.Stderr}
	}
}

func (m *Model) View() string {
	switch m.mode {
	case modeViewConfig:
		title := theme.HeaderStyle().Render("Config: " + m.viewingTitle)
		body := theme.PanelStyle().Width(m.width - 2).Render(m.viewingBody)
		foot := theme.MutedText().Render("  Esc / Enter: close")
		return lipgloss.JoinVertical(lipgloss.Left, title, body, foot)
	case modeAddVhost:
		header := theme.HeaderStyle().Render("Add Nginx vhost")
		form := m.addDomain.View() + "\n" + m.addUpstream.View()
		foot := theme.MutedText().Render("  Tab: field  Enter: next / save  Esc: cancel")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", form, "", foot)
	case modeConfirmRemove:
		return m.viewList() + "\n\n " + theme.WarningText().Render("Remove vhost file? (y/n)")
	case modeRenewSSLWait:
		return m.viewList() + "\n\n " + theme.InfoBadge().Render("Running certbot renew…")
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	tabBar := m.renderTabs()
	title := theme.HeaderStyle().Render("Reverse proxy")
	help := components.NewHelpBar(
		components.KeyBinding{Key: "1/2", Desc: "nginx/traefik"},
		components.KeyBinding{Key: "r", Desc: "refresh"},
		components.KeyBinding{Key: "v", Desc: "view config"},
		components.KeyBinding{Key: "a", Desc: "add vhost"},
		components.KeyBinding{Key: "x", Desc: "remove vhost"},
		components.KeyBinding{Key: "s", Desc: "renew SSL"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width
	msg := ""
	if m.message != "" {
		msg = "\n " + m.message
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, tabBar, m.table.View(), msg, help.View())
}

func (m *Model) renderTabs() string {
	nginx := theme.MutedText().Render("[1] Nginx")
	traf := theme.MutedText().Render("[2] Traefik")
	if m.tab == tabNginx {
		nginx = theme.TitleStyle().Render("[1] Nginx")
	} else {
		traf = theme.TitleStyle().Render("[2] Traefik")
	}
	return lipgloss.NewStyle().PaddingLeft(1).Render(nginx + "    " + traf)
}
