// Package databasus deploys Databasus — self-hosted database backup tool.
// See https://databasus.com/
package databasus

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "databasus"
const dir = "/opt/xmanager/services/databasus"

type Databasus struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Databasus {
	return &Databasus{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (d *Databasus) Name() string { return serviceType }

func (d *Databasus) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "databasus") || d.InstanceEnabled(d.serverID, serviceType)
}

func (d *Databasus) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "4005"
	}

	compose := fmt.Sprintf(`services:
  databasus:
    image: databasus/databasus:latest
    container_name: databasus
    restart: unless-stopped
    ports:
      - "%s:4005"
    volumes:
      - databasus_data:/databasus-data
    healthcheck:
      test: ["CMD", "databasus", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 60s
volumes:
  databasus_data:
`, port)

	if err := d.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("databasus enable: %w", err)
	}
	return d.SaveInstance(d.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s"}`, port))
}

func (d *Databasus) Disable(exec *ssh.Executor) error {
	if err := d.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("databasus disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f databasus 2>/dev/null || true")
	return d.SaveInstance(d.serverID, serviceType, "stopped", "")
}

func (d *Databasus) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "databasus")
}
