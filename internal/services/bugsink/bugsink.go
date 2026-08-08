// Package bugsink deploys Bugsink — self-hosted Sentry-compatible error tracking.
// See https://www.bugsink.com/
package bugsink

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "bugsink"
const dir = "/opt/xmanager/services/bugsink"

type Bugsink struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Bugsink {
	return &Bugsink{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (b *Bugsink) Name() string { return serviceType }

func (b *Bugsink) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "bugsink") || b.InstanceEnabled(b.serverID, serviceType)
}

func (b *Bugsink) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "8000"
	}
	secretKey := cfg["secret_key"]
	if secretKey == "" {
		secretKey = randomSecret(48)
	}
	admin := cfg["admin"]
	if admin == "" {
		admin = "admin@localhost:admin"
	}

	// Single-container deploy per https://www.bugsink.com/ (SQLite by default).
	compose := fmt.Sprintf(`services:
  bugsink:
    image: bugsink/bugsink:latest
    container_name: bugsink
    restart: unless-stopped
    environment:
      - SECRET_KEY=%s
      - CREATE_SUPERUSER=%s
      - PORT=8000
    ports:
      - "%s:8000"
    volumes:
      - bugsink_data:/data
volumes:
  bugsink_data:
`, secretKey, admin, port)

	// Stop legacy GlitchTip/Sentry stack if present on this host.
	_, _ = exec.Run("cd /opt/xmanager/services/sentry && docker compose down 2>/dev/null || true")
	_, _ = exec.Run("docker rm -f glitchtip-web 2>/dev/null || true")

	if err := b.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("bugsink enable: %w", err)
	}
	return b.SaveInstance(b.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s","admin":"%s"}`, port, admin))
}

func (b *Bugsink) Disable(exec *ssh.Executor) error {
	if err := b.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("bugsink disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f bugsink 2>/dev/null || true")
	return b.SaveInstance(b.serverID, serviceType, "stopped", "")
}

func (b *Bugsink) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "bugsink")
}

func randomSecret(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "change-me-bugsink-secret-key-please"
	}
	return hex.EncodeToString(buf)
}
