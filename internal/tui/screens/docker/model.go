package docker

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type viewTab int

const (
	tabContainers viewTab = iota
	tabCompose
	tabImages
)

type containerRow struct {
	ID     string
	Names  string
	State  string
	Status string
	Image  string
}

type composeProject struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

type imageRow struct {
	Repository string
	Tag        string
	ID         string
	Size       string
}

type loadDoneMsg struct {
	tab   viewTab
	err   string
	brief string

	containers []containerRow
	compose    []composeProject
	images     []imageRow
}

type actionDoneMsg struct {
	err   string
	brief string
}

type Model struct {
	ctx *shared.AppContext

	tab   viewTab
	width int
	height int

	table table.Model

	containers []containerRow
	compose    []composeProject
	images     []imageRow

	busy   bool
	status string
	err    string
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx, tab: tabContainers}
}

func (m *Model) Name() string     { return "Docker" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.reloadCmd()
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadDoneMsg:
		m.busy = false
		if msg.tab != m.tab {
			return m, nil
		}
		m.err = msg.err
		m.status = msg.brief
		if msg.err == "" {
			switch msg.tab {
			case tabContainers:
				m.containers = msg.containers
			case tabCompose:
				m.compose = msg.compose
			case tabImages:
				m.images = msg.images
			}
		}
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
		case "tab":
			m.nextTab()
			return m, m.reloadCmd()
		case "shift+tab":
			m.prevTab()
			return m, m.reloadCmd()
		case "1":
			m.setTab(tabContainers)
			return m, m.reloadCmd()
		case "2":
			m.setTab(tabCompose)
			return m, m.reloadCmd()
		case "3":
			m.setTab(tabImages)
			return m, m.reloadCmd()
		case "ctrl+r", "f5":
			m.status = "Refreshing…"
			return m, m.reloadCmd()
		case "s":
			return m, m.dockerStart()
		case "t":
			return m, m.dockerStop()
		case "r":
			return m, m.dockerRestart()
		case "u":
			return m, m.composeUp()
		case "d":
			return m, m.composeDown()
		case "c":
			return m, m.imagePrune()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Docker")
	tabs := m.renderTabs()
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

	help := components.NewHelpBar(m.helpBindings()...)
	help.Width = m.width

	return lipgloss.JoinVertical(lipgloss.Left, title, tabs, body, msg, help.View())
}

func (m *Model) renderTabs() string {
	names := []string{"Containers", "Compose", "Images"}
	var parts []string
	for i, n := range names {
		st := theme.SubtitleStyle()
		if viewTab(i) == m.tab {
			st = theme.TitleStyle().Underline(true)
		}
		label := fmt.Sprintf("%d:%s", i+1, n)
		parts = append(parts, st.Render(label))
	}
	return lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(parts, "  "))
}

func (m *Model) helpBindings() []components.KeyBinding {
	base := []components.KeyBinding{
		{Key: "tab", Desc: "next view"},
		{Key: "1-3", Desc: "view"},
		{Key: "ctrl+r", Desc: "refresh"},
		{Key: "esc", Desc: "back"},
	}
	switch m.tab {
	case tabContainers:
		return append([]components.KeyBinding{
			{Key: "s", Desc: "start"},
			{Key: "t", Desc: "stop"},
			{Key: "r", Desc: "restart"},
		}, base...)
	case tabCompose:
		return append([]components.KeyBinding{
			{Key: "u", Desc: "up"},
			{Key: "d", Desc: "down"},
		}, base...)
	case tabImages:
		return append([]components.KeyBinding{
			{Key: "c", Desc: "prune dangling"},
		}, base...)
	}
	return base
}

func (m *Model) nextTab() {
	m.tab = (m.tab + 1) % 3
	m.rebuildTable()
}

func (m *Model) prevTab() {
	m.tab = (m.tab + 2) % 3
	m.rebuildTable()
}

func (m *Model) setTab(t viewTab) {
	m.tab = t
	m.rebuildTable()
}

