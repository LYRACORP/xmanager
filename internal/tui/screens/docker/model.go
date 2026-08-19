package docker

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	xmdocker "github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type viewTab int

const (
	tabContainers viewTab = iota
	tabCompose
	tabImages
	tabVolumes
	tabNetworks
	tabBuild
	tabSwarm
	tabCount
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
	volumes    []xmdocker.Volume
	networks   []xmdocker.Network
	cache      []xmdocker.BuildCache

	swarm       xmdocker.SwarmInfo
	swarmNodes  []xmdocker.SwarmNode
	swarmSvcs   []xmdocker.SwarmService
	workerJoin  string
	managerJoin string
}

type headerDoneMsg struct {
	login xmdocker.LoginInfo
	df    []xmdocker.SystemDFRow
}

type actionDoneMsg struct {
	err   string
	brief string
}

type Model struct {
	ctx *shared.AppContext

	tab    viewTab
	width  int
	height int

	table components.ListTable

	containers []containerRow
	compose    []composeProject
	images     []imageRow
	volumes    []xmdocker.Volume
	networks   []xmdocker.Network
	cache      []xmdocker.BuildCache

	login xmdocker.LoginInfo
	df    []xmdocker.SystemDFRow

	swarm         xmdocker.SwarmInfo
	swarmNodes    []xmdocker.SwarmNode
	swarmSvcs     []xmdocker.SwarmService
	workerJoin    string
	managerJoin   string
	swarmUI       swarmMode
	joinTargets   []fleetJoinTarget
	joinCursor    int
	pendingNodeID string

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
	return m.refresh()
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case headerDoneMsg:
		m.login = msg.login
		m.df = msg.df
		return m, nil

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
			case tabVolumes:
				m.volumes = msg.volumes
			case tabNetworks:
				m.networks = msg.networks
			case tabBuild:
				m.cache = msg.cache
			case tabSwarm:
				m.swarm = msg.swarm
				m.swarmNodes = msg.swarmNodes
				m.swarmSvcs = msg.swarmSvcs
				m.workerJoin = msg.workerJoin
				m.managerJoin = msg.managerJoin
			}
		}
		m.rebuildTable()
		return m, nil

	case actionDoneMsg:
		m.busy = false
		m.err = msg.err
		m.status = msg.brief
		if msg.err == "" {
			return m, m.refresh()
		}
		return m, nil

	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		if scr, cmd, ok := m.handleSwarmKey(msg); ok {
			return scr, cmd
		}
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "tab":
			m.nextTab()
			return m, m.refresh()
		case "shift+tab":
			m.prevTab()
			return m, m.refresh()
		case "1":
			m.setTab(tabContainers)
			return m, m.refresh()
		case "2":
			m.setTab(tabCompose)
			return m, m.refresh()
		case "3":
			m.setTab(tabImages)
			return m, m.refresh()
		case "4":
			m.setTab(tabVolumes)
			return m, m.refresh()
		case "5":
			m.setTab(tabNetworks)
			return m, m.refresh()
		case "6":
			m.setTab(tabBuild)
			return m, m.refresh()
		case "7":
			m.setTab(tabSwarm)
			return m, m.refresh()
		case "ctrl+r", "f5":
			m.status = "Refreshing…"
			return m, m.refresh()
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
			return m, m.pruneCurrent()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *Model) KeyBindings() []components.KeyBinding {
	base := []components.KeyBinding{
		{Key: "tab", Desc: "next view"},
		{Key: "1-7", Desc: "view"},
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
	case tabVolumes:
		return append([]components.KeyBinding{
			{Key: "c", Desc: "prune unused"},
		}, base...)
	case tabNetworks:
		return append([]components.KeyBinding{
			{Key: "c", Desc: "prune unused"},
		}, base...)
	case tabBuild:
		return append([]components.KeyBinding{
			{Key: "c", Desc: "prune cache"},
		}, base...)
	case tabSwarm:
		keys := []components.KeyBinding{
			{Key: "i", Desc: "init"},
			{Key: "c", Desc: "copy join"},
			{Key: "j", Desc: "fleet join"},
			{Key: "p", Desc: "promote"},
			{Key: "m", Desc: "demote"},
			{Key: "a", Desc: "availability"},
			{Key: "x", Desc: "remove"},
			{Key: "L", Desc: "leave"},
		}
		if m.swarmUI == swarmModeConfirmLeave || m.swarmUI == swarmModeConfirmRemove {
			keys = []components.KeyBinding{{Key: "y/n", Desc: "confirm"}}
		}
		if m.swarmUI == swarmModeJoinPick {
			keys = []components.KeyBinding{
				{Key: "enter", Desc: "join"},
				{Key: "esc", Desc: "cancel"},
			}
		}
		return append(keys, base...)
	}
	return base
}

