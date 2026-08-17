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
	existing := n.readVHost(domain)
	meta := ParseVHostConfig(existing)
	return n.writeVHost(domain, HTTPVHostConfig(domain, upstream, meta.Includes), true)
}

func (n *NginxManager) readVHost(domain string) string {
	return n.exec.RunQuiet(fmt.Sprintf("cat /etc/nginx/sites-available/%s 2>/dev/null || cat /etc/nginx/sites-enabled/%s 2>/dev/null", domain, domain))
}

func (n *NginxManager) writeVHost(domain, config string, enable bool) error {
	_, _ = n.exec.Run("sudo mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled")
	_, _ = n.exec.Run(`grep -q sites-enabled /etc/nginx/nginx.conf 2>/dev/null || sudo sed -i '/http {/a\    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf 2>/dev/null || true`)

	configPath := fmt.Sprintf("/etc/nginx/sites-available/%s", domain)
	enablePath := fmt.Sprintf("/etc/nginx/sites-enabled/%s", domain)
	writeCmd := fmt.Sprintf("sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", configPath, config)
	if enable {
		writeCmd += fmt.Sprintf("\nsudo ln -sf %s %s", configPath, enablePath)
	}
	if res, err := n.exec.Run(writeCmd); err != nil {
		return err
	} else if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("write nginx vhost: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	if output, err := n.ValidateConfig(); err != nil || !strings.Contains(output, "successful") {
		if enable {
			_, _ = n.exec.Run(fmt.Sprintf("sudo rm -f %s", enablePath))
		}
		return fmt.Errorf("nginx config validation failed: %s", output)
	}
	return n.ReloadConfig()
}

// EnableSSL rewrites the vhost with TLS + HTTP redirect, preserving snippet includes.
func (n *NginxManager) EnableSSL(domain, cert, key string) error {
	if err := n.EnsureInstalled(); err != nil {
		return err
	}
	meta := ParseVHostConfig(n.readVHost(domain))
	upstream := strings.TrimSpace(meta.Upstream)
	if upstream == "" {
		return fmt.Errorf("nginx: no proxy_pass for %s", domain)
	}
	return n.writeVHost(domain, HTTPSVHostConfig(domain, upstream, cert, key, meta.Includes), true)
}

// DisableSSL rewrites the vhost to HTTP-only, leaving certificate files on disk.
func (n *NginxManager) DisableSSL(domain string) error {
	if err := n.EnsureInstalled(); err != nil {
		return err
	}
	meta := ParseVHostConfig(n.readVHost(domain))
	upstream := strings.TrimSpace(meta.Upstream)
	if upstream == "" {
		return fmt.Errorf("nginx: no proxy_pass for %s", domain)
	}
	return n.writeVHost(domain, HTTPVHostConfig(domain, upstream, meta.Includes), true)
}

// DisableVHost unlinks the site without deleting the available config (used when Caddy takes the host).
func (n *NginxManager) DisableVHost(domain string) error {
	_, _ = n.exec.Run(fmt.Sprintf("sudo rm -f /etc/nginx/sites-enabled/%s", domain))
	return n.ReloadConfig()
}

// EnsureCertbot installs certbot and the nginx plugin if missing.
func (n *NginxManager) EnsureCertbot() error {
	if n.exec.RunQuiet("which certbot") != "" {
		return nil
	}
	res, err := n.exec.Run("sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq certbot python3-certbot-nginx 2>&1")
	if err != nil {
		return fmt.Errorf("install certbot: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("install certbot: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	if n.exec.RunQuiet("which certbot") == "" {
		return fmt.Errorf("certbot still not found after apt install")
	}
	return nil
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
