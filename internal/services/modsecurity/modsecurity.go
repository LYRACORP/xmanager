package modsecurity

import (
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/waf"
	"gorm.io/gorm"
)

const serviceType = "modsecurity"

// Service installs ModSecurity + CRS on nginx via SSH.
type Service struct {
	services.BaseDeployer
	serverID uint
	onReload func()
}

// New creates the modsecurity service adapter.
func New(db *gorm.DB, serverID uint, onReload func()) *Service {
	return &Service{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID, onReload: onReload}
}

func (s *Service) Name() string { return serviceType }

func (s *Service) IsEnabled(exec *ssh.Executor) bool {
	st := waf.DetectModsec(exec)
	return st.Installed || s.InstanceEnabled(s.serverID, serviceType)
}

func (s *Service) Enable(exec *ssh.Executor, _ map[string]string) error {
	p := security.LoadPolicy(s.DB, s.serverID)
	p.WAFModsec = true
	if err := waf.InstallModsec(exec, p); err != nil {
		return err
	}
	_ = security.SavePolicy(s.DB, s.serverID, p)
	if s.onReload != nil {
		s.onReload()
	}
	return s.SaveInstance(s.serverID, serviceType, "running", `{}`)
}

func (s *Service) Disable(exec *ssh.Executor) error {
	if err := waf.RemoveModsec(exec); err != nil {
		return err
	}
	p := security.LoadPolicy(s.DB, s.serverID)
	p.WAFModsec = false
	_ = security.SavePolicy(s.DB, s.serverID, p)
	if s.onReload != nil {
		s.onReload()
	}
	return s.SaveInstance(s.serverID, serviceType, "stopped", `{}`)
}

func (s *Service) Status(exec *ssh.Executor) string {
	st := waf.DetectModsec(exec)
	if st.Installed {
		if st.Enabled {
			return "running: modsecurity active"
		}
		return "installed (not included in vhosts)"
	}
	if s.InstanceEnabled(s.serverID, serviceType) {
		return "marked on · not installed"
	}
	return "stopped"
}
