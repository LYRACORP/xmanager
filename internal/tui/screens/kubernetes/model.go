package kubernetes

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/k8s"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeList mode = iota
	modeCreate
	modeDetail
	modeConfirm
)

type viewTab int

const (
	tabOverview viewTab = iota
	tabNodes
	tabNamespaces
	tabWorkloads
	tabNetwork
	tabConfig
	tabStorage
	tabEvents
	tabPodLogs
	tabHelm
	tabInstall
	tabCount
)

type createStep int

const (
	stepName createStep = iota
	stepMembers
	stepVersions
)

type memberPick struct {
	Server storage.Server
	Role   string // empty = off
}

type loadedListMsg struct{ items []storage.K8sCluster }
type loadedDetailMsg struct {
	cluster storage.K8sCluster
	members []storage.K8sClusterMember
	rows    []k8s.ResourceRow
	text    string
	err     error
}
type doneMsg struct {
	text string
	err  error
}
type tickMsg struct{}

type Model struct {
	ctx     *shared.AppContext
	mode    mode
	message string
	width   int
	height  int

	items []storage.K8sCluster
	tbl   components.ListTable

	cluster storage.K8sCluster
	members []storage.K8sClusterMember
	tab     viewTab
	rows    []k8s.ResourceRow
	body    string
	logView components.ScrollView

	createStep createStep
	nameIn     textinput.Model
	kubeIn     textinput.Model
	cniIn      textinput.Model
	picks      []memberPick
	pickIdx    int

	nsIn    textinput.Model
	nameArg textinput.Model
	extraIn textinput.Model
	formIdx int

	confirm   string
	confirmFn func() tea.Cmd
}

func New(ctx *shared.AppContext) *Model {
	nameIn := textinput.New()
	nameIn.Placeholder = "prod"
	nameIn.Prompt = "Name: "
	nameIn.Width = 32
	kubeIn := textinput.New()
	kubeIn.SetValue(k8s.DefaultKubeVersion)
	kubeIn.Prompt = "Kubernetes: "
	kubeIn.Width = 20
	cniIn := textinput.New()
	cniIn.SetValue(k8s.DefaultNetworkPlugin)
	cniIn.Prompt = "CNI: "
	cniIn.Width = 16
	nsIn := textinput.New()
	nsIn.Placeholder = "default"
	nsIn.Prompt = "ns: "
	nsIn.Width = 16
	nameArg := textinput.New()
	nameArg.Placeholder = "name"
	nameArg.Prompt = "name: "
	nameArg.Width = 24
	extraIn := textinput.New()
	extraIn.Placeholder = "replicas / chart / tail"
	extraIn.Prompt = "> "
	extraIn.Width = 40
	return &Model{
		ctx:     ctx,
		nameIn:  nameIn,
		kubeIn:  kubeIn,
		cniIn:   cniIn,
		nsIn:    nsIn,
		nameArg: nameArg,
		extraIn: extraIn,
		logView: components.NewScrollView(80, 12),
	}
}

func (m *Model) Name() string { return "Kubernetes" }

func (m *Model) OnNavigate(params map[string]interface{}) {
	if params == nil {
		return
	}
	if id, ok := params["cluster_id"].(uint); ok && id > 0 {
		m.mode = modeDetail
		m.cluster.ID = id
	}
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.rebuild()
	m.logView = m.logView.SetSize(w, layout.BodyHeight(h, 6, 6))
}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeCreate:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next"},
			{Key: "space", Desc: "role"},
			{Key: "enter", Desc: "continue"},
			{Key: "esc", Desc: "cancel"},
		}
	case modeDetail:
		return []components.KeyBinding{
			{Key: "tab", Desc: "next tab"},
			{Key: "r", Desc: "refresh"},
			{Key: "i", Desc: "install/upgrade"},
			{Key: "x", Desc: "reset"},
			{Key: "esc", Desc: "back"},
		}
	case modeConfirm:
		return []components.KeyBinding{{Key: "y", Desc: "confirm"}, {Key: "n", Desc: "cancel"}}
	default:
		return []components.KeyBinding{
			{Key: "enter", Desc: "open"},
			{Key: "n", Desc: "new"},
			{Key: "r", Desc: "refresh"},
		}
	}
}

