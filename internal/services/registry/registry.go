package registry

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "registry"
const dir = "/opt/xmanager/services/registry"

type Registry struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Registry {
	return &Registry{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (r *Registry) Name() string { return serviceType }

func (r *Registry) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker inspect registry 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (r *Registry) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "5000"
	}

	compose := fmt.Sprintf(`services:
  registry:
    image: registry:2
    restart: unless-stopped
    ports:
      - "%s:5000"
    volumes:
      - registry_data:/var/lib/registry
volumes:
  registry_data:
`, port)

	if err := r.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("registry enable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "running", fmt.Sprintf(`{"port":"%s"}`, port))
}

func (r *Registry) Disable(exec *ssh.Executor) error {
	if err := r.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("registry disable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "stopped", "")
}

func (r *Registry) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("docker inspect --format='{{.State.Status}}' registry 2>/dev/null")
	if out == "" {
		return "stopped"
	}
	return out
}
