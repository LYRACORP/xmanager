package rustfs

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "rustfs"
const dir = "/opt/xmanager/services/rustfs"

// RustFS is an S3-compatible object storage server (MinIO-compatible API).
type RustFS struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *RustFS {
	return &RustFS{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (r *RustFS) Name() string { return serviceType }

func (r *RustFS) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker inspect rustfs 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (r *RustFS) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "9000"
	}
	consolePort := cfg["console_port"]
	if consolePort == "" {
		consolePort = "9001"
	}
	accessKey := cfg["access_key"]
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := cfg["secret_key"]
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	compose := fmt.Sprintf(`services:
  rustfs:
    image: rustfs/rustfs:latest
    restart: unless-stopped
    command: server /data --console-address ":9001"
    environment:
      - RUSTFS_ROOT_USER=%s
      - RUSTFS_ROOT_PASSWORD=%s
    ports:
      - "%s:9000"
      - "%s:9001"
    volumes:
      - rustfs_data:/data
volumes:
  rustfs_data:
`, accessKey, secretKey, port, consolePort)

	if err := r.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("rustfs enable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s","console_port":"%s"}`, port, consolePort))
}

func (r *RustFS) Disable(exec *ssh.Executor) error {
	if err := r.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("rustfs disable: %w", err)
	}
	return r.SaveInstance(r.serverID, serviceType, "stopped", "")
}

func (r *RustFS) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("docker inspect --format='{{.State.Status}}' rustfs 2>/dev/null")
	if out == "" {
		return "stopped"
	}
	return out
}