func (m *Model) engine() *k8s.Engine {
	return k8s.NewEngine(m.ctx.DB, m.ctx.Pool)
}

func (m *Model) Init() tea.Cmd { return m.loadList() }

func (m *Model) loadList() tea.Cmd {
	return func() tea.Msg {
		list, err := m.engine().List(0)
		if err != nil {
			return doneMsg{err: err}
		}
		return loadedListMsg{items: list}
	}
}

func (m *Model) loadDetail() tea.Cmd {
	id := m.cluster.ID
	tab := m.tab
	return func() tea.Msg {
		eng := m.engine()
		c, err := eng.Get(id)
		if err != nil {
			return loadedDetailMsg{err: err}
		}
		members, _ := eng.Members(id)
		msg := loadedDetailMsg{cluster: c, members: members}
		if c.Status == "installing" || c.Status == "resetting" || c.Status == "scaling" || c.Status == "upgrading" {
			msg.text = c.LastLog
			return msg
		}
		ctx := context.Background()
		switch tab {
		case tabOverview:
			ov, err := eng.Overview(ctx, id)
			if err != nil {
				msg.err = err
				msg.text = c.LastLog
				break
			}
			msg.text = fmt.Sprintf("%s  %s\nkube %s  cni %s  kubespray %s\nnodes %v/%v ready\n%s",
				ov["name"], ov["status"], ov["kube_version"], ov["network_plugin"], ov["kubespray"],
				ov["nodes_ready"], ov["nodes_total"], ov["cluster_info"])
		case tabNodes:
			msg.rows, msg.err = eng.GetResources(ctx, id, "nodes", false)
		case tabNamespaces:
			msg.rows, msg.err = eng.GetResources(ctx, id, "namespaces", false)
		case tabWorkloads:
			msg.rows, msg.err = eng.GetResources(ctx, id, "deployments,statefulsets,daemonsets,jobs,cronjobs,pods", true)
		case tabNetwork:
			msg.rows, msg.err = eng.GetResources(ctx, id, "svc,ingress", true)
		case tabConfig:
			msg.rows, _, msg.err = eng.GetSecretsMasked(ctx, id)
			cms, err := eng.GetResources(ctx, id, "configmaps", true)
			if err == nil {
				msg.rows = append(cms, msg.rows...)
			}
		case tabStorage:
			msg.rows, msg.err = eng.GetResources(ctx, id, "pvc,pv,sc", true)
		case tabEvents:
			msg.rows, msg.err = eng.GetResources(ctx, id, "events", true)
		case tabPodLogs:
			ns := "default"
			pod := ""
			msg.text = "set ns + pod name, then enter"
			_ = ns
			_ = pod
		case tabHelm:
			out, err := eng.HelmList(ctx, id, "all")
			msg.text, msg.err = out, err
		case tabInstall:
			msg.text = c.LastLog
		}
		return msg
	}
}

func (m *Model) selected() (storage.K8sCluster, bool) {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.items) {
		return storage.K8sCluster{}, false
	}
	return m.items[i], true
}

