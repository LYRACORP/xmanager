package mattermost

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "mattermost"
const dir = "/opt/xmanager/services/mattermost"

type Mattermost struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Mattermost {
	return &Mattermost{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (m *Mattermost) Name() string { return serviceType }

func (m *Mattermost) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "mattermost") || m.InstanceEnabled(m.serverID, serviceType)
}

func (m *Mattermost) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "8065"
	}
	dbPass := cfg["db_password"]
	if dbPass == "" {
		dbPass = "mmpassword"
	}
	siteURL := cfg["site_url"]
	if siteURL == "" {
		siteURL = "http://localhost:" + port
	}

	compose := fmt.Sprintf(`services:
  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      - POSTGRES_USER=mattermost
      - POSTGRES_PASSWORD=%s
      - POSTGRES_DB=mattermost
    volumes:
      - mm_postgres:/var/lib/postgresql/data
  mattermost:
    image: mattermost/mattermost-team-edition:latest
    container_name: mattermost
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      - MM_SQLSETTINGS_DRIVERNAME=postgres
      - MM_SQLSETTINGS_DATASOURCE=postgres://mattermost:%s@postgres:5432/mattermost?sslmode=disable
      - MM_SERVICESETTINGS_SITEURL=%s
    ports:
      - "%s:8065"
    volumes:
      - mm_data:/mattermost/data
      - mm_logs:/mattermost/logs
      - mm_config:/mattermost/config
volumes:
  mm_postgres:
  mm_data:
  mm_logs:
  mm_config:
`, dbPass, dbPass, siteURL, port)

	if err := m.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("mattermost enable: %w", err)
	}
	return m.SaveInstance(m.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s","site_url":"%s"}`, port, siteURL))
}

func (m *Mattermost) Disable(exec *ssh.Executor) error {
	if err := m.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("mattermost disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f mattermost 2>/dev/null || true")
	return m.SaveInstance(m.serverID, serviceType, "stopped", "")
}

func (m *Mattermost) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "mattermost")
}
