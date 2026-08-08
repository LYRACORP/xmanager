// Package mailinbox deploys Stalwart Mail Server — a modern all-in-one mail server
// (SMTP, IMAP, JMAP) with a web admin UI.
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
	return services.ServiceUp(exec, dir, "stalwart") || m.InstanceEnabled(m.serverID, serviceType)
}

func (m *MailInbox) Enable(exec *ssh.Executor, cfg map[string]string) error {
	if cfg == nil {
		cfg = map[string]string{}
	}
	smtpPort := cfg["smtp_port"]
	if smtpPort == "" {
		smtpPort = "25"
	}
	imapPort := cfg["imap_port"]
	if imapPort == "" {
		imapPort = "143"
	}
	// Never default to 8080 — that collides with the node web panel.
	httpsPort := cfg["https_port"]
	if httpsPort == "" {
		httpsPort = "8085"
	}
	hostname := cfg["hostname"]
	if hostname == "" {
		hostname = "mail.example.com"
	}

	err := m.writeAndUp(exec, hostname, smtpPort, imapPort, httpsPort)
	if err == nil {
		return m.SaveInstance(m.serverID, serviceType, "running",
			fmt.Sprintf(`{"smtp_port":"%s","imap_port":"%s","https_port":"%s","hostname":"%s"}`,
				smtpPort, imapPort, httpsPort, hostname))
	}

	// Common failures: privileged SMTP :25 or port conflicts — retry with safer ports.
	if smtpPort == "25" || httpsPort == "8080" {
		altSMTP := smtpPort
		if altSMTP == "25" {
			altSMTP = "2525"
		}
		altHTTPS := httpsPort
		if altHTTPS == "8080" {
			altHTTPS = "8085"
		}
		if err2 := m.writeAndUp(exec, hostname, altSMTP, imapPort, altHTTPS); err2 == nil {
			return m.SaveInstance(m.serverID, serviceType, "running",
				fmt.Sprintf(`{"smtp_port":"%s","imap_port":"%s","https_port":"%s","hostname":"%s"}`,
					altSMTP, imapPort, altHTTPS, hostname))
		} else {
			return fmt.Errorf("mailinbox enable: %v (retry: %w)", err, err2)
		}
	}
	return fmt.Errorf("mailinbox enable: %w", err)
}

func (m *MailInbox) writeAndUp(exec *ssh.Executor, hostname, smtpPort, imapPort, httpsPort string) error {
	compose := fmt.Sprintf(`services:
  stalwart-mail:
    image: stalwartlabs/stalwart:latest
    container_name: stalwart-mail
    restart: unless-stopped
    hostname: %s
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
`, hostname, smtpPort, imapPort, httpsPort)
	return m.WriteCompose(exec, dir, compose)
}

func (m *MailInbox) Disable(exec *ssh.Executor) error {
	if err := m.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("mailinbox disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f stalwart-mail 2>/dev/null || true")
	return m.SaveInstance(m.serverID, serviceType, "stopped", "")
}

func (m *MailInbox) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "stalwart")
}