func (m *Model) rebuildTable() {
	h := m.height - 10
	if h < 5 {
		h = 5
	}

	switch m.tab {
	case tabContainers:
		cols := []table.Column{
			{Title: "ID", Width: 14},
			{Title: "Names", Width: 22},
			{Title: "State", Width: 10},
			{Title: "Status", Width: 28},
			{Title: "Image", Width: 28},
		}
		rows := make([]table.Row, len(m.containers))
		for i, c := range m.containers {
			id := c.ID
			if len(id) > 12 {
				id = id[:12]
			}
			rows[i] = table.Row{id, c.Names, c.State, c.Status, c.Image}
		}
		m.table = components.StyledTable(cols, rows, h)

	case tabCompose:
		cols := []table.Column{
			{Title: "Project", Width: 20},
			{Title: "Status", Width: 22},
			{Title: "Config", Width: 48},
		}
		rows := make([]table.Row, len(m.compose))
		for i, p := range m.compose {
			rows[i] = table.Row{p.Name, p.Status, truncate(p.ConfigFiles, 46)}
		}
		m.table = components.StyledTable(cols, rows, h)

	case tabImages:
		cols := []table.Column{
			{Title: "Repository", Width: 28},
			{Title: "Tag", Width: 14},
			{Title: "ID", Width: 14},
			{Title: "Size", Width: 10},
		}
		rows := make([]table.Row, len(m.images))
		for i, im := range m.images {
			rows[i] = table.Row{im.Repository, im.Tag, shortID(im.ID, 12), im.Size}
		}
		m.table = components.StyledTable(cols, rows, h)
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	tab := m.tab
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return loadDoneMsg{tab: tab, err: "Not connected to a server (open from dashboard after connecting)."}
		}
		switch tab {
		case tabContainers:
			return m.loadContainers(ex, tab)
		case tabCompose:
			return m.loadCompose(ex, tab)
		case tabImages:
			return m.loadImages(ex, tab)
		}
		return loadDoneMsg{tab: tab}
	}
}

func (m *Model) loadContainers(ex *ssh.Executor, tab viewTab) tea.Msg {
	r, err := ex.Run(`docker ps -a --no-trunc --format '{{.ID}}	{{.Names}}	{{.State}}	{{.Status}}	{{.Image}}'`)
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	if r.ExitCode != 0 {
		out := strings.TrimSpace(r.Stderr)
		if out == "" {
			out = strings.TrimSpace(r.Stdout)
		}
		return loadDoneMsg{tab: tab, err: out}
	}
	var rows []containerRow
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		rows = append(rows, containerRow{
			ID: parts[0], Names: parts[1], State: parts[2], Status: parts[3], Image: parts[4],
		})
	}
	return loadDoneMsg{tab: tab, containers: rows, brief: fmt.Sprintf("%d containers", len(rows))}
}

func (m *Model) loadCompose(ex *ssh.Executor, tab viewTab) tea.Msg {
	r, err := ex.Run(`docker compose ls --format json`)
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	if r.ExitCode != 0 {
		r2, err2 := ex.Run(`docker-compose ls --format json`)
		if err2 != nil {
			out := strings.TrimSpace(r.Stderr)
			if out == "" {
				out = strings.TrimSpace(r.Stdout)
			}
			return loadDoneMsg{tab: tab, err: out}
		}
		r = r2
		if r.ExitCode != 0 {
			out := strings.TrimSpace(r.Stderr)
			if out == "" {
				out = strings.TrimSpace(r.Stdout)
			}
			return loadDoneMsg{tab: tab, err: out}
		}
	}
	projects, perr := parseComposeLSJSON(r.Stdout)
	if perr != nil {
		return loadDoneMsg{tab: tab, err: perr.Error()}
	}
	return loadDoneMsg{tab: tab, compose: projects, brief: fmt.Sprintf("%d stacks", len(projects))}
}

func parseComposeLSJSON(raw string) ([]composeProject, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var arr []composeProject
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		return arr, nil
	}
	var one composeProject
	if err := json.Unmarshal([]byte(raw), &one); err == nil && one.Name != "" {
		return []composeProject{one}, nil
	}
	var out []composeProject
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p composeProject
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("compose ls JSON: %w", err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("compose ls JSON: could not parse output")
	}
	return out, nil
}

