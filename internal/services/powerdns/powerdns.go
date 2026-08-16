package powerdns

import (
	"fmt"
	"strconv"
	"strings"

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
	if c.DNSPort == "" {
		c.DNSPort = "53"
	}
	if c.APIPort == "" {
		c.APIPort = DefaultAPIPort
	}

	if _, err := exec.Run("mkdir -p " + dir); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	// Tear down legacy MariaDB stack and any orphaned auth container before port pick.
	_, _ = exec.Run("cd " + dir + " && docker compose down --remove-orphans 2>/dev/null || true")
	_, _ = exec.Run("docker rm -f pdns-auth powerdns-mariadb-1 2>/dev/null || true")
	_, _ = exec.Run("docker ps -aq --filter name=powerdns-mariadb --format '{{.ID}}' 2>/dev/null | xargs -r docker rm -f 2>/dev/null || true")

	// Legacy default 8081 collides with Adminer — remapped unless the port is free.
	if c.APIPort == "8081" && hostPortInUse(exec, "8081") {
		c.APIPort = DefaultAPIPort
	}
	c.APIPort = pickFreeHostPort(exec, c.APIPort, DefaultAPIPort)
	// Clear base_url override when it still points at the old Adminer collision port.
	if c.BaseURL != "" && strings.Contains(c.BaseURL, ":8081") && c.APIPort != "8081" {
		c.BaseURL = ""
	}

	compose := ComposeYAML(c)

	initScript := `#!/bin/sh
set -e
DB=/var/lib/powerdns/pdns.sqlite3
if [ ! -f "$DB" ]; then
  SCHEMA=""
  for s in \
    /usr/share/doc/pdns/schema.sqlite3.sql \
    /usr/share/doc/pdns-backend-sqlite3/schema.sqlite3.sql \
    /usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql \
    /usr/share/doc/pdns-backend-sqlite/schema.sqlite3.sql
  do
    if [ -f "$s" ]; then SCHEMA="$s"; break; fi
  done
  if [ -z "$SCHEMA" ]; then
    SCHEMA=$(find /usr -name schema.sqlite3.sql 2>/dev/null | head -1 || true)
  fi
  if [ -z "$SCHEMA" ] || [ ! -f "$SCHEMA" ]; then
    echo "powerdns: schema.sqlite3.sql not found in image" >&2
    exit 1
  fi
  sqlite3 "$DB" < "$SCHEMA"
  chown pdns:pdns "$DB" 2>/dev/null || true
fi
# Official image entrypoint (env → pdns.conf); fall back to pdns_server.
if [ -x /usr/local/sbin/start.sh ]; then
  exec /usr/local/sbin/start.sh
fi
if [ -x /entrypoint.sh ]; then
  exec /entrypoint.sh
fi
exec pdns_server --daemon=no
`

	writeFile := func(path, body string) error {
		cmd := "cat > " + path + " << 'XEOF'\n" + body + "\nXEOF\nchmod +x " + path + " 2>/dev/null || true"
		res, err := exec.Run(cmd)
		if err != nil {
			return err
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("write %s failed: %s", path, strings.TrimSpace(res.Stdout+res.Stderr))
		}
		return nil
	}
	if err := writeFile(dir+"/init-and-run.sh", initScript); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	if err := writeFile(dir+"/docker-compose.yml", compose); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	upRes, err := exec.Run("cd " + dir + " && docker compose up -d 2>&1")
	if err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	if upRes.ExitCode != 0 {
		return fmt.Errorf("powerdns enable: %s", strings.TrimSpace(upRes.Stdout+upRes.Stderr))
	}
	if err := verifyAPIPublish(exec, c.APIPort); err != nil {
		return fmt.Errorf("powerdns enable: %w", err)
	}
	return p.SaveInstance(p.serverID, serviceType, "running", c.JSON())
}

