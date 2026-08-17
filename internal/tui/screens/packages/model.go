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

type confirmKind int

const (
	confirmInstall confirmKind = iota
	confirmUninstall
	confirmUndoAll
)

type Model struct {
	ctx       *shared.AppContext
	items     []recipes.Recipe
	installed map[string]storage.RecipeInstall
	cursor    int
	mode      mode
	confirm   confirmKind
	undoList  []recipes.Recipe
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
	items     []recipes.Recipe
	installed map[string]storage.RecipeInstall
	err       error
}

type recipeDoneMsg struct {
	ok           bool
	out          string
	creds        string
	err          error
	installedIDs []string
	removedIDs   []string
}

func New(ctx *shared.AppContext) *Model {
	return &Model{ctx: ctx, installed: map[string]storage.RecipeInstall{}}
}

func (m *Model) Name() string { return "Install Packages" }

func (m *Model) KeyBindings() []components.KeyBinding {
	switch m.mode {
	case modeConfirm:
		return []components.KeyBinding{
			{Key: "y", Desc: "confirm"},
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
			{Key: "u", Desc: "uninstall"},
			{Key: "U", Desc: "undo all"},
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
	serverID := m.ctx.ServerID
	db := m.ctx.DB
	return func() tea.Msg {
		items, err := recipes.All()
		if err != nil {
			return recipesLoadedMsg{err: err}
		}
		installed := map[string]storage.RecipeInstall{}
		rows, lerr := storage.ListRecipeInstalls(db, serverID)
		if lerr != nil {
			return recipesLoadedMsg{items: items, installed: installed, err: lerr}
		}
		for _, row := range rows {
			installed[row.RecipeID] = row
		}
		return recipesLoadedMsg{items: items, installed: installed}
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case recipesLoadedMsg:
		if msg.err != nil && len(msg.items) == 0 {
			m.loadErr = msg.err.Error()
			return m, nil
		}
		m.items = msg.items
		if msg.installed != nil {
			m.installed = msg.installed
		}
		m.loadErr = ""
		if msg.err != nil {
			m.message = msg.err.Error()
		}
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
		m.applyInstallRecords(msg)
		if msg.ok {
			return m, shared.PlayFinishSound()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) applyInstallRecords(msg recipeDoneMsg) {
	if m.ctx.DB == nil || m.ctx.ServerID == 0 {
		return
	}
	for _, id := range msg.installedIDs {
		_ = storage.UpsertRecipeInstall(m.ctx.DB, m.ctx.ServerID, id)
		m.installed[id] = storage.RecipeInstall{ServerID: m.ctx.ServerID, RecipeID: id}
	}
	for _, id := range msg.removedIDs {
		_ = storage.DeleteRecipeInstall(m.ctx.DB, m.ctx.ServerID, id)
		delete(m.installed, id)
	}
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
			return m, m.load()
		}
		return m, nil
	case modeConfirm:
		switch msg.String() {
		case "y", "Y":
			switch m.confirm {
			case confirmUninstall:
				return m, m.startUninstall()
			case confirmUndoAll:
				return m, m.startUndoAll()
			default:
				return m, m.startInstall()
			}
		case "n", "N", "esc":
			m.mode = modeBrowse
			m.undoList = nil
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
			m.confirm = confirmInstall
			m.mode = modeConfirm
			return m, nil
		case "u":
			if m.ctx.ServerID == 0 || len(m.items) == 0 {
				m.message = "Connect to a server first."
				return m, nil
			}
			r, ok := m.selected()
			if !ok || !r.CanUninstall() {
				m.message = "This recipe has no uninstall steps."
				return m, nil
			}
			m.confirm = confirmUninstall
			m.mode = modeConfirm
			return m, nil
		case "U":
			if m.ctx.ServerID == 0 {
				m.message = "Connect to a server first."
				return m, nil
			}
			m.undoList = m.uninstallableInstalled()
			if len(m.undoList) == 0 {
				m.message = "No TUI-installed packages to undo on this server."
				return m, nil
			}
			m.confirm = confirmUndoAll
			m.mode = modeConfirm
			return m, nil
		}
		return m, nil
	}
}

func (m *Model) uninstallableInstalled() []recipes.Recipe {
	refs := make([]recipes.InstalledRef, 0, len(m.installed))
	for _, row := range m.installed {
		refs = append(refs, recipes.InstalledRef{
			RecipeID:  row.RecipeID,
			Installed: row.CreatedAt.UnixNano(),
		})
	}
	return recipes.UninstallOrder(refs, m.items)
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
		runner, err := m.runner(serverID)
		if err != nil {
			ch <- recipeDoneMsg{ok: false, err: err}
			return
		}
		res := runner.Run(r, func(pct float64, detail string) {
			ch <- shared.WebPanelProgressMsg{Pct: pct, Detail: detail}
		})
		msg := recipeDoneMsg{ok: res.OK, out: res.Output, creds: res.Creds, err: res.Err}
		if res.OK {
			msg.installedIDs = []string{r.ID}
		}
		ch <- msg
	}()
	return tea.Batch(shared.WaitMsg(ch), shared.TickProgressNet())
}

func (m *Model) startUninstall() tea.Cmd {
	r, ok := m.selected()
	if !ok {
		m.mode = modeBrowse
		return nil
	}
	m.mode = modeRunning
	m.busy = true
	m.progress.Start("Uninstalling " + r.Name)
	m.message = ""

	ch := make(chan tea.Msg, 32)
	m.progCh = ch
	serverID := m.ctx.ServerID
	go func() {
		defer close(ch)
		runner, err := m.runner(serverID)
		if err != nil {
			ch <- recipeDoneMsg{ok: false, err: err}
			return
		}
		res := runner.Uninstall(r, func(pct float64, detail string) {
			ch <- shared.WebPanelProgressMsg{Pct: pct, Detail: detail}
		})
		msg := recipeDoneMsg{ok: res.OK, out: res.Output, err: res.Err}
		if res.OK {
			msg.removedIDs = []string{r.ID}
		}
		ch <- msg
	}()
	return tea.Batch(shared.WaitMsg(ch), shared.TickProgressNet())
}

func (m *Model) startUndoAll() tea.Cmd {
	list := m.undoList
	if len(list) == 0 {
		m.mode = modeBrowse
		return nil
	}
	m.mode = modeRunning
	m.busy = true
	m.progress.Start("Undo all packages")
	m.message = ""

	ch := make(chan tea.Msg, 32)
	m.progCh = ch
	serverID := m.ctx.ServerID
	go func() {
		defer close(ch)
		runner, err := m.runner(serverID)
		if err != nil {
			ch <- recipeDoneMsg{ok: false, err: err}
			return
		}
		var log strings.Builder
		var removed []string
		n := len(list)
		for i, r := range list {
			base := float64(i) / float64(n)
			span := 1.0 / float64(n)
			fmt.Fprintf(&log, "==> uninstall %s\n", r.Name)
			res := runner.Uninstall(r, func(pct float64, detail string) {
				ch <- shared.WebPanelProgressMsg{Pct: base + pct*span, Detail: r.Name + ": " + detail}
			})
			log.WriteString(res.Output)
			log.WriteString("\n")
			if !res.OK {
				ch <- recipeDoneMsg{
					ok:         false,
					out:        log.String(),
					err:        res.Err,
					removedIDs: removed,
				}
				return
			}
			removed = append(removed, r.ID)
		}
		ch <- recipeDoneMsg{ok: true, out: log.String(), removedIDs: removed}
	}()
	return tea.Batch(shared.WaitMsg(ch), shared.TickProgressNet())
}

func (m *Model) runner(serverID uint) (recipes.Runner, error) {
	exec, ok := m.ctx.Pool.GetExecutor(serverID)
	if !ok {
		return recipes.Runner{}, fmt.Errorf("not connected — open server from fleet")
	}
	var srv storage.Server
	user, pass := "", ""
	if m.ctx.DB != nil && m.ctx.DB.First(&srv, serverID).Error == nil {
		user, pass = srv.User, srv.Password
	}
	return recipes.Runner{Exec: exec, User: user, Password: pass}, nil
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
		Subtitle: "install or uninstall bash setup recipes on this server",
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
		badge := ""
		if _, ok := m.installed[r.ID]; ok {
			badge = " " + theme.SuccessText().Render("[installed]")
		} else if !r.CanUninstall() {
			badge = " " + theme.MutedText().Render("[one-shot]")
		}
		req := ""
		if len(r.Requires) > 0 {
			req = theme.MutedText().Render("  needs: " + strings.Join(r.Requires, ", "))
		}
		title := r.Name
		if i == m.cursor {
			title = theme.TitleStyle().Render(r.Name)
		}
		lines = append(lines, mark+title+badge+req)
		if i == m.cursor {
			lines = append(lines, "    "+theme.MutedText().Render(r.Description))
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
	switch m.confirm {
	case confirmUninstall:
		return m.viewConfirmUninstall()
	case confirmUndoAll:
		return m.viewConfirmUndoAll()
	default:
		return m.viewConfirmInstall()
	}
}

func (m *Model) viewConfirmInstall() string {
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

func (m *Model) viewConfirmUninstall() string {
	r, ok := m.selected()
	if !ok {
		return m.frame("nothing selected")
	}
	var stepLines []string
	for _, s := range r.UninstallSteps {
		stepLines = append(stepLines, theme.MutedText().Render("  • "+s.Name))
	}
	parts := []string{
		theme.TitleStyle().Render("Uninstall " + r.Name),
		theme.MutedText().Render(r.Description),
		"",
		strings.Join(stepLines, "\n"),
		"",
	}
	if r.Destructive {
		parts = append(parts, theme.ErrorText().Render("This removes data, containers, or compiled installs. It cannot be undone."), "")
	}
	parts = append(parts, theme.WarningText().Render("Uninstall this recipe on the connected server? (y/n)"))
	return m.frame(lipgloss.JoinVertical(lipgloss.Left, parts...))
}

func (m *Model) viewConfirmUndoAll() string {
	var names []string
	destructive := false
	for _, r := range m.undoList {
		names = append(names, theme.MutedText().Render("  • "+r.Name))
		if r.Destructive {
			destructive = true
		}
	}
	parts := []string{
		theme.TitleStyle().Render("Undo all TUI-installed packages"),
		theme.MutedText().Render("Runs uninstall in reverse order (dependents first)."),
		"",
		strings.Join(names, "\n"),
		"",
	}
	if destructive {
		parts = append(parts, theme.ErrorText().Render("Includes destructive recipes (Docker/Postgres/Portainer/Python). Data will be deleted."), "")
	}
	parts = append(parts, theme.WarningText().Render("Uninstall every listed package on this server? (y/n)"))
	return m.frame(lipgloss.JoinVertical(lipgloss.Left, parts...))
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
