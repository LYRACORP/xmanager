package umami

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "umami"
const dir = "/opt/xmanager/services/umami"

type Umami struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Umami {
	return &Umami{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (u *Umami) Name() string { return serviceType }

func (u *Umami) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "umami") || u.InstanceEnabled(u.serverID, serviceType)
}

func (u *Umami) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "3000"
	}
	dbPass := cfg["db_password"]
	if dbPass == "" {
		dbPass = "umamipassword"
	}
	appSecret := cfg["app_secret"]
	if appSecret == "" {
		appSecret = "changeme-random-secret"
	}

	compose := fmt.Sprintf(`services:
  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      - POSTGRES_DB=umami
      - POSTGRES_USER=umami
      - POSTGRES_PASSWORD=%s
    volumes:
      - umami_db:/var/lib/postgresql/data
  umami:
    image: ghcr.io/umami-software/umami:postgresql-latest
    container_name: umami
    restart: unless-stopped
    depends_on:
      - postgres
    environment:
      - DATABASE_URL=postgresql://umami:%s@postgres:5432/umami
      - DATABASE_TYPE=postgresql
      - APP_SECRET=%s
    ports:
      - "%s:3000"
volumes:
  umami_db:
`, dbPass, dbPass, appSecret, port)

	if err := u.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("umami enable: %w", err)
	}
	return u.SaveInstance(u.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s"}`, port))
}

func (u *Umami) Disable(exec *ssh.Executor) error {
	if err := u.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("umami disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f umami 2>/dev/null || true")
	return u.SaveInstance(u.serverID, serviceType, "stopped", "")
}

func (u *Umami) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "umami")
}