// ComposeYAML builds the docker-compose file for the given config.
func ComposeYAML(c Config) string {
	if c.APIPort == "" {
		c.APIPort = DefaultAPIPort
	}
	if c.DNSPort == "" {
		c.DNSPort = "53"
	}
	return fmt.Sprintf(`services:
  pdns-auth:
    image: powerdns/pdns-auth-48:latest
    container_name: pdns-auth
    restart: unless-stopped
    environment:
      - PDNS_AUTH_API=yes
      - PDNS_AUTH_API_KEY=%s
      - PDNS_AUTH_LAUNCH=gsqlite3
      - PDNS_AUTH_GSQLITE3_DATABASE=/var/lib/powerdns/pdns.sqlite3
      - PDNS_AUTH_WEBSERVER=yes
      - PDNS_AUTH_WEBSERVER_ADDRESS=0.0.0.0
      - PDNS_AUTH_WEBSERVER_PORT=%s
      - PDNS_AUTH_WEBSERVER_ALLOW_FROM=0.0.0.0/0
    ports:
      - "%s:53/tcp"
      - "%s:53/udp"
      - "%s:%s"
    volumes:
      - pdns_data:/var/lib/powerdns
      - ./init-and-run.sh:/init-and-run.sh:ro
    entrypoint: ["/bin/sh", "/init-and-run.sh"]
volumes:
  pdns_data:
`, c.APIKey, ContainerAPIPort, c.DNSPort, c.DNSPort, c.APIPort, ContainerAPIPort)
}

func hostPortInUse(exec *ssh.Executor, port string) bool {
	if exec == nil || port == "" {
		return false
	}
	// Docker published ports or host listeners.
	out := exec.RunQuiet(fmt.Sprintf(
		`docker ps --format '{{.Ports}}' 2>/dev/null | grep -E '(:|^)%s->|0\.0\.0\.0:%s|:::%s' >/dev/null && echo busy; `+
			`ss -ltn 2>/dev/null | awk '{print $4}' | grep -E ':%s$' >/dev/null && echo busy; `+
			`true`,
		port, port, port, port))
	return strings.Contains(out, "busy")
}

func pickFreeHostPort(exec *ssh.Executor, preferred, fallbackStart string) string {
	start := preferred
	if start == "" {
		start = fallbackStart
	}
	n, err := strconv.Atoi(start)
	if err != nil || n <= 0 {
		n, _ = strconv.Atoi(fallbackStart)
	}
	if n <= 0 {
		n = 8082
	}
	for i := 0; i < 30; i++ {
		cand := strconv.Itoa(n + i)
		if !hostPortInUse(exec, cand) {
			return cand
		}
	}
	return strconv.Itoa(n)
}

func verifyAPIPublish(exec *ssh.Executor, hostPort string) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	out := strings.TrimSpace(exec.RunQuiet("docker port pdns-auth 2>/dev/null"))
	if out == "" {
		return fmt.Errorf("pdns-auth has no published ports — host port %s may be in use (Adminer uses 8081). Re-enable with api_port=%s or free the port", hostPort, DefaultAPIPort)
	}
	want := ":" + hostPort
	if !strings.Contains(out, ContainerAPIPort+"/tcp") || !strings.Contains(out, want) {
		return fmt.Errorf("pdns-auth API not published on host :%s (got: %s). Free the port or set api_port differently", hostPort, truncateOut(out, 120))
	}
	return nil
}

func truncateOut(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (p *PowerDNS) Disable(exec *ssh.Executor) error {
	if err := p.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("powerdns disable: %w", err)
	}
	_, _ = exec.Run("docker rm -f pdns-auth powerdns-mariadb-1 2>/dev/null || true")
	return p.SaveInstance(p.serverID, serviceType, "stopped", "")
}

func (p *PowerDNS) Status(exec *ssh.Executor) string {
	return services.ContainerStatus(exec, "pdns")
}

// API returns a configured HTTP client for this server's PowerDNS instance.
func (p *PowerDNS) API() *Client {
	return NewClient(LoadConfig(p.DB, p.serverID))
}
