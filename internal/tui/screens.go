package tui

import (
	"github.com/lyracorp/xmanager/internal/tui/screens/backup"
	"github.com/lyracorp/xmanager/internal/tui/screens/chat"
	"github.com/lyracorp/xmanager/internal/tui/screens/dashboard"
	"github.com/lyracorp/xmanager/internal/tui/screens/database"
	"github.com/lyracorp/xmanager/internal/tui/screens/docker"
	"github.com/lyracorp/xmanager/internal/tui/screens/errtrack"
	"github.com/lyracorp/xmanager/internal/tui/screens/logs"
	"github.com/lyracorp/xmanager/internal/tui/screens/multiserver"
	"github.com/lyracorp/xmanager/internal/tui/screens/pm2"
	"github.com/lyracorp/xmanager/internal/tui/screens/proxy"
	"github.com/lyracorp/xmanager/internal/tui/screens/serverlist"
	"github.com/lyracorp/xmanager/internal/tui/screens/servermap"
	"github.com/lyracorp/xmanager/internal/tui/screens/settings"
	"github.com/lyracorp/xmanager/internal/tui/screens/wizard"
	"github.com/lyracorp/xmanager/internal/tui/shared"
)

func NewServerListScreen(ctx *shared.AppContext) shared.Screen  { return serverlist.New(ctx) }
func NewDashboardScreen(ctx *shared.AppContext) shared.Screen    { return dashboard.New(ctx) }
func NewServerMapScreen(ctx *shared.AppContext) shared.Screen    { return servermap.New(ctx) }
func NewDockerScreen(ctx *shared.AppContext) shared.Screen       { return docker.New(ctx) }
func NewPM2Screen(ctx *shared.AppContext) shared.Screen          { return pm2.New(ctx) }
func NewLogsScreen(ctx *shared.AppContext) shared.Screen         { return logs.New(ctx) }
func NewChatScreen(ctx *shared.AppContext) shared.Screen         { return chat.New(ctx) }
func NewWizardScreen(ctx *shared.AppContext) shared.Screen       { return wizard.New(ctx) }
func NewErrTrackScreen(ctx *shared.AppContext) shared.Screen     { return errtrack.New(ctx) }
func NewDatabaseScreen(ctx *shared.AppContext) shared.Screen     { return database.New(ctx) }
func NewProxyScreen(ctx *shared.AppContext) shared.Screen        { return proxy.New(ctx) }
func NewBackupScreen(ctx *shared.AppContext) shared.Screen       { return backup.New(ctx) }
func NewMultiServerScreen(ctx *shared.AppContext) shared.Screen  { return multiserver.New(ctx) }
func NewSettingsScreen(ctx *shared.AppContext) shared.Screen      { return settings.New(ctx) }
