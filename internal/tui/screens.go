package tui

import (
	"github.com/lyracorp/xmanager/internal/tui/screens/backup"
	"github.com/lyracorp/xmanager/internal/tui/screens/chat"
	"github.com/lyracorp/xmanager/internal/tui/screens/cronjobs"
	"github.com/lyracorp/xmanager/internal/tui/screens/dashboard"
	"github.com/lyracorp/xmanager/internal/tui/screens/database"
	"github.com/lyracorp/xmanager/internal/tui/screens/docker"
	"github.com/lyracorp/xmanager/internal/tui/screens/errtrack"
	"github.com/lyracorp/xmanager/internal/tui/screens/fleet"
	ftpscreen "github.com/lyracorp/xmanager/internal/tui/screens/ftp"
	"github.com/lyracorp/xmanager/internal/tui/screens/logs"
	"github.com/lyracorp/xmanager/internal/tui/screens/packages"
	"github.com/lyracorp/xmanager/internal/tui/screens/pm2"
	"github.com/lyracorp/xmanager/internal/tui/screens/projects"
	"github.com/lyracorp/xmanager/internal/tui/screens/proxy"
	"github.com/lyracorp/xmanager/internal/tui/screens/recon"
	"github.com/lyracorp/xmanager/internal/tui/screens/scripts"
	"github.com/lyracorp/xmanager/internal/tui/screens/security"
	"github.com/lyracorp/xmanager/internal/tui/screens/servermap"
	"github.com/lyracorp/xmanager/internal/tui/screens/services"
	"github.com/lyracorp/xmanager/internal/tui/screens/settings"
	"github.com/lyracorp/xmanager/internal/tui/screens/uptime"
	"github.com/lyracorp/xmanager/internal/tui/screens/wizard"
	"github.com/lyracorp/xmanager/internal/tui/shared"
)

func NewFleetOverviewScreen(ctx *shared.AppContext) shared.Screen { return fleet.New(ctx) }
func NewDashboardScreen(ctx *shared.AppContext) shared.Screen     { return dashboard.New(ctx) }
func NewServerMapScreen(ctx *shared.AppContext) shared.Screen     { return servermap.New(ctx) }
func NewDockerScreen(ctx *shared.AppContext) shared.Screen        { return docker.New(ctx) }
func NewPM2Screen(ctx *shared.AppContext) shared.Screen           { return pm2.New(ctx) }
func NewLogsScreen(ctx *shared.AppContext) shared.Screen          { return logs.New(ctx) }
func NewChatScreen(ctx *shared.AppContext) shared.Screen          { return chat.New(ctx) }
func NewWizardScreen(ctx *shared.AppContext) shared.Screen        { return wizard.New(ctx) }
func NewErrTrackScreen(ctx *shared.AppContext) shared.Screen      { return errtrack.New(ctx) }
func NewDatabaseScreen(ctx *shared.AppContext) shared.Screen      { return database.New(ctx) }
func NewProxyScreen(ctx *shared.AppContext) shared.Screen         { return proxy.New(ctx) }
func NewBackupScreen(ctx *shared.AppContext) shared.Screen        { return backup.New(ctx) }
func NewSettingsScreen(ctx *shared.AppContext) shared.Screen      { return settings.New(ctx) }
func NewProjectsScreen(ctx *shared.AppContext) shared.Screen      { return projects.New(ctx) }
func NewCronJobsScreen(ctx *shared.AppContext) shared.Screen      { return cronjobs.New(ctx) }
func NewScriptsScreen(ctx *shared.AppContext) shared.Screen       { return scripts.New(ctx) }
func NewUptimeScreen(ctx *shared.AppContext) shared.Screen        { return uptime.New(ctx) }
func NewServicesScreen(ctx *shared.AppContext) shared.Screen      { return services.New(ctx) }
func NewReconScreen(ctx *shared.AppContext) shared.Screen         { return recon.New(ctx) }
func NewPackagesScreen(ctx *shared.AppContext) shared.Screen      { return packages.New(ctx) }
func NewFTPScreen(ctx *shared.AppContext) shared.Screen           { return ftpscreen.New(ctx) }
func NewSecurityScreen(ctx *shared.AppContext) shared.Screen      { return security.New(ctx) }
