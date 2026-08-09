package powerdns

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "powerdns"
const dir = "/opt/xmanager/services/powerdns"

type PowerDNS struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *PowerDNS {
	return &PowerDNS{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (p *PowerDNS) Name() string { return serviceType }

func (p *PowerDNS) IsEnabled(exec *ssh.Executor) bool {
	return services.ServiceUp(exec, dir, "pdns") || p.InstanceEnabled(p.serverID, serviceType)
}

func (p *PowerDNS) Enable(exec *ssh.Executor, cfg map[string]string) error {
	existing := LoadConfig(p.DB, p.serverID)
	c := existing
	if cfg != nil {
		if v := cfg["dns_port"]; v != "" {
			c.DNSPort = v
		}
		if v := cfg["api_port"]; v != "" {
			c.APIPort = v
		}
		if v := cfg["api_key"]; v != "" {
			c.APIKey = v
		}
		if v := cfg["db_password"]; v != "" {
			c.DBPassword = v
		}
		if v := cfg["base_url"]; v != "" {
			c.BaseURL = v
		}
	}
	if c.APIKey == "" || c.APIKey == "changeme" {
		// Prefer a stable random key; keep existing non-default keys across re-enable.
		if existing.APIKey != "" && existing.APIKey != "changeme" {
			c.APIKey = existing.APIKey
		} else {
			c.APIKey = randomHex(16)
		}
	}
	if c.DBPassword == "" || c.DBPassword == "pdnspassword" {
		if existing.DBPassword != "" && existing.DBPassword != "pdnspassword" {
			c.DBPassword = existing.DBPassword
		} else {
			c.DBPassword = randomHex(12)
		}
	}
	if c.DNSPort == "" {
		c.DNSPort = "53"
	}
	if c.APIPort == "" {
		c.APIPort = "8081"
	}

	compose := fmt.Sprintf(`services:
  mariadb:
    image: mariadb:11
    restart: unless-stopped
    environment:
      - MARIADB_ROOT_PASSWORD=%s
      - MARIADB_DATABASE=pdns
      - MARIADB_USER=pdns
      - MARIADB_PASSWORD=%s
    volumes:
      - pdns_db:/var/lib/mysql
  pdns-auth:
    image: powerdns/pdns-auth-48:latest
    container_name: pdns-auth
    restart: unless-stopped
    depends_on:
      - mariadb
    environment:
      - PDNS_AUTH_API=yes
      - PDNS_AUTH_API_KEY=%s
      - PDNS_AUTH_LAUNCH=gmysql
      - PDNS_AUTH_GMYSQL_HOST=mariadb
      - PDNS_AUTH_GMYSQL_DBNAME=pdns
      - PDNS_AUTH_GMYSQL_USER=pdns
      - PDNS_AUTH_GMYSQL_PASSWORD=%s
      - PDNS_AUTH_WEBSERVER=yes
      - PDNS_AUTH_WEBSERVER_ADDRESS=0.0.0.0
      - PDNS_AUTH_WEBSERVER_PORT=8081
    ports:
      - "%s:53/tcp"
      - "%s:53/udp"
      - "%s:8081"
volumes:
  pdns_db:
`, c.DBPassword, c.DBPassword, c.APIKey, c.DBPassword, c.DNSPort, c.DNSPort, c.APIPort)

	if err := p.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	return p.SaveInstance(p.serverID, serviceType, "running", c.JSON())
}

func (p *PowerDNS) Disable(exec *ssh.Executor) error {
	if err := p.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("powerdns disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f pdns-auth 2>/dev/null || true")
	return p.SaveInstance(p.serverID, serviceType, "stopped", "")
}

func (p *PowerDNS) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "pdns")
}

// API returns a configured HTTP client for this server's PowerDNS instance.
func (p *PowerDNS) API() *Client {
	return NewClient(LoadConfig(p.DB, p.serverID))
}