func (m *Model) rebuild() {
	h := layout.BodyHeight(m.height, 4, 4)
	cols := []table.Column{
		{Title: "Name", Width: 22},
		{Title: "Status", Width: 12},
		{Title: "Kube", Width: 10},
		{Title: "CNI", Width: 10},
	}
	rows := make([]table.Row, len(m.items))
	for i, c := range m.items {
		rows[i] = table.Row{c.Name, c.Status, c.KubeVersion, c.NetworkPlugin}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(m.mode == modeList)
	if m.mode == modeDetail {
		m.rebuildDetailTable()
	}
}

func (m *Model) rebuildDetailTable() {
	h := layout.BodyHeight(m.height, 8, 4)
	cols := []table.Column{
		{Title: "NS", Width: 14},
		{Title: "Kind", Width: 14},
		{Title: "Name", Width: 28},
		{Title: "Status", Width: 12},
		{Title: "Extra", Width: 20},
	}
	rows := make([]table.Row, len(m.rows))
	for i, r := range m.rows {
		rows[i] = table.Row{r.Namespace, r.Kind, r.Name, r.Status, r.Extra}
	}
	m.tbl = m.tbl.SetData(m.width, cols, rows, h)
	m.tbl = m.tbl.SetFocused(true)
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedListMsg:
		m.items = msg.items
		m.rebuild()
		return m, nil
	case loadedDetailMsg:
		if msg.cluster.ID != 0 {
			m.cluster = msg.cluster
			m.members = msg.members
		}
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = ""
		}
		m.rows = msg.rows
		if msg.text != "" {
			m.body = msg.text
			m.logView = m.logView.SetContent(msg.text)
		}
		m.rebuild()
		busy := m.cluster.Status == "installing" || m.cluster.Status == "resetting" || m.cluster.Status == "scaling" || m.cluster.Status == "upgrading"
		if busy {
			return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
		}
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = msg.text
		}
		if m.mode == modeDetail {
			return m, m.loadDetail()
		}
		return m, m.loadList()
	case tickMsg:
		if m.mode == modeDetail {
			return m, m.loadDetail()
		}
		return m, nil
	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		var extra tea.Cmd
		switch m.mode {
		case modeCreate:
			extra = m.updateCreateInputs(msg)
		case modeList:
			m.tbl, extra = m.tbl.Update(msg)
		case modeDetail:
			var c2 tea.Cmd
			m.tbl, extra = m.tbl.Update(msg)
			m.logView, c2 = m.logView.Update(msg)
			extra = tea.Batch(extra, c2)
		}
		return m, tea.Batch(cmd, extra)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch m.mode {
	case modeConfirm:
		switch msg.String() {
		case "y":
			fn := m.confirmFn
			m.mode = modeDetail
			m.confirmFn = nil
			if fn != nil {
				return fn()
			}
		case "n", "esc":
			m.mode = modeDetail
			m.confirmFn = nil
		}
		return nil
	case modeCreate:
		return m.handleCreateKey(msg)
	case modeDetail:
		return m.handleDetailKey(msg)
	default:
		switch msg.String() {
		case "esc", "q":
			return func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			return m.loadList()
		case "n":
			m.startCreate()
			return nil
		case "enter":
			c, ok := m.selected()
			if !ok {
				return nil
			}
			m.cluster = c
			m.mode = modeDetail
			m.tab = tabOverview
			return m.loadDetail()
		}
	}
	return nil
}

func (m *Model) startCreate() {
	m.mode = modeCreate
	m.createStep = stepName
	m.nameIn.SetValue("")
	m.nameIn.Focus()
	m.kubeIn.SetValue(k8s.DefaultKubeVersion)
	m.cniIn.SetValue(k8s.DefaultNetworkPlugin)
	var servers []storage.Server
	_ = m.ctx.DB.Order("name").Find(&servers).Error
	m.picks = make([]memberPick, len(servers))
	for i, s := range servers {
		m.picks[i] = memberPick{Server: s}
	}
	m.pickIdx = 0
	m.message = ""
}

