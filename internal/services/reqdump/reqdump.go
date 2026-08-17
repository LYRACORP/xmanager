package reqdump

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

// Service is a DB-only toggle for request capture (no Docker).
type Service struct {
	services.BaseDeployer
	serverID uint
	onReload func()
}

// New creates the reqdump service adapter.
func New(db *gorm.DB, serverID uint, onReload func()) *Service {
	return &Service{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID, onReload: onReload}
}

func (r *Service) Name() string { return reqdump.ServiceType }

func (r *Service) IsEnabled(_ *ssh.Executor) bool {
	return r.InstanceEnabled(r.serverID, reqdump.ServiceType)
}

func (r *Service) Enable(_ *ssh.Executor, cfg map[string]string) error {
	c := reqdump.LoadConfig(r.DB, r.serverID)
	c.Enabled = true
	if v, ok := cfg["dump_panel"]; ok {
		c.DumpPanel = v == "true" || v == "on" || v == "1"
	} else if !c.DumpPanel && !c.DumpNginx && !c.DumpHoneypot {
		c.DumpPanel = true
	}
	if v, ok := cfg["dump_nginx"]; ok {
		c.DumpNginx = v == "true" || v == "on" || v == "1"
	}
	if v, ok := cfg["dump_honeypot"]; ok {
		c.DumpHoneypot = v == "true" || v == "on" || v == "1"
	}
	if v, ok := cfg["honeypot_ports"]; ok && v != "" {
		c.HoneypotPorts = v
	}
	if err := reqdump.SaveConfig(r.DB, r.serverID, c); err != nil {
		return err
	}
	if r.onReload != nil {
		r.onReload()
	}
	return nil
}

func (r *Service) Disable(_ *ssh.Executor) error {
	c := reqdump.LoadConfig(r.DB, r.serverID)
	c.Enabled = false
	if err := reqdump.SaveConfig(r.DB, r.serverID, c); err != nil {
		return err
	}
	if r.onReload != nil {
		r.onReload()
	}
	return nil
}

func (r *Service) Status(_ *ssh.Executor) string {
	if !r.IsEnabled(nil) {
		return "stopped"
	}
	c := reqdump.LoadConfig(r.DB, r.serverID)
	var parts []string
	if c.DumpPanel {
		parts = append(parts, "panel")
	}
	if c.DumpNginx {
		parts = append(parts, "nginx")
	}
	if c.DumpHoneypot {
		parts = append(parts, "honeypot")
	}
	if len(parts) == 0 {
		return "enabled (no channels)"
	}
	return fmt.Sprintf("running: %v", parts)
}
