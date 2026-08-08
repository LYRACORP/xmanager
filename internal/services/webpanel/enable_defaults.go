package webpanel

import (
	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/services/bugsink"
	"github.com/lyracorp/xmanager/internal/services/gitea"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/services/registry"
	"github.com/lyracorp/xmanager/internal/services/rustfs"
	"gorm.io/gorm"
)

// defaultRemoteStacks returns services that should be on by default when the
// node panel is installed on a server.
func defaultRemoteStacks(db *gorm.DB, serverID uint) []svcs.Service {
	return []svcs.Service{
		registry.New(db, serverID),
		gitea.New(db, serverID),
		rustfs.New(db, serverID),
		powerdns.New(db, serverID),
		mailinbox.New(db, serverID),
		bugsink.New(db, serverID),
	}
}