func (m *Model) handleCreateKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m.loadList()
	case "tab":
		if m.createStep == stepVersions {
			m.formIdx = (m.formIdx + 1) % 2
			m.focusVersions()
			return nil
		}
	case "up", "k":
		if m.createStep == stepMembers && m.pickIdx > 0 {
			m.pickIdx--
		}
	case "down", "j":
		if m.createStep == stepMembers && m.pickIdx < len(m.picks)-1 {
			m.pickIdx++
		}
	case " ":
		if m.createStep == stepMembers && m.pickIdx >= 0 && m.pickIdx < len(m.picks) {
			m.picks[m.pickIdx].Role = nextRole(m.picks[m.pickIdx].Role)
		}
	case "enter":
		switch m.createStep {
		case stepName:
			m.createStep = stepMembers
		case stepMembers:
			m.createStep = stepVersions
			m.formIdx = 0
			m.focusVersions()
		case stepVersions:
			return m.submitCreate()
		}
	}
	return nil
}

func (m *Model) focusVersions() {
	if m.formIdx == 0 {
		m.kubeIn.Focus()
		m.cniIn.Blur()
	} else {
		m.kubeIn.Blur()
		m.cniIn.Focus()
	}
}

func (m *Model) updateCreateInputs(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.createStep {
	case stepName:
		m.nameIn, cmd = m.nameIn.Update(msg)
	case stepVersions:
		if m.formIdx == 0 {
			m.kubeIn, cmd = m.kubeIn.Update(msg)
		} else {
			m.cniIn, cmd = m.cniIn.Update(msg)
		}
	}
	return cmd
}

func nextRole(cur string) string {
	switch cur {
	case "":
		return k8s.RoleWorker
	case k8s.RoleWorker:
		return "all"
	case "all":
		return k8s.RoleControlPlane + "," + k8s.RoleEtcd
	default:
		return ""
	}
}

func (m *Model) submitCreate() tea.Cmd {
	var members []k8s.MemberSpec
	for _, p := range m.picks {
		if p.Role == "" {
			continue
		}
		members = append(members, k8s.MemberSpec{ServerID: p.Server.ID, Role: p.Role})
	}
	if len(members) == 0 {
		m.message = "select at least one server (space cycles roles)"
		return nil
	}
	spec := k8s.CreateSpec{
		Name:          strings.TrimSpace(m.nameIn.Value()),
		KubeVersion:   strings.TrimSpace(m.kubeIn.Value()),
		NetworkPlugin: strings.TrimSpace(m.cniIn.Value()),
		Members:       members,
	}
	m.mode = modeDetail
	m.tab = tabInstall
	eng := m.engine()
	return func() tea.Msg {
		c, _, err := eng.SaveNew(spec)
		if err != nil {
			return doneMsg{err: err}
		}
		go func() { _ = eng.RunPlaybook(context.Background(), c.ID, k8s.PlaybookCluster, "", nil) }()
		members, _ := eng.Members(c.ID)
		return loadedDetailMsg{cluster: c, members: members, text: c.LastLog}
	}
}

func (m *Model) handleDetailKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeList
		return m.loadList()
	case "tab":
		m.tab = (m.tab + 1) % tabCount
		return m.loadDetail()
	case "shift+tab":
		m.tab = (m.tab + tabCount - 1) % tabCount
		return m.loadDetail()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0":
		n, _ := strconv.Atoi(msg.String())
		if msg.String() == "0" {
			n = 10
		} else {
			n--
		}
		if n >= 0 && n < int(tabCount) {
			m.tab = viewTab(n)
			return m.loadDetail()
		}
	case "r":
		return m.loadDetail()
	case "i":
		id := m.cluster.ID
		m.mode = modeConfirm
		m.confirm = "Run cluster.yml / upgrade on " + m.cluster.Name + "?"
		m.confirmFn = func() tea.Cmd {
			return func() tea.Msg {
				play := k8s.PlaybookCluster
				if m.cluster.Status == "ready" {
					play = k8s.PlaybookUpgrade
				}
				eng := m.engine()
				go func() { _ = eng.RunPlaybook(context.Background(), id, play, "", nil) }()
				return doneMsg{text: play + " started"}
			}
		}
		return nil
	case "x":
		id := m.cluster.ID
		m.mode = modeConfirm
		m.confirm = "DESTRUCTIVE: reset cluster " + m.cluster.Name + "?"
		m.confirmFn = func() tea.Cmd {
			return func() tea.Msg {
				eng := m.engine()
				go func() { _ = eng.Reset(context.Background(), id, nil) }()
				return doneMsg{text: "reset started"}
			}
		}
		return nil
	case "c":
		return m.nodeAction("cordon")
	case "u":
		return m.nodeAction("uncordon")
	case "d":
		return m.nodeAction("drain")
	case "s":
		return m.scaleSelected()
	case "enter":
		if m.tab == tabPodLogs {
			return m.fetchPodLogs()
		}
		if m.tab == tabHelm && strings.TrimSpace(m.extraIn.Value()) != "" {
			return m.helmInstall()
		}
		return m.describeSelected()
	}
	if m.tab == tabPodLogs {
		var cmd tea.Cmd
		m.nameArg, cmd = m.nameArg.Update(msg)
		return cmd
	}
	if m.tab == tabHelm {
		var cmd tea.Cmd
		m.extraIn, cmd = m.extraIn.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) selectedRow() (k8s.ResourceRow, bool) {
	i := m.tbl.Cursor()
	if i < 0 || i >= len(m.rows) {
		return k8s.ResourceRow{}, false
	}
	return m.rows[i], true
}

