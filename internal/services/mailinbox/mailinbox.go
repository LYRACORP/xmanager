// Package mailinbox deploys Stalwart Mail Server and talks to its management API
// (or an external Mail-in-a-Box /admin API when mode=mailinabox).
package mailinbox

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "mailinbox"
const dir = "/opt/xmanager/services/mailinbox"

type MailInbox struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *MailInbox {
	return &MailInbox{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (m *MailInbox) Name() string { return serviceType }

func (m *MailInbox) IsEnabled(exec *ssh.Executor) bool {
	cfg := LoadConfig(m.DB, m.serverID)
	if cfg.Mode == ModeMiaB {
		return m.InstanceEnabled(m.serverID, serviceType)
	}
	return services.ServiceUp(exec, dir, "stalwart") || m.InstanceEnabled(m.serverID, serviceType)
}

func (m *MailInbox) Enable(exec *ssh.Executor, cfg map[string]string) error {
	c := LoadConfig(m.DB, m.serverID)
	// Preserve generated admin password across re-enable unless explicitly overridden.
	if cfg != nil {
		if v := cfg["smtp_port"]; v != "" {
			c.SMTPPort = v
		}
		if v := cfg["imap_port"]; v != "" {
			c.IMAPPort = v
		}
		if v := cfg["https_port"]; v != "" {
			c.HTTPSPort = v
		}
		if v := cfg["hostname"]; v != "" {
			c.Hostname = v
		}
		if v := cfg["admin_user"]; v != "" {
			c.AdminUser = v
		}
		if v := cfg["admin_password"]; v != "" {
			c.AdminPassword = v
		}
		if v := cfg["api_base"]; v != "" {
			c.APIBase = v
		}
		if v := cfg["webmail_url"]; v != "" {
			c.WebmailURL = v
		}
		if v := cfg["mode"]; v != "" {
			c.Mode = v
		}
	}
	if c.Mode == "" {
		c.Mode = ModeStalwart
	}
	if c.HTTPSPort == "" || c.HTTPSPort == "8080" {
		c.HTTPSPort = "8085"
	}
	if c.AdminUser == "" {
		c.AdminUser = "admin"
	}
	if c.AdminPassword == "" {
		c.AdminPassword = randomHex(12)
	}

	// External Mail-in-a-Box: no local compose — just store API credentials.
	if c.Mode == ModeMiaB {
		if c.APIBase == "" {
			c.APIBase = "https://127.0.0.1/admin"
		}
		return m.SaveInstance(m.serverID, serviceType, "running", c.JSON())
	}

	err := m.writeAndUp(exec, c)
	if err == nil {
		return m.SaveInstance(m.serverID, serviceType, "running", c.JSON())
	}

	// Common failures: privileged SMTP :25 or port conflicts — retry with safer ports.
	if c.SMTPPort == "25" {
		alt := c
		alt.SMTPPort = "2525"
		if err2 := m.writeAndUp(exec, alt); err2 == nil {
			return m.SaveInstance(m.serverID, serviceType, "running", alt.JSON())
		} else {
			return fmt.Errorf("mailinbox enable: %v (retry: %w)", err, err2)
		}
	}
	return fmt.Errorf("mailinbox enable: %w", err)
}

func (m *MailInbox) writeAndUp(exec *ssh.Executor, c Config) error {
	compose := fmt.Sprintf(`services:
  stalwart-mail:
    image: stalwartlabs/stalwart:latest
    container_name: stalwart-mail
    restart: unless-stopped
    hostname: %s
    environment:
      - STALWART_RECOVERY_ADMIN=%s:%s
    ports:
      - "%s:25"
      - "587:587"
      - "465:465"
      - "%s:143"
      - "993:993"
      - "4190:4190"
      - "%s:8080"
    volumes:
      - stalwart_data:/opt/stalwart
volumes:
  stalwart_data:
`, c.Hostname, c.AdminUser, c.AdminPassword, c.SMTPPort, c.IMAPPort, c.HTTPSPort)
	return m.WriteCompose(exec, dir, compose)
}

func (m *MailInbox) Disable(exec *ssh.Executor) error {
	cfg := LoadConfig(m.DB, m.serverID)
	if cfg.Mode != ModeMiaB {
		if err := m.ComposeDown(exec, dir); err != nil {
			return fmt.Errorf("mailinbox disable: %w", err)
		}
		_, _ = exec.Run("docker rm -f stalwart-mail 2>/dev/null || true")
	}
	return m.SaveInstance(m.serverID, serviceType, "stopped", "")
}

func (m *MailInbox) Status(exec *ssh.Executor) string {
	cfg := LoadConfig(m.DB, m.serverID)
	if cfg.Mode == ModeMiaB {
		if m.InstanceEnabled(m.serverID, serviceType) {
			return "configured (mail-in-a-box api)"
		}
		return "stopped"
	}
	return services.ContainerStatus(exec, "stalwart")
}

// API returns a mail provisioning client for this server.
func (m *MailInbox) API() *Client {
	return NewClient(LoadConfig(m.DB, m.serverID))
}