func (m *Model) loadImages(ex *ssh.Executor, tab viewTab) tea.Msg {
	r, err := ex.Run(`docker image ls --format '{{.Repository}}	{{.Tag}}	{{.ID}}	{{.Size}}'`)
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	if r.ExitCode != 0 {
		out := strings.TrimSpace(r.Stderr)
		if out == "" {
			out = strings.TrimSpace(r.Stdout)
		}
		return loadDoneMsg{tab: tab, err: out}
	}
	var rows []imageRow
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		rows = append(rows, imageRow{
			Repository: parts[0], Tag: parts[1], ID: parts[2], Size: parts[3],
		})
	}
	return loadDoneMsg{tab: tab, images: rows, brief: fmt.Sprintf("%d images", len(rows))}
}

func (m *Model) dockerStart() tea.Cmd {
	if m.tab != tabContainers {
		return nil
	}
	id := m.selectedContainerID()
	if id == "" {
		return nil
	}
	m.busy = true
	m.status = "Starting container…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, fmt.Sprintf("docker start %s", shellQuoteArg(id)))
	}
}

func (m *Model) dockerStop() tea.Cmd {
	if m.tab != tabContainers {
		return nil
	}
	id := m.selectedContainerID()
	if id == "" {
		return nil
	}
	m.busy = true
	m.status = "Stopping container…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, fmt.Sprintf("docker stop %s", shellQuoteArg(id)))
	}
}

func (m *Model) dockerRestart() tea.Cmd {
	if m.tab != tabContainers {
		return nil
	}
	id := m.selectedContainerID()
	if id == "" {
		return nil
	}
	m.busy = true
	m.status = "Restarting container…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, fmt.Sprintf("docker restart %s", shellQuoteArg(id)))
	}
}

func (m *Model) composeUp() tea.Cmd {
	if m.tab != tabCompose {
		return nil
	}
	p, ok := m.selectedCompose()
	if !ok {
		return nil
	}
	cmd, err := composeCommand(p, "up -d")
	if err != nil {
		return func() tea.Msg { return actionDoneMsg{err: err.Error()} }
	}
	m.busy = true
	m.status = "Compose up…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, cmd)
	}
}

func (m *Model) composeDown() tea.Cmd {
	if m.tab != tabCompose {
		return nil
	}
	p, ok := m.selectedCompose()
	if !ok {
		return nil
	}
	cmd, err := composeCommand(p, "down")
	if err != nil {
		return func() tea.Msg { return actionDoneMsg{err: err.Error()} }
	}
	m.busy = true
	m.status = "Compose down…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, cmd)
	}
}

func (m *Model) imagePrune() tea.Cmd {
	if m.tab != tabImages {
		return nil
	}
	m.busy = true
	m.status = "Pruning dangling images…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, `docker image prune -f`)
	}
}

func (m *Model) runDockerAction(serverID uint, cmd string) tea.Msg {
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

func (m *Model) selectedContainerID() string {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.containers) {
		return ""
	}
	return m.containers[idx].ID
}

func (m *Model) selectedCompose() (composeProject, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.compose) {
		return composeProject{}, false
	}
	return m.compose[idx], true
}

func composeCommand(p composeProject, sub string) (string, error) {
	files := splitConfigFiles(p.ConfigFiles)
	if len(files) == 0 {
		return "", fmt.Errorf("no compose config path for project %q", p.Name)
	}
	dir := filepath.Dir(files[0])
	var b strings.Builder
	b.WriteString("docker compose")
	b.WriteString(" --project-directory ")
	b.WriteString(shellQuoteArg(dir))
	b.WriteString(" -p ")
	b.WriteString(shellQuoteArg(p.Name))
	for _, f := range files {
		b.WriteString(" -f ")
		b.WriteString(shellQuoteArg(f))
	}
	b.WriteByte(' ')
	b.WriteString(sub)
	return b.String(), nil
}

func splitConfigFiles(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, sep := range []string{", ", ",", "  "} {
		if strings.Contains(s, sep) {
			var out []string
			for _, part := range strings.Split(s, sep) {
				part = strings.TrimSpace(part)
				if part != "" {
					out = append(out, part)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []string{s}
}

func shellQuoteArg(s string) string {
	if s == "" {
		return "''"
	}
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func shortID(id string, n int) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > n {
		return id[:n]
	}
	return id
}