func (m *Model) OnNavigate(params map[string]interface{}) {
	if params == nil {
		return
	}
	if t, ok := params["tab"].(int); ok && t >= 0 && t < int(tabCount) {
		m.tab = viewTab(t)
		m.rebuildTable()
	}
}

func (m *Model) localChrome() int {
	n := components.FrameChromeRows(true) + 3*components.TabBarRows()
	if m.err != "" || m.status != "" || m.busy {
		n++
	}
	if m.tab == tabSwarm {
		n += m.swarmChromeRows(layout.ContentWidth(m.width))
	}
	return n
}

func (m *Model) tabBar() string {
	row1 := components.NewTabBar([]components.TabItem{
		{ID: int(tabContainers), Label: "[1] Containers"},
		{ID: int(tabCompose), Label: "[2] Compose"},
		{ID: int(tabImages), Label: "[3] Images"},
	}, int(m.tab))
	row2 := components.NewTabBar([]components.TabItem{
		{ID: int(tabVolumes), Label: "[4] Volumes"},
		{ID: int(tabNetworks), Label: "[5] Networks"},
		{ID: int(tabBuild), Label: "[6] Build"},
	}, int(m.tab))
	row3 := components.NewTabBar([]components.TabItem{
		{ID: int(tabSwarm), Label: "[7] Swarm"},
	}, int(m.tab))
	row1.Width = m.width
	row2.Width = m.width
	row3.Width = m.width
	return row1.View() + "\n" + row2.View() + "\n" + row3.View()
}

func (m *Model) headerSubtitle() string {
	acc := m.login.AccountLine()
	parts := []string{acc}
	if h, ok := xmdocker.DFRowByType(m.df, "Images"); ok && h.SizeHuman != "" {
		parts = append(parts, "images "+h.SizeHuman)
	}
	if h, ok := xmdocker.DFRowByType(m.df, "Volumes"); ok && h.SizeHuman != "" {
		parts = append(parts, "volumes "+h.SizeHuman)
	}
	if h, ok := xmdocker.DFRowByType(m.df, "Build Cache"); ok && h.SizeHuman != "" {
		parts = append(parts, "cache "+h.SizeHuman)
	}
	if len(parts) == 1 {
		return acc + "  ·  volumes · networks · images · build"
	}
	return strings.Join(parts, "  ·  ")
}

func (m *Model) View() string {
	inner := layout.ContentWidth(m.width)
	body := m.tabBar()
	if p := m.swarmPanel(inner); p != "" {
		body += "\n" + p
	}
	body += "\n" + m.table.View()
	if o := m.swarmOverlay(); o != "" {
		body += "\n" + o
	}
	if m.err != "" {
		body += "\n" + theme.ErrorText().Render(components.Wrap(m.err, inner))
	} else if m.status != "" {
		body += "\n" + theme.MutedText().Render(components.Wrap(m.status, inner))
	}
	if m.busy {
		body += "\n" + theme.MutedText().Render("Working…")
	}
	return components.ScreenFrame{
		Title:       "Docker",
		Subtitle:    m.headerSubtitle(),
		Width:       m.width,
		Body:        body,
		LocalChrome: m.localChrome(),
	}.View()
}

func (m *Model) nextTab() {
	m.tab = (m.tab + 1) % tabCount
	m.rebuildTable()
}

func (m *Model) prevTab() {
	m.tab = (m.tab + tabCount - 1) % tabCount
	m.rebuildTable()
}

func (m *Model) setTab(t viewTab) {
	m.tab = t
	m.swarmUI = swarmModeList
	m.rebuildTable()
}

