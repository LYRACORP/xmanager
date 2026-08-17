package packages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/recipes"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type mode int

const (
	modeBrowse mode = iota
	modeConfirm
	modeRunning
	modeDone
)

type Model struct {
	ctx       *shared.AppContext
	items     []recipes.Recipe
	cursor    int
	mode      mode
	width     int
	height    int
	loadErr   string
	busy      bool
	progress  shared.WebPanelProgressState
	progCh    <-chan tea.Msg
	lastOut   string
	lastOK    bool
	lastCreds string
	message   string
}

type recipesLoadedMsg struct {
	items []recipes.Recipe
	err   error
}

type recipeDoneMsg struct {
	ok    bool
	out   string
	creds string
	err   error
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx}
}

func (m *Model) Name() string { return "Install Packages" }

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeConfirm:
		return []components.KeyBinding{
			{Key: "y", Desc: "install"},
			{Key: "n/esc", Desc: "cancel"},
		}
	case modeRunning:
		return []components.KeyBinding{
			{Key: "…", Desc: "running"},
		}
	case modeDone:
		return []components.KeyBinding{
			{Key: "enter/esc", Desc: "back to list"},
		}
	default:
		return []components.KeyBinding{
			{Key: "↑↓", Desc: "select"},
			{Key: "enter", Desc: "install"},
			{Key: "esc", Desc: "back"},
		}
	}
}

func (m *Model) OnNavigate(_ map[string]interface{}) {
	m.mode = modeBrowse
	m.message = ""
	m.lastOut = ""
	m.progress.Reset()
}

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m *Model) Init() tea.Cmd { return m.load() }

func (m *Model) load() tea.Cmd {
	return func() tea.Msg {
		items, err := recipes.All()
		return recipesLoadedMsg{items: items, err: err}
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case recipesLoadedMsg:
		if msg.err != nil {
			m.loadErr = msg.err.Error()
			return m, nil
		}
		m.items = msg.items
		m.loadErr = ""
		if m.cursor >= len(m.items) && len(m.items) > 0 {
			m.cursor = len(m.items) - 1
		}
		return m, nil

	case shared.WebPanelProgressMsg:
		m.progress.Apply(msg)
		return m, shared.WaitMsg(m.progCh)

	case shared.ProgressNetTickMsg:
		if !m.progress.Active {
			return m, nil
		}
		m.progress.SampleNet()
		return m, shared.TickProgressNet()

	case recipeDoneMsg:
		m.busy = false
		m.progCh = nil
		m.progress.Reset()
		m.mode = modeDone
		m.lastOK = msg.ok
		m.lastOut = msg.out
		m.lastCreds = msg.creds
		if msg.err != nil && msg.out == "" {
			m.lastOut = msg.err.Error()
		}
		if msg.ok {
			return m, shared.PlayFinishSound()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	if m.busy || m.mode == modeRunning {
		return m, nil
	}
	switch m.mode {
	case modeDone:
		switch msg.String() {
		case "esc", "enter", " ", "q":
			m.mode = modeBrowse
			m.lastOut = ""
			m.lastCreds = ""
			return m, nil
		}
		return m, nil
	case modeConfirm:
		switch msg.String() {
		case "y", "Y":
			return m, m.startInstall()
		case "n", "N", "esc":
			m.mode = modeBrowse
			return m, nil
		}
		return m, nil
	default:
		switch msg.String() {
		case "esc", "q", "b":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
			return m, nil
		case "enter":
			if m.ctx.ServerID == 0 || len(m.items) == 0 {
				m.message = "Connect to a server first."
				return m, nil
			}
			m.mode = modeConfirm
			return m, nil
		}
		return m, nil
	}
}

func (m *Model) startInstall() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		m.mode = modeBrowse
		return nil
	}
	r := m.items[m.cursor]
	m.mode = modeRunning
	m.busy = true
	m.progress.Start("Installing " + r.Name)
	m.message = ""

	ch := make(chan tea.Msg, 32)
	m.progCh = ch
	serverID := m.ctx.ServerID
	go func() {
		defer close(ch)
		exec, ok := m.ctx.Pool.GetExecutor(serverID)
		if !ok {
			ch <- recipeDoneMsg{ok: false, err: fmt.Errorf("not connected — open server from fleet")}
			return
		}
		var srv storage.Server
		user, pass := "", ""
		if m.ctx.DB != nil && m.ctx.DB.First(&srv, serverID).Error == nil {
			user, pass = srv.User, srv.Password
		}
		runner := recipes.Runner{Exec: exec, User: user, Password: pass}
		res := runner.Run(r, func(pct float64, detail string) {
			ch <- shared.WebPanelProgressMsg{Pct: pct, Detail: detail}
		})
		ch <- recipeDoneMsg{ok: res.OK, out: res.Output, creds: res.Creds, err: res.Err}
	}()
	return tea.Batch(shared.WaitMsg(ch), shared.TickProgressNet())
}

