package rustfs

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "rustfs"
const dir = "/opt/xmanager/services/rustfs"

// RustFS is an S3-compatible object storage server.
type RustFS struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *RustFS {
	return &RustFS{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (r *RustFS) Name() string { return serviceType }

func (r *RustFS) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "rustfs") || r.InstanceEnabled(r.serverID, serviceType)
}

func (r *RustFS) Enable(exec *ssh.Executor, cfg map[string]string) error {
	c := LoadConfig(r.DB, r.serverID)
	if p := cfg["port"]; p != "" {
		c.Port = p
	}
	if p := cfg["console_port"]; p != "" {
		c.ConsolePort = p
	}
	if k := cfg["access_key"]; k != "" {
		c.AccessKey = k
	}
	if k := cfg["secret_key"]; k != "" {
		c.SecretKey = k
	}
	if c.Port == "" {
		c.Port = "9000"
	}
	if c.ConsolePort == "" {
		c.ConsolePort = "9001"
	}
	if c.AccessKey == "" {
		c.AccessKey = "rustfsadmin"
	}
	if c.SecretKey == "" {
		c.SecretKey = "rustfsadmin"
	}
	c.Endpoint = "http://127.0.0.1:" + c.Port

	compose := fmt.Sprintf(`services:
  rustfs:
    image: rustfs/rustfs:latest
    container_name: rustfs
    restart: unless-stopped
    environment:
      - RUSTFS_ACCESS_KEY=%s
      - RUSTFS_SECRET_KEY=%s
      - RUSTFS_ROOT_USER=%s
      - RUSTFS_ROOT_PASSWORD=%s
    ports:
      - "%s:9000"
      - "%s:9001"
    volumes:
      - rustfs_data:/data
volumes:
  rustfs_data:
`, c.AccessKey, c.SecretKey, c.AccessKey, c.SecretKey, c.Port, c.ConsolePort)

	if err := r.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("rustfs enable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "running", c.JSON())
}

func (r *RustFS) Disable(exec *ssh.Executor) error {
	if err := r.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("rustfs disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f rustfs 2>/dev/null || true")
	return r.SaveInstance(r.serverID, serviceType, "stopped", "")
}

func (r *RustFS) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "rustfs")
}
