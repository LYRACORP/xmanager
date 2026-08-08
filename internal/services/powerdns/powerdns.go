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
	dnsPort := cfg["dns_port"]
	if dnsPort == "" {
		dnsPort = "53"
	}
	apiPort := cfg["api_port"]
	if apiPort == "" {
		apiPort = "8081"
	}
	apiKey := cfg["api_key"]
	if apiKey == "" {
		apiKey = "changeme"
	}
	dbPass := cfg["db_password"]
	if dbPass == "" {
		dbPass = "pdnspassword"
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
`, dbPass, dbPass, apiKey, dbPass, dnsPort, dnsPort, apiPort)

	if err := p.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	return p.SaveInstance(p.serverID, serviceType, "running",
		fmt.Sprintf(`{"dns_port":"%s","api_port":"%s"}`, dnsPort, apiPort))
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
