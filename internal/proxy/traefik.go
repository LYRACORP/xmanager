package proxy

import (
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type TraefikManager struct {
	exec *ssh.Executor
}

func (t *TraefikManager) Type() ProxyType { return Traefik }

func (t *TraefikManager) IsAvailable() bool {
	return t.exec.RunQuiet("which traefik 2>/dev/null || docker ps --filter name=traefik --format '{{.Names}}' 2>/dev/null") != ""
}

func (t *TraefikManager) ListVHosts() ([]VHost, error) {
	routers, err := t.ListRouters()
	if err != nil {
		return nil, err
	}

	vhosts := make([]VHost, len(routers))
	for i, r := range routers {
		vhosts[i] = VHost{
			Domain:     r.Rule,
			Upstream:   r.Service,
			SSLEnabled: r.TLS,
		}
	}
	return vhosts, nil
}

func (t *TraefikManager) ListRouters() ([]TraefikRouter, error) {
	paths := []string{
		"/etc/traefik/dynamic/",
		"/etc/traefik/conf.d/",
		"/opt/traefik/dynamic/",
	}

	for _, path := range paths {
		result := t.exec.RunQuiet("ls " + path + " 2>/dev/null")
		if result == "" {
			continue
		}

		content := t.exec.RunQuiet("cat " + path + "*.yml " + path + "*.yaml 2>/dev/null")
		return parseTraefikRouters(content), nil
	}

	return nil, nil
}

func parseTraefikRouters(content string) []TraefikRouter {
	var routers []TraefikRouter
	var current *TraefikRouter
	inRouters := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.Contains(trimmed, "routers:") {
			inRouters = true
			continue
		}

		if inRouters {
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
				inRouters = false
				if current != nil {
					routers = append(routers, *current)
					current = nil
				}
				continue
			}

			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			if indent <= 4 && strings.HasSuffix(trimmed, ":") {
				if current != nil {
					routers = append(routers, *current)
				}
				current = &TraefikRouter{Name: strings.TrimSuffix(trimmed, ":")}
			}

			if current != nil {
				if strings.HasPrefix(trimmed, "rule:") {
					current.Rule = strings.TrimSpace(strings.TrimPrefix(trimmed, "rule:"))
					current.Rule = strings.Trim(current.Rule, "\"'")
				}
				if strings.HasPrefix(trimmed, "service:") {
					current.Service = strings.TrimSpace(strings.TrimPrefix(trimmed, "service:"))
				}
				if strings.Contains(trimmed, "tls") {
					current.TLS = true
				}
			}
		}
	}

	if current != nil {
		routers = append(routers, *current)
	}
	return routers
}

func (t *TraefikManager) ValidateConfig() (string, error) {
	result, err := t.exec.Run("traefik healthcheck 2>&1 || echo 'healthcheck not available'")
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (t *TraefikManager) ReloadConfig() error {
	_, err := t.exec.Run("docker kill -s HUP traefik 2>/dev/null || systemctl reload traefik 2>/dev/null")
	return err
}
