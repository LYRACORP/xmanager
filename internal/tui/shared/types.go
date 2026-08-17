package shared

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/workflow"
	"gorm.io/gorm"
)

type ScreenID int

const (
	ScreenFleetOverview ScreenID = iota // 0 — default home
	ScreenDashboard
	ScreenServerMap
	ScreenDocker
	ScreenPM2
	ScreenLogs
	ScreenChat
	ScreenWizard
	ScreenErrTrack
	ScreenDatabase
	ScreenProxy
	ScreenBackup
	ScreenSettings
	ScreenProjects
	ScreenCronJobs
	ScreenScripts
	ScreenUptime
	ScreenServices
	ScreenRecon
	ScreenPackages
	ScreenFTP
	ScreenSecurity
	ScreenDateTime
	ScreenWorkflows
)

// Backward-compatible aliases.
const (
	ScreenServerList  = ScreenFleetOverview
	ScreenMultiServer = ScreenFleetOverview
)

func (s ScreenID) String() string {
	names := [...]string{
		"Fleet Overview", "Dashboard", "Server Map", "Docker",
		"PM2", "Logs", "AI Chat", "Setup Wizard",
		"Error Tracker", "Database", "Proxy", "Backup",
		"Settings", "Projects", "Cron Jobs", "Scripts",
		"Uptime", "Services", "Recon", "Install Packages", "FTP", "Security", "Date & time", "Workflows",
	}
	if int(s) < len(names) {
		return names[s]
	}
	return "Unknown"
}

// Screen is the interface every TUI screen must implement.
type Screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (Screen, tea.Cmd)
	View() string
	SetSize(width, height int)
	Name() string
	KeyBindings() []components.KeyBinding
	OnNavigate(params map[string]interface{})
}

// NavigateMsg tells the app to switch to a different screen.
type NavigateMsg struct {
	Screen   ScreenID
	ServerID uint
	Params   map[string]interface{}
}

// GoBackMsg tells the app to pop the screen stack.
type GoBackMsg struct{}

// ConnectServerMsg requests the app to connect to a specific server.
type ConnectServerMsg struct {
	ServerID uint
}

// ServerConnectedMsg is sent after a successful server connection.
type ServerConnectedMsg struct {
	ServerID uint
}

// AppContext holds shared dependencies injected into every screen.
type AppContext struct {
	Config   *config.Config
	DB       *gorm.DB
	Pool     *ssh.Pool
	ServerID uint
	Poller    *poller.Poller
	Catalog   *ops.Catalog
	Workflows *workflow.Engine
}
