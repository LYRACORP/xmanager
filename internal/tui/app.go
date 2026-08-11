package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
	"gorm.io/gorm"
)

type AppOptions struct {
	Config        *config.Config
	DB            *gorm.DB
	Pool          *ssh.Pool
	Poller        *poller.Poller
	InitialTarget string
}

type App struct {
	ctx         *shared.AppContext
	router      *Router
	screens     map[shared.ScreenID]shared.Screen
	statusBar   components.StatusBar
	helpOverlay components.HelpOverlay
	width       int
	height      int
	ready       bool
	showHelp    bool
}

func newApp(opts AppOptions) *App {
	theme.SetTheme(opts.Config.UI.Theme)

	pool := opts.Pool
	if pool == nil {
		pool = ssh.NewPool()
	}

	ctx := &shared.AppContext{
		Config: opts.Config,
		DB:     opts.DB,
		Pool:   pool,
		Poller: opts.Poller,
	}

	app := &App{
		ctx:       ctx,
		router:    NewRouter(),
		screens:   make(map[shared.ScreenID]shared.Screen),
		statusBar: components.NewStatusBar(),
	}

	app.initScreens()

	if opts.InitialTarget == "__setup__" {
		app.router.Reset(shared.ScreenWizard)
	}

	return app
}

func (a *App) initScreens() {
	a.screens[shared.ScreenFleetOverview] = NewFleetOverviewScreen(a.ctx)
	a.screens[shared.ScreenDashboard] = NewDashboardScreen(a.ctx)
	a.screens[shared.ScreenServerMap] = NewServerMapScreen(a.ctx)
	a.screens[shared.ScreenDocker] = NewDockerScreen(a.ctx)
	a.screens[shared.ScreenPM2] = NewPM2Screen(a.ctx)
	a.screens[shared.ScreenLogs] = NewLogsScreen(a.ctx)
	a.screens[shared.ScreenChat] = NewChatScreen(a.ctx)
	a.screens[shared.ScreenWizard] = NewWizardScreen(a.ctx)
	a.screens[shared.ScreenErrTrack] = NewErrTrackScreen(a.ctx)
	a.screens[shared.ScreenDatabase] = NewDatabaseScreen(a.ctx)
	a.screens[shared.ScreenProxy] = NewProxyScreen(a.ctx)
	a.screens[shared.ScreenBackup] = NewBackupScreen(a.ctx)
	a.screens[shared.ScreenSettings] = NewSettingsScreen(a.ctx)
	a.screens[shared.ScreenProjects] = NewProjectsScreen(a.ctx)
	a.screens[shared.ScreenCronJobs] = NewCronJobsScreen(a.ctx)
	a.screens[shared.ScreenScripts] = NewScriptsScreen(a.ctx)
	a.screens[shared.ScreenUptime] = NewUptimeScreen(a.ctx)
	a.screens[shared.ScreenServices] = NewServicesScreen(a.ctx)
	a.screens[shared.ScreenRecon] = NewReconScreen(a.ctx)
	a.screens[shared.ScreenPackages] = NewPackagesScreen(a.ctx)
}

func (a *App) Init() tea.Cmd {
	screen := a.screens[a.router.Current()]
	return screen.Init()
}

func (a *App) contentHeight() int {
	return layout.ContentHeight(a.height)
}

func (a *App) globalBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "?", Desc: "help"},
		{Key: "ctrl+f", Desc: "fleet"},
		{Key: "ctrl+a", Desc: "AI chat"},
		{Key: "esc", Desc: "back"},
	}
}

func (a *App) mergedFooterBindings() []components.KeyBinding {
	screen := a.screens[a.router.Current()]
	bindings := screen.KeyBindings()
	bindings = append(bindings, a.globalBindings()...)
	return bindings
}

