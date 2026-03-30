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
