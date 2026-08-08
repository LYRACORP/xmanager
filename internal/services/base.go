package services

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// Service is the common interface every managed service must implement.
type Service interface {
	// Name returns the canonical service type identifier.
	Name() string
	// IsEnabled reports whether the service is currently running on the server.
	IsEnabled(exec *ssh.Executor) bool
	// Enable deploys and starts the service.
	Enable(exec *ssh.Executor, cfg map[string]string) error
	// Disable stops and removes the service.
	Disable(exec *ssh.Executor) error
	// Status returns a human-readable status string.
	Status(exec *ssh.Executor) string
}

// BaseDeployer provides shared SSH/SFTP/DB helpers for service implementations.
type BaseDeployer struct {
	DB *gorm.DB
}

// ContainerRunning reports whether any container whose name matches filter is up.
// Matches compose names like "gitea-gitea-1" via docker's name filter.
func ContainerRunning(exec *ssh.Executor, nameFilter string) bool {
	if exec == nil || nameFilter == "" {
		return false
	}
	out := exec.RunQuiet(fmt.Sprintf("docker ps -q --filter name=%s 2>/dev/null | head -1", shellSafe(nameFilter)))
	return strings.TrimSpace(out) != ""
}

// ComposeRunning reports whether docker compose in dir has any running service.
func ComposeRunning(exec *ssh.Executor, dir string) bool {
	if exec == nil || dir == "" {
		return false
	}
	out := exec.RunQuiet(fmt.Sprintf(
		`cd %s 2>/dev/null && docker compose ps --status running -q 2>/dev/null | head -1`,
		shellSafe(dir),
	))
	return strings.TrimSpace(out) != ""
}

// ServiceUp is true if compose project or a matching container is running.
func ServiceUp(exec *ssh.Executor, dir, nameFilter string) bool {
	if ComposeRunning(exec, dir) {
		return true
	}
	return ContainerRunning(exec, nameFilter)
}

// ContainerStatus returns docker status text for the first matching container.
func ContainerStatus(exec *ssh.Executor, nameFilter string) string {
	if exec == nil || nameFilter == "" {
		return "stopped"
	}
	out := exec.RunQuiet(fmt.Sprintf(
		`docker ps -a --filter name=%s --format '{{.Status}}' 2>/dev/null | head -1`,
		shellSafe(nameFilter),
	))
	out = strings.TrimSpace(out)
	if out == "" {
		return "stopped"
	}
	return out
}

// InstanceEnabled reports whether the DB marks this service enabled for the server.
func (b *BaseDeployer) InstanceEnabled(serverID uint, serviceType string) bool {
	if b.DB == nil {
		return false
	}
	var inst storage.ServiceInstance
	err := b.DB.Where("server_id = ? AND service_type = ? AND enabled = ?", serverID, serviceType, true).
		First(&inst).Error
	return err == nil
}

func shellSafe(s string) string {
	return strings.ReplaceAll(s, "'", "")
}

// WriteCompose writes a docker-compose.yml to dir on the remote host and runs compose up -d.
func (b *BaseDeployer) WriteCompose(exec *ssh.Executor, dir, composeYAML string) error {
	if _, err := exec.Run("mkdir -p " + dir); err != nil {
		return err
	}

	cmd := "cat > " + dir + "/docker-compose.yml << 'XEOF'\n" + composeYAML + "\nXEOF"
	res, err := exec.Run(cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errFromResult(res)
	}
	upRes, err := exec.Run("cd " + dir + " && docker compose up -d 2>&1")
	if err != nil {
		return err
	}
	if upRes.ExitCode != 0 {
		return errFromResult(upRes)
	}
	return nil
}

// ComposeDown runs docker compose down in the given directory.
func (b *BaseDeployer) ComposeDown(exec *ssh.Executor, dir string) error {
	res, err := exec.Run("cd " + dir + " && docker compose down 2>&1")
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return errFromResult(res)
	}
	return nil
}

// SaveInstance persists or updates a ServiceInstance in the DB.
func (b *BaseDeployer) SaveInstance(serverID uint, serviceType, status, configJSON string) error {
	if status != "running" && (configJSON == "" || configJSON == "{}") {
		// Mark intentional stop so node defaults do not auto-reenable.
		configJSON = `{"user_disabled":true}`
	}
	if status == "running" && strings.Contains(configJSON, `"user_disabled":true`) {
		configJSON = strings.ReplaceAll(configJSON, `"user_disabled":true,`, "")
		configJSON = strings.ReplaceAll(configJSON, `,"user_disabled":true`, "")
		configJSON = strings.ReplaceAll(configJSON, `"user_disabled":true`, "")
		if configJSON == "" || configJSON == "{}" {
			configJSON = "{}"
		}
	}

	var inst storage.ServiceInstance
	res := b.DB.Where("server_id = ? AND service_type = ?", serverID, serviceType).First(&inst)
	if res.Error != nil {
		inst = storage.ServiceInstance{
			ServerID:    serverID,
			ServiceType: serviceType,
			Enabled:     status == "running",
			Status:      status,
			ConfigJSON:  configJSON,
		}
		return b.DB.Create(&inst).Error
	}
	inst.Status = status
	inst.Enabled = status == "running"
	inst.ConfigJSON = configJSON
	return b.DB.Save(&inst).Error
}

func errFromResult(res *ssh.ExecResult) error {
	if res.Stderr != "" {
		return &remoteError{msg: res.Stderr}
	}
	return &remoteError{msg: res.Stdout}
}

type remoteError struct{ msg string }

func (e *remoteError) Error() string { return e.msg }