func (m *Model) nodeAction(action string) tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	id := m.cluster.ID
	name := row.Name
	run := func() tea.Msg {
		err := m.engine().NodeAction(context.Background(), id, action, name)
		return doneMsg{err: err, text: action + " " + name}
	}
	if action == "drain" {
		m.mode = modeConfirm
		m.confirm = "Drain node " + name + "?"
		m.confirmFn = func() tea.Cmd { return run }
		return nil
	}
	return run
}

func (m *Model) scaleSelected() tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(m.extraIn.Value()))
	if err != nil {
		m.message = "set replica count in the input, then s"
		return nil
	}
	id := m.cluster.ID
	kind, ns, name := row.Kind, row.Namespace, row.Name
	return func() tea.Msg {
		err := m.engine().ScaleWorkload(context.Background(), id, kind, ns, name, n)
		return doneMsg{err: err, text: "scaled"}
	}
}

func (m *Model) describeSelected() tea.Cmd {
	row, ok := m.selectedRow()
	if !ok {
		return nil
	}
	id := m.cluster.ID
	return func() tea.Msg {
		out, err := m.engine().Describe(context.Background(), id, row.Kind, row.Namespace, row.Name)
		return loadedDetailMsg{text: out, err: err, cluster: m.cluster, members: m.members, rows: m.rows}
	}
}

func (m *Model) fetchPodLogs() tea.Cmd {
	ns := strings.TrimSpace(m.nsIn.Value())
	pod := strings.TrimSpace(m.nameArg.Value())
	if row, ok := m.selectedRow(); ok && pod == "" {
		ns, pod = row.Namespace, row.Name
	}
	if pod == "" {
		m.message = "pick a pod or type a name"
		return nil
	}
	id := m.cluster.ID
	return func() tea.Msg {
		out, err := m.engine().Logs(context.Background(), id, ns, pod, "", 200)
		return loadedDetailMsg{text: out, err: err, cluster: m.cluster, members: m.members}
	}
}

func (m *Model) helmInstall() tea.Cmd {
	parts := strings.Fields(m.extraIn.Value())
	if len(parts) < 2 {
		m.message = "helm: name chart [ns]"
		return nil
	}
	ns := strings.TrimSpace(m.nsIn.Value())
	if len(parts) > 2 {
		ns = parts[2]
	}
	id := m.cluster.ID
	name, chart := parts[0], parts[1]
	return func() tea.Msg {
		out, err := m.engine().HelmInstall(context.Background(), id, name, chart, ns, "")
		return doneMsg{err: err, text: out}
	}
}

