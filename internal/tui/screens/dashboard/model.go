package dashboard

import (
	"errors"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
)

var errNotConnected = errors.New("not connected — open server from list")

type dashTab int

const (
	tabOverview dashTab = iota
	tabServices
	tabFiles
)

type loadState int

const (
	stateIdle loadState = iota
	stateLoading
	stateReady
	stateError
)

type tickMsg struct{}

type alertsLoadedMsg struct {
	alerts []storage.ErrorEvent
}

type Model struct {
	ctx   *shared.AppContext
	tab   dashTab
	width int
	height int

	// overview
	metrics      metricsData
	metricsState loadState
	metricsErr   string
	alerts       []storage.ErrorEvent
	alertsState  loadState

	// services
	services      []serviceEntry
	servicesState loadState
	servicesErr   string
	svcFilter     svcFilterMode
	svcTable      components.ListTable

	// files
	curPath       string
	dirEntries    []dirEntry
	dirState      loadState
	dirErr        string
	dirTable      components.ListTable
	dirLoaded     bool
	previewOpen   bool
	previewOverlay bool
	previewPath   string
	previewBody   string
	previewScroll components.ScrollView
	pathInput     textinput.Model
	pathInputMode bool
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "/var/www"
	ti.CharLimit = 512
	ti.Width = 40

	m := &Model{
		ctx:     ctx,
		tab:     tabOverview,
		curPath: "/",
		pathInput: ti,
	}
	return m
}

func (m *Model) Name() string { return "Dashboard" }

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.rebuildSvcTable()
	m.rebuildDirTable()
	if m.previewOpen {
		w := m.previewWidth()
		h := m.previewHeight()
		m.previewScroll = m.previewScroll.SetSize(w, h)
		m.previewScroll = m.previewScroll.SetContent(m.previewBody)
	}
	m.pathInput.Width = layoutInputWidth(w)
}

func layoutInputWidth(w int) int {
	if w < 40 {
		return w - 4
	}
	if w > 72 {
		return 72
	}
	return w - 6
}

func (m *Model) Init() tea.Cmd {
	m.metricsState = stateLoading
	m.servicesState = stateLoading
	m.alertsState = stateLoading
	return tea.Batch(
		m.loadAlerts(),
		m.loadMetrics(),
		m.loadServices(),
		m.scheduleTick(),
	)
}

func (m *Model) scheduleTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) refreshAll() tea.Cmd {
	cmds := []tea.Cmd{m.loadAlerts(), m.loadMetrics(), m.loadServices()}
	if m.tab == tabFiles && m.dirLoaded {
		cmds = append(cmds, m.loadDir())
	}
	return tea.Batch(cmds...)
}

func (m *Model) loadAlerts() tea.Cmd {
	return func() tea.Msg {
		var alerts []storage.ErrorEvent
		if m.ctx.DB != nil && m.ctx.ServerID != 0 {
			m.ctx.DB.Where("server_id = ? AND resolved = ?", m.ctx.ServerID, false).
				Order("last_seen desc").
				Limit(10).
				Find(&alerts)
		}
		return alertsLoadedMsg{alerts: alerts}
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(m.refreshAll(), m.scheduleTick())

	case alertsLoadedMsg:
		m.alerts = msg.alerts
		m.alertsState = stateReady
		return m, nil

	case metricsLoadedMsg:
		m.metricsState = stateReady
		if msg.err != nil {
			m.metricsState = stateError
			m.metricsErr = msg.err.Error()
		} else {
			m.metricsErr = ""
			m.metrics = msg.data
		}
		return m, nil

	case servicesLoadedMsg:
		m.servicesState = stateReady
		if msg.err != nil {
			m.servicesState = stateError
			m.servicesErr = msg.err.Error()
			m.services = nil
		} else {
			m.servicesErr = ""
			m.services = msg.services
		}
		m.rebuildSvcTable()
		return m, nil

	case dirLoadedMsg:
		m.dirState = stateReady
		if msg.err != nil {
			m.dirState = stateError
			m.dirErr = msg.err.Error()
		} else {
			m.dirErr = ""
			m.dirEntries = msg.entries
			m.curPath = msg.path
		}
		m.dirLoaded = true
		m.rebuildDirTable()
		return m, nil

	case filePreviewMsg:
		if msg.err != nil {
			m.previewBody = "Error: " + msg.err.Error()
		} else {
			m.previewBody = msg.body
		}
		m.previewPath = msg.path
		m.previewOpen = true
		m.previewOverlay = layoutBreakpointNarrow(m.width)
		w := m.previewWidth()
		h := m.previewHeight()
		m.previewScroll = components.NewScrollView(w, h)
		m.previewScroll = m.previewScroll.SetContent(m.previewBody).GotoBottom()
		return m, nil

	case tea.KeyMsg:
		if m.pathInputMode {
			return m.updatePathInput(msg)
		}
		if m.previewOpen && m.previewOverlay {
			if msg.String() == "esc" {
				m.previewOpen = false
				return m, nil
			}
			var cmd tea.Cmd
			m.previewScroll, cmd = m.previewScroll.Update(msg)
			return m, cmd
		}
		if nav, ok := m.handleKeys(msg); ok {
			return m, nav
		}
	}

	var cmd tea.Cmd
	switch m.tab {
	case tabServices:
		m.svcTable, cmd = m.svcTable.Update(msg)
	case tabFiles:
		if m.previewOpen && !m.previewOverlay {
			m.previewScroll, cmd = m.previewScroll.Update(msg)
		}
		m.dirTable, cmd = m.dirTable.Update(msg)
	}
	return m, cmd
}

