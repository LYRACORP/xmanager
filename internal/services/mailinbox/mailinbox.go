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
	smtpPort := cfg["smtp_port"]
	if smtpPort == "" {
		smtpPort = "25"
	}
	imapPort := cfg["imap_port"]
	if imapPort == "" {
		imapPort = "143"
	}
	httpsPort := cfg["https_port"]
	if httpsPort == "" {
		httpsPort = "8080"
	}
	hostname := cfg["hostname"]
	if hostname == "" {
		hostname = "mail.example.com"
	}

	compose := fmt.Sprintf(`services:
  stalwart-mail:
    image: stalwartlabs/stalwart-mail:latest
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
      - stalwart_data:/opt/stalwart-mail
volumes:
  stalwart_data:
`, hostname, smtpPort, imapPort, httpsPort)

	if err := m.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("mailinbox enable: %w", err)
	}
	return m.SaveInstance(m.serverID, serviceType, "running",
		fmt.Sprintf(`{"smtp_port":"%s","imap_port":"%s","https_port":"%s","hostname":"%s"}`,
			smtpPort, imapPort, httpsPort, hostname))
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
