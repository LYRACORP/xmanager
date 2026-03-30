package docker

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ComposeStack struct {
	Name       string
	Status     string
	ConfigFile string
}

func (m *Manager) ListComposeStacks() ([]ComposeStack, error) {
	result, err := m.exec.Run("docker compose ls --format json 2>/dev/null || docker-compose ls --format json 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}

	var stacks []ComposeStack
	var raw []struct {
		Name       string `json:"Name"`
		Status     string `json:"Status"`
		ConfigFile string `json:"ConfigFiles"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &raw); err != nil {
		for _, line := range strings.Split(result.Stdout, "\n") {
			var s struct {
				Name       string `json:"Name"`
				Status     string `json:"Status"`
				ConfigFile string `json:"ConfigFiles"`
			}
			if json.Unmarshal([]byte(line), &s) == nil && s.Name != "" {
				raw = append(raw, s)
			}
		}
	}

	for _, r := range raw {
		stacks = append(stacks, ComposeStack{
			Name: r.Name, Status: r.Status, ConfigFile: r.ConfigFile,
		})
	}
	return stacks, nil
}

func (m *Manager) ComposeUp(projectDir string) error {
	_, err := m.exec.Run(fmt.Sprintf("cd %s && docker compose up -d 2>&1", projectDir))
	return err
}

func (m *Manager) ComposeDown(projectDir string) error {
	_, err := m.exec.Run(fmt.Sprintf("cd %s && docker compose down 2>&1", projectDir))
	return err
}

func (m *Manager) ComposeBuild(projectDir string) error {
	_, err := m.exec.Run(fmt.Sprintf("cd %s && docker compose build 2>&1", projectDir))
	return err
}

func (m *Manager) ComposePull(projectDir string) error {
	_, err := m.exec.Run(fmt.Sprintf("cd %s && docker compose pull 2>&1", projectDir))
	return err
}

type Image struct {
	Repository string
	Tag        string
	ID         string
	Size       string
	Created    string
}

func (m *Manager) ListImages() ([]Image, error) {
	result, err := m.exec.Run("docker image ls --format '{{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}\t{{.CreatedSince}}'")
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}

	var images []Image
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		images = append(images, Image{
			Repository: parts[0], Tag: parts[1], ID: parts[2],
			Size: parts[3], Created: parts[4],
		})
	}
	return images, nil
}

func (m *Manager) PruneImages() (string, error) {
	result, err := m.exec.Run("docker image prune -af 2>&1")
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
