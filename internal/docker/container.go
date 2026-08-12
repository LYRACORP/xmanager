package docker

import (
	"fmt"
	"strings"
)

type Container struct {
	ID     string
	Name   string
	Image  string
	Status string
	Ports  string
	State  string
}

// ContainerStats holds live resource usage from docker stats.
type ContainerStats struct {
	CPUPct   string
	MemUsage string
}

func (m *Manager) ListContainers() ([]Container, error) {
	result, err := m.exec.Run("docker ps -a --format '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}\t{{.State}}'")
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}

	var containers []Container
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 6 {
			continue
		}
		containers = append(containers, Container{
			ID: parts[0], Name: parts[1], Image: parts[2],
			Status: parts[3], Ports: parts[4], State: parts[5],
		})
	}
	return containers, nil
}

// Stats returns CPU and memory usage for the given container names (one docker stats call).
func (m *Manager) Stats(names []string) map[string]ContainerStats {
	out := make(map[string]ContainerStats, len(names))
	if len(names) == 0 {
		return out
	}
	args := strings.Join(names, " ")
	cmd := fmt.Sprintf(
		`docker stats --no-stream --format '{{.Name}}	{{.CPUPerc}}	{{.MemUsage}}' %s 2>/dev/null`,
		args,
	)
	result, err := m.exec.Run(cmd)
	if err != nil || result == nil || result.ExitCode != 0 || result.Stdout == "" {
		return out
	}
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		out[parts[0]] = ContainerStats{CPUPct: parts[1], MemUsage: parts[2]}
	}
	return out
}

func (m *Manager) StartContainer(id string) error {
	_, err := m.exec.Run(fmt.Sprintf("docker start %s", id))
	return err
}

func (m *Manager) StopContainer(id string) error {
	_, err := m.exec.Run(fmt.Sprintf("docker stop %s", id))
	return err
}

func (m *Manager) RestartContainer(id string) error {
	_, err := m.exec.Run(fmt.Sprintf("docker restart %s", id))
	return err
}

func (m *Manager) RemoveContainer(id string) error {
	_, err := m.exec.Run(fmt.Sprintf("docker rm -f %s", id))
	return err
}

func (m *Manager) InspectContainer(id string) (string, error) {
	result, err := m.exec.Run(fmt.Sprintf("docker inspect %s", id))
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (m *Manager) ContainerLogs(id string, lines int) (string, error) {
	result, err := m.exec.Run(fmt.Sprintf("docker logs --tail %d %s 2>&1", lines, id))
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
