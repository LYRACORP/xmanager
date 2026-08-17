package ops

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func clientConfig(srv storage.Server) ssh.ClientConfig {
	return ssh.ClientConfig{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.User,
		KeyPath:  srv.SSHKeyPath,
		Password: srv.Password,
		JumpHost: srv.JumpHost,
	}
}

func (c *Catalog) loadServer(id uint) (storage.Server, error) {
	var srv storage.Server
	if id == 0 {
		return srv, fmt.Errorf("server_id is required")
	}
	if err := c.DB.First(&srv, id).Error; err != nil {
		return srv, fmt.Errorf("server %d not found: %w", id, err)
	}
	return srv, nil
}

// Executor returns a live SSH executor, connecting from stored credentials if needed.
func (c *Catalog) Executor(serverID uint) (*ssh.Executor, error) {
	if c.Pool == nil {
		return nil, fmt.Errorf("ssh pool not configured")
	}
	if exec, ok := c.Pool.GetExecutor(serverID); ok {
		if client, cok := c.Pool.GetClient(serverID); cok && client.IsConnected() {
			return exec, nil
		}
	}
	srv, err := c.loadServer(serverID)
	if err != nil {
		return nil, err
	}
	if _, err := c.Pool.Connect(serverID, clientConfig(srv)); err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", srv.Name, err)
	}
	exec, ok := c.Pool.GetExecutor(serverID)
	if !ok {
		return nil, fmt.Errorf("executor missing after connect")
	}
	return exec, nil
}
