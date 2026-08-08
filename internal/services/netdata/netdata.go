package netdata

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "netdata"
const dir = "/opt/xmanager/services/netdata"

type Netdata struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Netdata {
	return &Netdata{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (n *Netdata) Name() string { return serviceType }

func (n *Netdata) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "netdata") || n.InstanceEnabled(n.serverID, serviceType)
}

func (n *Netdata) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "19999"
	}
	// optional nginx auth_basic credentials (user:htpasswd-hash)
	nginxUser := cfg["nginx_user"]
	nginxPass := cfg["nginx_pass"]

	compose := fmt.Sprintf(`services:
  netdata:
    image: netdata/netdata:stable
    container_name: netdata
    restart: unless-stopped
    pid: host
    network_mode: host
    cap_add:
      - SYS_PTRACE
      - SYS_ADMIN
    security_opt:
      - apparmor:unconfined
    volumes:
      - netdata_config:/etc/netdata
      - netdata_lib:/var/lib/netdata
      - netdata_cache:/var/cache/netdata
      - /etc/passwd:/host/etc/passwd:ro
      - /etc/group:/host/etc/group:ro
      - /etc/localtime:/etc/localtime:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /etc/os-release:/host/etc/os-release:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - NETDATA_CLAIM_TOKEN=
      - NETDATA_CLAIM_URL=https://app.netdata.cloud
volumes:
  netdata_config:
  netdata_lib:
  netdata_cache:
`)

	if err := n.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("netdata enable: %w", err)
	}

	// optionally configure nginx reverse proxy with basic auth
	if nginxUser != "" && nginxPass != "" {
		if err := n.configureNginxAuth(exec, port, nginxUser, nginxPass); err != nil {
			// non-fatal: netdata itself is still up
			_ = err
		}
	}

	return n.SaveInstance(n.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s"}`, port))
}

func (n *Netdata) configureNginxAuth(exec *ssh.Executor, port, user, pass string) error {
	htpasswd := fmt.Sprintf("%s:%s", user, pass)
	nginxConf := fmt.Sprintf(`server {
    listen 19998;
    location / {
        auth_basic "Netdata";
        auth_basic_user_file /etc/nginx/netdata.htpasswd;
        proxy_pass http://127.0.0.1:%s;
        proxy_set_header Host $host;
    }
}`, port)

	_, _ = exec.Run("mkdir -p /etc/nginx/sites-enabled")
	_, _ = exec.Run(fmt.Sprintf(
		"echo %s | sudo tee /etc/nginx/netdata.htpasswd > /dev/null",
		shellQuote(htpasswd),
	))
	_, _ = exec.Run(fmt.Sprintf(
		"echo %s | sudo tee /etc/nginx/sites-enabled/netdata > /dev/null && sudo nginx -s reload 2>/dev/null || true",
		shellQuote(nginxConf),
	))
	return nil
}

func (n *Netdata) Disable(exec *ssh.Executor) error {
	if err := n.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("netdata disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f netdata 2>/dev/null || true")
	_, _ = exec.Run("sudo rm -f /etc/nginx/sites-enabled/netdata && sudo nginx -s reload 2>/dev/null || true")
	return n.SaveInstance(n.serverID, serviceType, "stopped", "")
}

func (n *Netdata) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "netdata")
}

func shellQuote(s string) string {
	return `'` + s + `'`
}
