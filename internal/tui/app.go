package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
	"gorm.io/gorm"
)

type AppOptions struct {
	Config        *config.Config
	DB            *gorm.DB
	InitialTarget string
}

type App struct {
	ctx       *shared.AppContext
	router    *Router
	screens   map[shared.ScreenID]shared.Screen
	helpBar   components.HelpBar
	width     int
	height    int
	ready     bool
}

func newApp(opts AppOptions) *App {
	theme.SetTheme(opts.Config.UI.Theme)

	ctx := &shared.AppContext{
		Config: opts.Config,
		DB:     opts.DB,
		Pool:   ssh.NewPool(),
	}

	app := &App{
		ctx:    ctx,
		router: NewRouter(),
		screens: make(map[shared.ScreenID]shared.Screen),
		helpBar: components.NewHelpBar(
			components.KeyBinding{Key: "?", Desc: "help"},
			components.KeyBinding{Key: "ctrl+s", Desc: "servers"},
			components.KeyBinding{Key: "ctrl+a", Desc: "AI chat"},
			components.KeyBinding{Key: "q/esc", Desc: "back"},
		),
	}

	app.initScreens()

	if opts.InitialTarget == "__setup__" {
		app.router.Reset(shared.ScreenWizard)
	}

	return app
}

func (a *App) initScreens() {
	a.screens[shared.ScreenServerList] = NewServerListScreen(a.ctx)
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
	a.screens[shared.ScreenMultiServer] = NewMultiServerScreen(a.ctx)
	a.screens[shared.ScreenSettings] = NewSettingsScreen(a.ctx)
}

func (a *App) Init() tea.Cmd {
	screen := a.screens[a.router.Current()]
	return screen.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.helpBar.Width = msg.Width
		a.ready = true
		contentHeight := a.height - 3
		for _, s := range a.screens {
			s.SetSize(a.width, contentHeight)
		}
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			a.ctx.Pool.DisconnectAll()
			return a, tea.Quit
		case "ctrl+s":
			return a, a.navigate(shared.ScreenServerList)
		case "ctrl+a":
			return a, a.navigate(shared.ScreenChat)
		}

	case shared.NavigateMsg:
		if msg.ServerID > 0 {
			a.ctx.ServerID = msg.ServerID
		}
		return a, a.navigate(msg.Screen)

	case shared.GoBackMsg:
		prev := a.router.Pop()
		screen := a.screens[prev]
		return a, screen.Init()

	case shared.ConnectServerMsg:
		a.ctx.ServerID = msg.ServerID
		return a, a.navigate(shared.ScreenDashboard)
	}

	current := a.router.Current()
	screen := a.screens[current]
	newScreen, cmd := screen.Update(msg)
	a.screens[current] = newScreen
	return a, cmd
}

func (a *App) View() string {
	if !a.ready {
		return "Loading XManager..."
	}

	screen := a.screens[a.router.Current()]
	header := a.renderHeader()
	content := screen.View()
	footer := a.helpBar.View()

	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

func (a *App) renderHeader() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Current.Primary).
		PaddingLeft(1)

	screenName := lipgloss.NewStyle().
		Foreground(theme.Current.TextDim).
		PaddingLeft(1).
		Render(fmt.Sprintf("/ %s", a.router.Current().String()))

	left := titleStyle.Render("XManager") + screenName

	return lipgloss.NewStyle().
		Background(theme.Current.Surface).
		Width(a.width).
		Padding(0, 1).
		Render(left)
}

func (a *App) navigate(screen shared.ScreenID) tea.Cmd {
	a.router.Push(screen)
	s := a.screens[screen]
	s.SetSize(a.width, a.height-3)
	return s.Init()
}

func Run(opts AppOptions) error {
	app := newApp(opts)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err := p.Run()
	return err
}
