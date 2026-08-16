package proxy

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type NginxManager struct {
	exec *ssh.Executor
}

func (n *NginxManager) Type() ProxyType { return Nginx }

func (n *NginxManager) IsAvailable() bool {
	return n.exec.RunQuiet("which nginx") != ""
}

// EnsureInstalled installs nginx via apt if missing (idempotent).
func (n *NginxManager) EnsureInstalled() error {
	if n.IsAvailable() {
		return nil
	}
	res, err := n.exec.Run("sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx 2>&1")
	if err != nil {
		return fmt.Errorf("install nginx: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("install nginx: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	_, _ = n.exec.Run("sudo systemctl enable --now nginx 2>/dev/null || sudo service nginx start 2>/dev/null || true")
	if !n.IsAvailable() {
		return fmt.Errorf("nginx still not found after apt install")
	}
	return nil
}

func (n *NginxManager) ListVHosts() ([]VHost, error) {
	result, err := n.exec.Run("ls /etc/nginx/sites-enabled/ 2>/dev/null")
	if err != nil || result.Stdout == "" {
		return nil, err
	}

	var vhosts []VHost
	for _, file := range strings.Split(result.Stdout, "\n") {
		file = strings.TrimSpace(file)
		if file == "" || file == "default" {
			continue
		}

		configPath := "/etc/nginx/sites-enabled/" + file
		content := n.exec.RunQuiet(fmt.Sprintf("cat %s 2>/dev/null", configPath))

		vhost := VHost{ConfigFile: configPath}
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "server_name ") {
				vhost.Domain = strings.TrimSuffix(strings.TrimPrefix(line, "server_name "), ";")
			}
			if strings.HasPrefix(line, "proxy_pass ") {
				vhost.Upstream = strings.TrimSuffix(strings.TrimPrefix(line, "proxy_pass "), ";")
			}
			if strings.Contains(line, "ssl_certificate") && !strings.Contains(line, "ssl_certificate_key") {
				vhost.SSLEnabled = true
			}
		}

		if vhost.Domain == "" {
			vhost.Domain = file
		}
		vhosts = append(vhosts, vhost)
	}
	return vhosts, nil
}

func (n *NginxManager) AddVHost(domain, upstream string) error {
	if err := n.EnsureInstalled(); err != nil {
		return err
	}
	// Ensure sites dirs exist (minimal nginx packages).
	_, _ = n.exec.Run("sudo mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled")
	// Include sites-enabled if the stock config doesn't.
	_, _ = n.exec.Run(`grep -q sites-enabled /etc/nginx/nginx.conf 2>/dev/null || sudo sed -i '/http {/a\    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf 2>/dev/null || true`)

	config := fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;

    location / {
        proxy_pass %s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}`, domain, upstream)

	configPath := fmt.Sprintf("/etc/nginx/sites-available/%s", domain)
	enablePath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", domain)

	// Write via tee with a heredoc-safe approach (avoid shell metachar in echo).
	writeCmd := fmt.Sprintf("sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF\nsudo ln -sf %s %s",
		configPath, config, configPath, enablePath)
	if res, err := n.exec.Run(writeCmd); err != nil {
		return err
	} else if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("write nginx vhost: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}

	if output, err := n.ValidateConfig(); err != nil || !strings.Contains(output, "successful") {
		_, _ = n.exec.Run(fmt.Sprintf("sudo rm -f %s", enablePath))
		return fmt.Errorf("nginx config validation failed: %s", output)
	}

	return n.ReloadConfig()
}

func (n *NginxManager) RemoveVHost(domain string) error {
	cmds := []string{
		fmt.Sprintf("sudo rm -f /etc/nginx/sites-enabled/%s", domain),
		fmt.Sprintf("sudo rm -f /etc/nginx/sites-available/%s", domain),
	}
	for _, cmd := range cmds {
		_, _ = n.exec.Run(cmd)
	}
	return n.ReloadConfig()
}

func (n *NginxManager) ValidateConfig() (string, error) {
	result, err := n.exec.Run("sudo nginx -t 2>&1")
	if err != nil {
		return result.Stdout + "\n" + result.Stderr, err
	}
	return result.Stdout + "\n" + result.Stderr, nil
}

func (n *NginxManager) ReloadConfig() error {
	_, err := n.exec.Run("sudo nginx -s reload")
	return err
}

func (n *NginxManager) RenewSSL() (string, error) {
	result, err := n.exec.Run("sudo certbot renew 2>&1")
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
