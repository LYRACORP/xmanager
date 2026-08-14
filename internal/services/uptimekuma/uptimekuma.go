// Package uptimekuma deploys Uptime Kuma — self-hosted uptime monitoring.
// See https://github.com/louislam/uptime-kuma
package uptimekuma

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "uptimekuma"
const dir = "/opt/xmanager/services/uptimekuma"

type UptimeKuma struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *UptimeKuma {
	return &UptimeKuma{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (u *UptimeKuma) Name() string { return serviceType }

func (u *UptimeKuma) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "uptime-kuma") || u.InstanceEnabled(u.serverID, serviceType)
}

func (u *UptimeKuma) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "3001"
	}

	compose := fmt.Sprintf(`services:
  uptime-kuma:
    image: louislam/uptime-kuma:2
    container_name: uptime-kuma
    restart: unless-stopped
    ports:
      - "%s:3001"
    volumes:
      - uptime_kuma_data:/app/data
volumes:
  uptime_kuma_data:
`, port)

	if err := u.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("uptimekuma enable: %w", err)
	}
	return u.SaveInstance(u.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s"}`, port))
}

func (u *UptimeKuma) Disable(exec *ssh.Executor) error {
	if err := u.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("uptimekuma disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f uptime-kuma 2>/dev/null || true")
	return u.SaveInstance(u.serverID, serviceType, "stopped", "")
}

func (u *UptimeKuma) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "uptime-kuma")
}