func (m *Model) updatePathInput(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pathInputMode = false
		m.pathInput.Blur()
		return m, nil
	case "enter":
		path := m.pathInput.Value()
		m.pathInputMode = false
		m.pathInput.Blur()
		if path == "" {
			path = "/"
		}
		m.curPath = path
		m.previewOpen = false
		return m, m.loadDir()
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	return m, cmd
}

func (m *Model) handleKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "esc", "b":
		if m.previewOpen {
			m.previewOpen = false
			return nil, true
		}
		return func() tea.Msg { return shared.GoBackMsg{} }, true
	case "1":
		m.tab = tabOverview
		return nil, true
	case "2":
		m.tab = tabServices
		return nil, true
	case "3":
		m.tab = tabFiles
		if !m.dirLoaded {
			return m.loadDir(), true
		}
		return nil, true
	case "r":
		return tea.Batch(m.refreshAll()), true
	case "a":
		if m.tab == tabServices {
			m.svcFilter = m.svcFilter.next()
			m.rebuildSvcTable()
		}
		return nil, true
	case "enter":
		if m.tab == tabServices {
			return m.handleSvcEnter(), true
		}
		if m.tab == tabFiles {
			return m.handleDirEnter(), true
		}
		return nil, false
	case "backspace", "-":
		if m.tab == tabFiles && !m.pathInputMode {
			return m.goUpDir(), true
		}
		return nil, false
	case ".":
		if m.tab == tabFiles {
			return m.loadDir(), true
		}
		return nil, false
	case "g":
		if m.tab == tabFiles {
			m.pathInputMode = true
			m.pathInput.SetValue(m.curPath)
			m.pathInput.Focus()
			return textinput.Blink, true
		}
		return nil, false
	case "n":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDatabase, ServerID: m.ctx.ServerID}
		}, true
	case "d":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDocker, ServerID: m.ctx.ServerID}
		}, true
	case "p":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenPM2, ServerID: m.ctx.ServerID}
		}, true
	case "l":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenLogs, ServerID: m.ctx.ServerID}
		}, true
	case "m":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenServerMap, ServerID: m.ctx.ServerID}
		}, true
	case "e":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenErrTrack, ServerID: m.ctx.ServerID}
		}, true
	case "c":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenChat, ServerID: m.ctx.ServerID}
		}, true
	case "u":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenBackup, ServerID: m.ctx.ServerID}
		}, true
	case "x":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenProxy, ServerID: m.ctx.ServerID}
		}, true
	case ",":
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenSettings, ServerID: m.ctx.ServerID}
		}, true
	}
	return nil, false
}

func (m *Model) handleSvcEnter() tea.Cmd {
	idx := m.svcTable.Cursor()
	filtered := m.filteredServices()
	if idx < 0 || idx >= len(filtered) {
		return nil
	}
	s := filtered[idx]
	if s.kind == "docker" {
		return func() tea.Msg {
			return shared.NavigateMsg{Screen: shared.ScreenDocker, ServerID: m.ctx.ServerID}
		}
	}
	return func() tea.Msg {
		return shared.NavigateMsg{Screen: shared.ScreenLogs, ServerID: m.ctx.ServerID}
	}
}

func (m *Model) handleDirEnter() tea.Cmd {
	idx := m.dirTable.Cursor()
	if idx < 0 || idx >= len(m.dirEntries) {
		return nil
	}
	ent := m.dirEntries[idx]
	if ent.isParent {
		return m.goUpDir()
	}
	if ent.isDir {
		m.curPath = ent.fullPath
		m.previewOpen = false
		return m.loadDir()
	}
	return m.loadFilePreview(ent.fullPath, ent.size)
}

func (m *Model) goUpDir() tea.Cmd {
	if m.curPath == "/" {
		return nil
	}
	m.curPath = parentPath(m.curPath)
	m.previewOpen = false
	return m.loadDir()
}

func (m *Model) previewWidth() int {
	if m.previewOverlay {
		return m.width - 4
	}
	_, rightW, stack := splitPanels(m.width, 1, 50, 28, 24)
	if stack {
		return m.width - 4
	}
	return rightW
}

func (m *Model) previewHeight() int {
	return layout.BodyHeight(m.height, m.dirLocalChrome()+2, 5)
}

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.tab {
	case tabServices:
		return []components.KeyBinding{
			{Key: "a", Desc: "filter"},
			{Key: "enter", Desc: "open"},
			{Key: "r", Desc: "refresh"},
			{Key: "1/2/3", Desc: "tabs"},
			{Key: "b", Desc: "back"},
		}
	case tabFiles:
		return []components.KeyBinding{
			{Key: "enter", Desc: "open"},
			{Key: "-", Desc: "up dir"},
			{Key: "g", Desc: "go path"},
			{Key: ".", Desc: "refresh"},
			{Key: "b", Desc: "back"},
		}
	default:
		return []components.KeyBinding{
			{Key: "r", Desc: "refresh"},
			{Key: "d/p/l", Desc: "docker/pm2/logs"},
			{Key: "n", Desc: "database"},
			{Key: "1/2/3", Desc: "tabs"},
			{Key: "b", Desc: "back"},
		}
	}
}

func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) View() string {
	return renderDashboard(m)
}

// layoutBreakpointNarrow mirrors layout.Breakpoint without import cycle concerns in helpers.
func layoutBreakpointNarrow(width int) bool {
	return width <= 79
}