func (a *App) refreshStatusBar() {
	a.statusBar.Width = a.width
	if a.ctx.ServerID == 0 {
		a.statusBar.ServerName = ""
		a.statusBar.ServerHost = ""
		a.statusBar.Connected = false
		return
	}
	var srv storage.Server
	if err := a.ctx.DB.First(&srv, a.ctx.ServerID).Error; err != nil {
		return
	}
	_, ok := a.ctx.Pool.GetExecutor(a.ctx.ServerID)
	a.statusBar.ServerName = srv.Name
	a.statusBar.ServerHost = fmt.Sprintf("%s:%d", srv.Host, srv.Port)
	a.statusBar.Connected = ok
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.refreshStatusBar()
		ch := a.contentHeight()
		for _, s := range a.screens {
			s.SetSize(a.width, ch)
		}
		a.helpOverlay.Width = a.width
		a.helpOverlay.Height = a.height
		return a, nil

	case tea.KeyMsg:
		if a.showHelp {
			switch msg.String() {
			case "?", "esc":
				a.showHelp = false
				return a, nil
			}
			return a, nil
		}

		switch msg.String() {
		case "ctrl+c":
			a.ctx.Pool.DisconnectAll()
			if a.ctx.Poller != nil {
				a.ctx.Poller.Stop()
			}
			return a, tea.Quit
		case "?":
			a.showHelp = true
			a.helpOverlay = components.NewHelpOverlay(
				a.router.Current().String()+" — shortcuts",
				a.mergedFooterBindings(),
			)
			a.helpOverlay.Visible = true
			a.helpOverlay.Width = a.width
			a.helpOverlay.Height = a.height
			return a, nil
		case "ctrl+f":
			return a, a.replaceNavigate(shared.ScreenFleetOverview, nil)
		case "ctrl+a":
			return a, a.replaceNavigate(shared.ScreenChat, nil)
		}

	case shared.NavigateMsg:
		if msg.ServerID > 0 {
			a.ctx.ServerID = msg.ServerID
			a.refreshStatusBar()
		}
		return a, a.navigate(msg.Screen, msg.Params)

	case shared.GoBackMsg:
		prev := a.router.Pop()
		screen := a.screens[prev]
		a.refreshStatusBar()
		return a, screen.Init()

	case shared.ConnectServerMsg:
		a.ctx.ServerID = msg.ServerID
		a.refreshStatusBar()
		return a, a.navigate(shared.ScreenDashboard, nil)

	case shared.ServerConnectedMsg:
		a.refreshStatusBar()

	case poller.MetricsUpdatedMsg:
		// Forward to fleet overview so cards update in real time.
		if fleet, ok := a.screens[shared.ScreenFleetOverview]; ok {
			newFleet, cmd := fleet.Update(msg)
			a.screens[shared.ScreenFleetOverview] = newFleet
			return a, cmd
		}
	}

	current := a.router.Current()
	screen := a.screens[current]
	newScreen, cmd := screen.Update(msg)
	a.screens[current] = newScreen
	return a, cmd
}

func (a *App) View() string {
	if !a.ready {
		return theme.MutedText().Render("Loading XManager...")
	}

	screen := a.screens[a.router.Current()]
	header := a.renderHeader()
	status := a.statusBar.View()
	content := screen.View()
	helpBar := components.NewHelpBar(a.mergedFooterBindings()...)
	helpBar.Width = a.width
	footerView := helpBar.View()

	inner := lipgloss.JoinVertical(lipgloss.Left, header, status, content, footerView)
	base := theme.BackgroundStyle(a.width).MaxWidth(a.width).MaxHeight(a.height).Render(inner)

	if a.showHelp {
		overlay := a.helpOverlay.View()
		if overlay != "" {
			return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, overlay)
		}
	}
	return base
}

func (a *App) renderHeader() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Current.Text).
		Render("XManager")

	screenName := lipgloss.NewStyle().
		Foreground(theme.Current.TextDim).
		Render(fmt.Sprintf(" / %s", a.router.Current().String()))

	return theme.AppHeaderStyle(a.width).Render(title + screenName)
}

func (a *App) navigate(screen shared.ScreenID, params map[string]interface{}) tea.Cmd {
	a.router.Push(screen)
	s := a.screens[screen]
	s.SetSize(a.width, a.contentHeight())
	if params != nil {
		s.OnNavigate(params)
		a.screens[screen] = s
	}
	return s.Init()
}

func (a *App) replaceNavigate(screen shared.ScreenID, params map[string]interface{}) tea.Cmd {
	a.router.Replace(screen)
	s := a.screens[screen]
	s.SetSize(a.width, a.contentHeight())
	if params != nil {
		s.OnNavigate(params)
		a.screens[screen] = s
	}
	return s.Init()
}

func Run(opts AppOptions) error {
	app := newApp(opts)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
	if opts.Poller != nil {
		opts.Poller.SetOnMetric(func(msg poller.MetricsUpdatedMsg) {
			p.Send(msg)
		})
	}
	_, err := p.Run()
	return err
}
