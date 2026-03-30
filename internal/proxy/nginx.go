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
	config := fmt.Sprintf(`server {
    listen 80;
    server_name %s;

    location / {
        proxy_pass %s;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}`, domain, upstream)

	configPath := fmt.Sprintf("/etc/nginx/sites-available/%s", domain)
	enablePath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", domain)

	cmds := []string{
		fmt.Sprintf("echo '%s' | sudo tee %s", config, configPath),
		fmt.Sprintf("sudo ln -sf %s %s", configPath, enablePath),
	}

	for _, cmd := range cmds {
		if _, err := n.exec.Run(cmd); err != nil {
			return err
		}
	}

	if output, err := n.ValidateConfig(); err != nil || !strings.Contains(output, "successful") {
		n.exec.Run(fmt.Sprintf("sudo rm -f %s", enablePath))
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
		n.exec.Run(cmd)
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
