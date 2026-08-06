package services

import (
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

// WriteCompose writes a docker-compose.yml to dir on the remote host and runs compose up -d.
func (b *BaseDeployer) WriteCompose(exec *ssh.Executor, dir, composeYAML string) error {
	// create dir
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
	var inst storage.ServiceInstance
	res := b.DB.Where("server_id = ? AND service_type = ?", serverID, serviceType).First(&inst)
	if res.Error != nil {
		// create
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
