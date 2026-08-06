package gitea

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "gitea"
const dir = "/opt/xmanager/services/gitea"

type Gitea struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Gitea {
	return &Gitea{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (g *Gitea) Name() string { return serviceType }

func (g *Gitea) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker inspect gitea 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (g *Gitea) Enable(exec *ssh.Executor, cfg map[string]string) error {
	httpPort := cfg["http_port"]
	if httpPort == "" {
		httpPort = "3000"
	}
	sshPort := cfg["ssh_port"]
	if sshPort == "" {
		sshPort = "2222"
	}

	compose := fmt.Sprintf(`services:
  gitea:
    image: gitea/gitea:latest
    restart: unless-stopped
    environment:
      - USER_UID=1000
      - USER_GID=1000
    ports:
      - "%s:3000"
      - "%s:22"
    volumes:
      - gitea_data:/data
      - /etc/timezone:/etc/timezone:ro
      - /etc/localtime:/etc/localtime:ro
volumes:
  gitea_data:
`, httpPort, sshPort)

	if err := g.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("gitea enable: %w", err)
	}
	return g.SaveInstance(g.serverID, serviceType, "running",
		fmt.Sprintf(`{"http_port":"%s","ssh_port":"%s"}`, httpPort, sshPort))
}

func (g *Gitea) Disable(exec *ssh.Executor) error {
	if err := g.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("gitea disable: %w", err)
	}
	return g.SaveInstance(g.serverID, serviceType, "stopped", "")
}

func (g *Gitea) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("docker inspect --format='{{.State.Status}}' gitea 2>/dev/null")
	if out == "" {
		return "stopped"
	}
	return out
}
