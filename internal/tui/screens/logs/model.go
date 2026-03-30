package logs

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

// SourceKind identifies how the remote tail command was chosen.
type SourceKind string

const (
	SourceDocker   SourceKind = "docker"
	SourcePM2      SourceKind = "pm2"
	SourceSystemd  SourceKind = "systemd"
	SourceFile     SourceKind = "file"
)

type sourcePreset struct {
	ID      int
	Label   string
	Kind    SourceKind
	Command string
}

func builtinPresets() []sourcePreset {
	return []sourcePreset{
		{1, "Docker (web)", SourceDocker, `docker logs -f --tail 200 web 2>&1`},
		{2, "PM2", SourcePM2, `sh -c 'pm2 logs --raw --lines 200 2>&1'`},
		{3, "systemd journal", SourceSystemd, `journalctl -f -n 200 --no-pager`},
		{4, "/var/log/syslog", SourceFile, `tail -f /var/log/syslog 2>&1`},
	}
}

type logLine struct {
	PresetID int
	Source   string
	Kind     SourceKind
	Raw      string
}

type streamSlot struct {
	label   string
	kind    SourceKind
	reader  *bufio.Reader
	cleanup func()
}

type streamReadMsg struct {
	presetID int
	line     string
	err      error
}

// Model is the real-time multi-source log viewer (SSH tail -f / streaming).
type Model struct {
	ctx *shared.AppContext

	width  int
	height int

	viewport viewport.Model
	presets  []sourcePreset

	// wanted[presetID] = user wants this source when not paused
	wanted map[int]bool
	// active streams (subset of wanted when running && !paused)
	streams map[int]*streamSlot

	buffer   []logLine
	maxLines int

	follow bool
	paused bool

	searchInput textinput.Model
	searchMode  bool
	searchRe    *regexp.Regexp
	searchRaw   string
	searchErr   string

	status string

	mu sync.Mutex // guards streams map for read cmd goroutine vs Update
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "regex (Enter apply, Esc cancel)"
	ti.CharLimit = 256
	ti.Width = 40

	max := 2000
	if ctx != nil && ctx.Config != nil && ctx.Config.AI.MaxLogLines > 0 {
		max = ctx.Config.AI.MaxLogLines
	}

	m := &Model{
		ctx:         ctx,
		presets:     builtinPresets(),
		wanted:      make(map[int]bool),
		streams:     make(map[int]*streamSlot),
		maxLines:    max,
		follow:      true,
		searchInput: ti,
	}
	m.viewport.YPosition = 0
	return m
}

func (m *Model) Name() string { return "Logs" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.layoutViewport()
}

func (m *Model) layoutViewport() {
	headerRows := 2
	if m.searchMode {
		headerRows++
	}
	help := 1
	vh := m.height - headerRows - help
	if vh < 3 {
		vh = 3
	}
	m.viewport.Width = max(10, m.width-2)
	m.viewport.Height = vh
	m.syncViewportContent()
}

func (m *Model) Init() tea.Cmd {
	m.layoutViewport()
	return nil
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case streamReadMsg:
		return m.onStreamRead(msg)

	case tea.KeyMsg:
		if m.searchMode {
			return m.updateSearchKeys(msg)
		}
		return m.updateMainKeys(msg)

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) updateSearchKeys(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchInput.Blur()
		m.layoutViewport()
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.searchInput.Value())
		m.applySearchPattern(raw)
		m.searchMode = false
		m.searchInput.Blur()
		m.layoutViewport()
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m *Model) applySearchPattern(raw string) {
	m.searchRaw = raw
	m.searchErr = ""
	m.searchRe = nil
	if raw == "" {
		return
	}
	re, err := regexp.Compile(raw)
	if err != nil {
		m.searchErr = err.Error()
		return
	}
	m.searchRe = re
}

func (m *Model) updateMainKeys(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.stopAllStreams()
		return m, func() tea.Msg { return shared.GoBackMsg{} }

	case "f":
		m.follow = !m.follow
		m.status = fmt.Sprintf("follow: %v", m.follow)
		return m, nil

	case "p":
		if m.paused {
			m.paused = false
			m.status = "resumed"
			return m, m.restartWantedStreams()
		}
		m.paused = true
		m.status = "paused"
		m.stopAllStreams()
		return m, nil

	case "/":
		m.searchMode = true
		m.searchInput.Focus()
		m.layoutViewport()
		return m, textinput.Blink

	case "ctrl+u":
		m.applySearchPattern("")
		m.searchErr = ""
		m.status = "search cleared"
		m.syncViewportContent()
		return m, nil

	case "pgup", "b", "u":
		m.follow = false
		m.viewport.LineUp(3)
		return m, nil
	case "pgdown", "d", "ctrl+d":
		m.viewport.LineDown(3)
		return m, nil
	case "home", "g":
		m.follow = false
		m.viewport.GotoTop()
		return m, nil
	case "end", "G":
		m.viewport.GotoBottom()
		return m, nil
	}

	if len(msg.String()) == 1 {
		c := msg.String()[0]
		if c >= '1' && c <= '4' {
			id := int(c - '0')
			return m, m.togglePreset(id)
		}
	}

	// Delegate scroll keys to viewport when not searching
	if keyScrollsViewport(msg) {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if m.follow {
			m.follow = false
		}
		return m, cmd
	}

	return m, nil
}