func (m *Model) View() string {
	switch m.mode {
	case modeCreate:
		return m.viewCreate()
	case modeConfirm:
		body := theme.ScreenChrome("Confirm", m.confirm, m.width)
		body = lipgloss.JoinVertical(lipgloss.Left, body, theme.MutedText().Render("y confirm · n cancel"))
		return body
	case modeDetail:
		return m.viewDetail()
	default:
		title := theme.ScreenChrome("Kubernetes", "n new · enter open · Ctrl+K from anywhere", m.width)
		body := m.tbl.View()
		if m.message != "" {
			body = lipgloss.JoinVertical(lipgloss.Left, body, theme.MutedText().Render(m.message))
		}
		return lipgloss.JoinVertical(lipgloss.Left, title, body)
	}
}

func (m *Model) viewCreate() string {
	var body string
	switch m.createStep {
	case stepName:
		body = m.nameIn.View()
	case stepMembers:
		var b strings.Builder
		b.WriteString("space cycles role (worker / all / cp+etcd / off)\n")
		for i, p := range m.picks {
			mark := "  "
			if i == m.pickIdx {
				mark = "> "
			}
			role := p.Role
			if role == "" {
				role = "off"
			}
			fmt.Fprintf(&b, "%s%s  %s  %s\n", mark, p.Server.Name, p.Server.Host, role)
		}
		body = b.String()
	case stepVersions:
		body = lipgloss.JoinVertical(lipgloss.Left, m.kubeIn.View(), m.cniIn.View(), theme.MutedText().Render("enter to start cluster.yml"))
	}
	title := theme.ScreenChrome("New cluster", "Kubespray v2.31 · kube ≥ v1.34 · Docker on this machine", m.width)
	if m.message != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, theme.MutedText().Render(m.message))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, body)
}

func (m *Model) viewDetail() string {
	tabs := []components.TabItem{
		{ID: int(tabOverview), Label: "[1] Overview"},
		{ID: int(tabNodes), Label: "[2] Nodes"},
		{ID: int(tabNamespaces), Label: "[3] NS"},
		{ID: int(tabWorkloads), Label: "[4] Workloads"},
		{ID: int(tabNetwork), Label: "[5] Net"},
		{ID: int(tabConfig), Label: "[6] Config"},
		{ID: int(tabStorage), Label: "[7] Storage"},
		{ID: int(tabEvents), Label: "[8] Events"},
		{ID: int(tabPodLogs), Label: "[9] Logs"},
		{ID: int(tabHelm), Label: "[0] Helm"},
		{ID: int(tabInstall), Label: "Install"},
	}
	bar := components.TabBar{Tabs: tabs, Active: int(m.tab), Width: m.width}
	sub := fmt.Sprintf("%s · %s · kube %s · %s", m.cluster.Name, m.cluster.Status, m.cluster.KubeVersion, m.cluster.NetworkPlugin)
	title := theme.ScreenChrome("Kubernetes", sub, m.width)
	var body string
	switch m.tab {
	case tabOverview, tabInstall, tabHelm, tabPodLogs:
		if m.tab == tabPodLogs || m.tab == tabHelm {
			body = lipgloss.JoinVertical(lipgloss.Left, m.nsIn.View()+"  "+m.nameArg.View()+"  "+m.extraIn.View(), m.logView.View())
		} else {
			body = m.logView.View()
		}
	default:
		body = m.tbl.View()
	}
	if m.body != "" && (m.tab == tabOverview || m.tab == tabInstall || m.tab == tabHelm || m.tab == tabPodLogs) {
		m.logView = m.logView.SetContent(m.body)
		body = m.logView.View()
		if m.tab == tabPodLogs || m.tab == tabHelm {
			body = lipgloss.JoinVertical(lipgloss.Left, m.nsIn.View()+"  "+m.nameArg.View()+"  "+m.extraIn.View(), m.logView.View())
		}
	}
	if m.message != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, theme.MutedText().Render(m.message))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, bar.View(), body)
}
