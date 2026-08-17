package docker

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

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

func (m *Manager) run(cmd string) (string, error) {
	result, err := m.exec.Run(cmd)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	if result.ExitCode != 0 {
		out := strings.TrimSpace(result.Stderr)
		if out == "" {
			out = strings.TrimSpace(result.Stdout)
		}
		if out == "" {
			out = fmt.Sprintf("docker exited %d", result.ExitCode)
		}
		return "", fmt.Errorf("%s", out)
	}
	return result.Stdout, nil
}