func keyScrollsViewport(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "up", "down", "left", "right":
		return true
	default:
		return false
	}
}

func (m *Model) togglePreset(id int) tea.Cmd {
	found := false
	for _, p := range m.presets {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	if m.wanted[id] {
		delete(m.wanted, id)
		m.stopPreset(id)
		m.status = fmt.Sprintf("source %d off", id)
		return nil
	}
	m.wanted[id] = true
	m.status = fmt.Sprintf("source %d on", id)
	if m.paused {
		return nil
	}
	return m.startPreset(id)
}

func (m *Model) restartWantedStreams() tea.Cmd {
	var cmds []tea.Cmd
	for id := range m.wanted {
		cmds = append(cmds, m.startPreset(id))
	}
	return tea.Batch(cmds...)
}

func (m *Model) startPreset(id int) tea.Cmd {
	if m.ctx == nil {
		m.status = "no app context"
		return nil
	}
	exec, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
	if !ok || exec == nil {
		m.status = "SSH not connected — open server from list first"
		return nil
	}
	var preset *sourcePreset
	for i := range m.presets {
		if m.presets[i].ID == id {
			preset = &m.presets[i]
			break
		}
	}
	if preset == nil {
		return nil
	}

	m.stopPreset(id)

	reader, cleanup, err := exec.Stream(preset.Command)
	if err != nil {
		m.status = fmt.Sprintf("stream %d: %v", id, err)
		delete(m.wanted, id)
		return nil
	}

	m.mu.Lock()
	m.streams[id] = &streamSlot{
		label:   preset.Label,
		kind:    preset.Kind,
		reader:  bufio.NewReader(reader),
		cleanup: cleanup,
	}
	m.mu.Unlock()

	m.status = fmt.Sprintf("streaming: %s", preset.Label)
	return m.scheduleRead(id)
}

func (m *Model) scheduleRead(presetID int) tea.Cmd {
	return func() tea.Msg {
		m.mu.Lock()
		slot, ok := m.streams[presetID]
		if !ok || slot == nil || slot.reader == nil {
			m.mu.Unlock()
			return streamReadMsg{presetID: presetID, err: io.EOF}
		}
		line, err := slot.reader.ReadString('\n')
		m.mu.Unlock()

		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")
		return streamReadMsg{presetID: presetID, line: line, err: err}
	}
}

func (m *Model) onStreamRead(msg streamReadMsg) (shared.Screen, tea.Cmd) {
	if msg.err != nil && msg.err != io.EOF && !strings.Contains(msg.err.Error(), "EOF") {
		m.status = fmt.Sprintf("read error (preset %d): %v", msg.presetID, msg.err)
		m.stopPreset(msg.presetID)
		return m, nil
	}
	if msg.err == io.EOF || (msg.err != nil && strings.Contains(msg.err.Error(), "EOF")) {
		m.stopPreset(msg.presetID)
		return m, nil
	}

	if m.paused {
		// Pause stops remote streams; drop any in-flight line.
		return m, nil
	}

	preset := m.presetByID(msg.presetID)
	src := "?"
	kind := SourceFile
	if preset != nil {
		src = preset.Label
		kind = preset.Kind
	}

	m.appendLine(logLine{
		PresetID: msg.presetID,
		Source:   src,
		Kind:     kind,
		Raw:      msg.line,
	})
	m.syncViewportContent()
	if m.follow {
		m.viewport.GotoBottom()
	}
	return m, m.scheduleRead(msg.presetID)
}

func (m *Model) presetByID(id int) *sourcePreset {
	for i := range m.presets {
		if m.presets[i].ID == id {
			return &m.presets[i]
		}
	}
	return nil
}

func (m *Model) appendLine(line logLine) {
	m.buffer = append(m.buffer, line)
	if len(m.buffer) > m.maxLines {
		excess := len(m.buffer) - m.maxLines
		m.buffer = append([]logLine(nil), m.buffer[excess:]...)
	}
}

func (m *Model) stopPreset(id int) {
	m.mu.Lock()
	slot := m.streams[id]
	delete(m.streams, id)
	m.mu.Unlock()
	if slot != nil && slot.cleanup != nil {
		slot.cleanup()
	}
}

func (m *Model) stopAllStreams() {
	m.mu.Lock()
	ids := make([]int, 0, len(m.streams))
	for id := range m.streams {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.stopPreset(id)
	}
}

func (m *Model) visibleLines() []logLine {
	if m.searchRe == nil {
		return m.buffer
	}
	out := make([]logLine, 0, len(m.buffer)/4+8)
	for _, ln := range m.buffer {
		if m.searchRe.MatchString(ln.Raw) {
			out = append(out, ln)
		}
	}
	return out
}

func (m *Model) syncViewportContent() {
	m.viewport.SetContent(m.renderLogBody())
}

func (m *Model) renderLogBody() string {
	lines := m.visibleLines()
	if len(lines) == 0 {
		return theme.MutedText().Render("No lines yet. Keys 1–4 toggle built-in sources (Docker, PM2, journal, syslog). Connect via server list first.")
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString(m.styleLine(ln))
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m *Model) styleLine(ln logLine) string {
	raw := ln.Raw
	prefix := lipgloss.NewStyle().Foreground(theme.Current.Secondary).Render(fmt.Sprintf("[%d:%s] ", ln.PresetID, shortSource(ln.Source)))
	body := m.highlightLine(raw)
	return prefix + body
}

func shortSource(s string) string {
	if len(s) <= 18 {
		return s
	}
	return s[:15] + "..."
}

func (m *Model) highlightLine(s string) string {
	lower := strings.ToLower(s)
	levelStyle := lipgloss.NewStyle()
	switch {
	case strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") ||
		strings.Contains(lower, "critical") || strings.Contains(lower, "crit"):
		levelStyle = levelStyle.Foreground(theme.Current.Critical).Bold(true)
	case strings.Contains(lower, "error") || strings.Contains(lower, " err"):
		levelStyle = levelStyle.Foreground(theme.Current.Error)
	case strings.Contains(lower, "warn") || strings.Contains(lower, "warning"):
		levelStyle = levelStyle.Foreground(theme.Current.Warning)
	default:
		levelStyle = levelStyle.Foreground(theme.Current.Text)
	}

	if m.searchRe == nil {
		return levelStyle.Render(s)
	}

	matches := m.searchRe.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return levelStyle.Render(s)
	}

	highlight := lipgloss.NewStyle().
		Background(theme.Current.Surface).
		Foreground(theme.Current.Accent).
		Bold(true)

	var out strings.Builder
	last := 0
	for _, idx := range matches {
		if idx[0] > last {
			out.WriteString(levelStyle.Render(s[last:idx[0]]))
		}
		out.WriteString(highlight.Render(s[idx[0]:idx[1]]))
		last = idx[1]
	}
	if last < len(s) {
		out.WriteString(levelStyle.Render(s[last:]))
	}
	return out.String()
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("Logs · SSH stream")
	meta := theme.SubtitleStyle().Width(m.width).Render(m.metaLine())
	header := lipgloss.JoinVertical(lipgloss.Left, title, meta)

	var searchRow string
	if m.searchMode {
		searchRow = lipgloss.NewStyle().PaddingLeft(1).Render(m.searchInput.View())
	}

	vp := theme.PanelStyle().Width(m.width).Height(m.viewport.Height + 2).Render(m.viewport.View())

	help := components.NewHelpBar(
		components.KeyBinding{Key: "1-4", Desc: "toggle source"},
		components.KeyBinding{Key: "f", Desc: "follow"},
		components.KeyBinding{Key: "p", Desc: "pause/resume"},
		components.KeyBinding{Key: "/", Desc: "regex filter"},
		components.KeyBinding{Key: "ctrl+u", Desc: "clear filter"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width

	extra := ""
	if m.searchErr != "" {
		extra = "\n" + theme.ErrorText().Render("  regex: "+m.searchErr)
	} else if m.searchRaw != "" {
		extra = "\n" + theme.MutedText().Render("  filter: "+m.searchRaw)
	}

	if searchRow != "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, searchRow+extra, vp, help.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, header+extra, vp, help.View())
}

func (m *Model) metaLine() string {
	parts := []string{
		fmt.Sprintf("follow=%v", m.follow),
		fmt.Sprintf("paused=%v", m.paused),
		fmt.Sprintf("buf=%d/%d", len(m.buffer), m.maxLines),
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	return strings.Join(parts, " · ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