func (m *Model) rebuildTable() {
	h := layout.BodyHeight(m.height, m.localChrome(), 5)

	switch m.tab {
	case tabContainers:
		cols := []table.Column{
			{Title: "ID", Width: 14},
			{Title: "Names", Width: 22},
			{Title: "State", Width: 10},
			{Title: "Status", Width: 28},
			{Title: "Image", Width: 0},
		}
		rows := make([]table.Row, len(m.containers))
		for i, c := range m.containers {
			id := c.ID
			if len(id) > 12 {
				id = id[:12]
			}
			rows[i] = table.Row{id, c.Names, c.State, c.Status, c.Image}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabCompose:
		cols := []table.Column{
			{Title: "Project", Width: 20},
			{Title: "Status", Width: 22},
			{Title: "Config", Width: 0},
		}
		rows := make([]table.Row, len(m.compose))
		for i, p := range m.compose {
			rows[i] = table.Row{p.Name, p.Status, truncate(p.ConfigFiles, 46)}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabImages:
		cols := []table.Column{
			{Title: "Repository", Width: 28},
			{Title: "Tag", Width: 14},
			{Title: "ID", Width: 14},
			{Title: "Size", Width: 0},
		}
		rows := make([]table.Row, len(m.images))
		for i, im := range m.images {
			rows[i] = table.Row{im.Repository, im.Tag, shortID(im.ID, 12), im.Size}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabVolumes:
		cols := []table.Column{
			{Title: "Name", Width: 28},
			{Title: "Driver", Width: 10},
			{Title: "Links", Width: 7},
			{Title: "Size", Width: 0},
		}
		rows := make([]table.Row, len(m.volumes))
		for i, v := range m.volumes {
			rows[i] = table.Row{v.Name, v.Driver, fmt.Sprintf("%d", v.Links), v.SizeHuman}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabNetworks:
		cols := []table.Column{
			{Title: "Name", Width: 18},
			{Title: "Driver", Width: 10},
			{Title: "Scope", Width: 8},
			{Title: "Ctrs", Width: 6},
			{Title: "Subnet", Width: 0},
		}
		rows := make([]table.Row, len(m.networks))
		for i, n := range m.networks {
			name := n.Name
			if n.Internal {
				name += " (int)"
			}
			rows[i] = table.Row{name, n.Driver, n.Scope, fmt.Sprintf("%d", n.Containers), n.Subnet}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabBuild:
		cols := []table.Column{
			{Title: "ID", Width: 16},
			{Title: "Type", Width: 16},
			{Title: "Shared", Width: 8},
			{Title: "Use", Width: 5},
			{Title: "Size", Width: 0},
		}
		rows := make([]table.Row, len(m.cache))
		for i, c := range m.cache {
			shared := "no"
			if c.Shared {
				shared = "yes"
			}
			rows[i] = table.Row{shortID(c.ID, 12), c.Type, shared, fmt.Sprintf("%d", c.Usage), c.SizeHuman}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)

	case tabSwarm:
		cols := []table.Column{
			{Title: "ID", Width: 14},
			{Title: "Hostname", Width: 22},
			{Title: "Status", Width: 10},
			{Title: "Avail", Width: 10},
			{Title: "Manager", Width: 0},
		}
		rows := make([]table.Row, len(m.swarmNodes))
		for i, n := range m.swarmNodes {
			id := n.ID
			if len(id) > 12 {
				id = id[:12]
			}
			mgr := n.ManagerStatus
			if mgr == "" {
				mgr = "—"
			}
			rows[i] = table.Row{id, n.Hostname, n.Status, n.Availability, mgr}
		}
		m.table = m.table.SetData(m.width, cols, rows, h)
	}
}

func (m *Model) refresh() tea.Cmd {
	m.busy = true
	return tea.Batch(m.loadHeaderCmd(), m.reloadCmd())
}

func (m *Model) loadHeaderCmd() tea.Cmd {
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return headerDoneMsg{}
		}
		mgr := xmdocker.NewManager(ex)
		login, _ := mgr.LoginInfo()
		df, _ := mgr.SystemDF()
		return headerDoneMsg{login: login, df: df}
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
		case tabVolumes:
			return m.loadVolumes(ex, tab)
		case tabNetworks:
			return m.loadNetworks(ex, tab)
		case tabBuild:
			return m.loadBuild(ex, tab)
		case tabSwarm:
			return m.loadSwarm(ex, tab)
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

func (m *Model) loadVolumes(ex *ssh.Executor, tab viewTab) tea.Msg {
	vols, err := xmdocker.NewManager(ex).ListVolumes()
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	return loadDoneMsg{tab: tab, volumes: vols, brief: fmt.Sprintf("%d volumes", len(vols))}
}

func (m *Model) loadNetworks(ex *ssh.Executor, tab viewTab) tea.Msg {
	nets, err := xmdocker.NewManager(ex).ListNetworks()
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	return loadDoneMsg{tab: tab, networks: nets, brief: fmt.Sprintf("%d networks", len(nets))}
}

func (m *Model) loadBuild(ex *ssh.Executor, tab viewTab) tea.Msg {
	cache, err := xmdocker.NewManager(ex).ListBuildCache()
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error()}
	}
	var total uint64
	for _, c := range cache {
		total += c.SizeBytes
	}
	brief := fmt.Sprintf("%d cache entries", len(cache))
	if total > 0 {
		brief += " · " + xmdocker.FormatSize(total)
	}
	return loadDoneMsg{tab: tab, cache: cache, brief: brief}
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

func (m *Model) pruneCurrent() tea.Cmd {
	var cmd, status string
	switch m.tab {
	case tabImages:
		cmd, status = `docker image prune -f`, "Pruning dangling images…"
	case tabVolumes:
		cmd, status = `docker volume prune -f`, "Pruning unused volumes…"
	case tabNetworks:
		cmd, status = `docker network prune -f`, "Pruning unused networks…"
	case tabBuild:
		cmd, status = `docker builder prune -f`, "Pruning build cache…"
	default:
		return nil
	}
	m.busy = true
	m.status = status
	sid := m.ctx.ServerID
	return func() tea.Msg {
		return m.runDockerAction(sid, cmd)
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