func (m *Model) View() string {
	switch m.mode {
	case modeConfirm:
		return m.viewConfirm()
	case modeRunning:
		return m.viewRunning()
	case modeDone:
		return m.viewDone()
	default:
		return m.viewBrowse()
	}
}

func (m *Model) frame(body string) string {
	return components.ScreenFrame{
		Title:    "Install Packages",
		Subtitle: "bash setup recipes on this server",
		Width:    m.width,
		Body:     body,
	}.View()
}

func (m *Model) viewBrowse() string {
	if m.loadErr != "" {
		return m.frame(theme.ErrorText().Render(m.loadErr))
	}
	if m.ctx.ServerID == 0 {
		return m.frame(theme.WarningText().Render("Connect to a server first."))
	}
	var lines []string
	for i, r := range m.items {
		mark := "  "
		if i == m.cursor {
			mark = theme.KeyStyle().Render("> ")
		}
		req := ""
		if len(r.Requires) > 0 {
			req = theme.MutedText().Render("  needs: " + strings.Join(r.Requires, ", "))
		}
		title := r.Name
		if i == m.cursor {
			title = theme.TitleStyle().Render(r.Name)
		}
		lines = append(lines, mark+title+req)
		desc := "    " + theme.MutedText().Render(r.Description)
		if i == m.cursor {
			lines = append(lines, desc)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, theme.MutedText().Render("No recipes found."))
	}
	body := strings.Join(lines, "\n")
	if m.message != "" {
		body += "\n\n" + theme.WarningText().Render(m.message)
	}
	return m.frame(body)
}

func (m *Model) selected() (recipes.Recipe, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return recipes.Recipe{}, false
	}
	return m.items[m.cursor], true
}

func (m *Model) viewConfirm() string {
	r, ok := m.selected()
	if !ok {
		return m.frame("nothing selected")
	}
	var stepLines []string
	for _, s := range r.Steps {
		stepLines = append(stepLines, theme.MutedText().Render("  • "+s.Name))
	}
	warn := ""
	if len(r.Requires) > 0 {
		warn = "\n" + theme.WarningText().Render("Requires: "+strings.Join(r.Requires, ", "))
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		theme.TitleStyle().Render(r.Name),
		theme.MutedText().Render(r.Description),
		warn,
		"",
		strings.Join(stepLines, "\n"),
		"",
		theme.WarningText().Render("Run this install on the connected server? (y/n)"),
	)
	return m.frame(body)
}

func (m *Model) viewRunning() string {
	prog := m.progress.View(m.width)
	hint := theme.MutedText().Render("This may take several minutes (apt / compile / download)…")
	return m.frame(lipgloss.JoinVertical(lipgloss.Left, prog, "", hint))
}

func (m *Model) viewDone() string {
	status := theme.ErrorText().Render("Failed")
	if m.lastOK {
		status = theme.SuccessText().Render("Completed")
	}
	parts := []string{status, ""}
	if m.lastCreds != "" {
		parts = append(parts,
			theme.WarningText().Render("Generated credentials — save these:"),
			theme.MutedText().Render(m.lastCreds),
			"",
		)
	}
	out := m.lastOut
	max := m.height - 12
	if max < 8 {
		max = 8
	}
	olines := strings.Split(out, "\n")
	if len(olines) > max {
		olines = olines[len(olines)-max:]
		out = "…\n" + strings.Join(olines, "\n")
	}
	parts = append(parts, theme.MutedText().Render(out), "", theme.MutedText().Render("Enter / Esc: return"))
	return m.frame(lipgloss.JoinVertical(lipgloss.Left, parts...))
}
