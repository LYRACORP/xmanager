package docker

import "github.com/lyracorp/xmanager/internal/ssh"

type Manager struct {
	exec *ssh.Executor
}

func NewManager(exec *ssh.Executor) *Manager {
	return &Manager{exec: exec}
}

func (m *Manager) IsAvailable() bool {
	result := m.exec.RunQuiet("docker --version")
	return result != ""
}
