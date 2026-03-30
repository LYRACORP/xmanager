package recon

type ScriptCategory struct {
	Name     string
	Commands []string
}

func AllScripts() []ScriptCategory {
	return []ScriptCategory{
		{
			Name: "system",
			Commands: []string{
				"uname -a",
				"cat /etc/os-release 2>/dev/null || echo 'unknown'",
				"hostnamectl 2>/dev/null || hostname",
				"uptime",
				"nproc",
				"free -m",
				"df -h",
				"cat /proc/cpuinfo | grep 'model name' | head -1",
			},
		},
		{
			Name: "webservers",
			Commands: []string{
				"which nginx 2>/dev/null && nginx -v 2>&1 || echo 'not found'",
				"which apache2 2>/dev/null && apache2 -v 2>&1 || which httpd 2>/dev/null && httpd -v 2>&1 || echo 'not found'",
				"which caddy 2>/dev/null && caddy version 2>&1 || echo 'not found'",
				"which traefik 2>/dev/null && traefik version 2>&1 || echo 'not found'",
				"which haproxy 2>/dev/null && haproxy -v 2>&1 || echo 'not found'",
			},
		},
		{
			Name: "databases",
			Commands: []string{
				"which psql 2>/dev/null && psql --version 2>&1 || echo 'not found'",
				"which mysql 2>/dev/null && mysql --version 2>&1 || echo 'not found'",
				"which mongosh 2>/dev/null && mongosh --version 2>&1 || which mongo 2>/dev/null && mongo --version 2>&1 || echo 'not found'",
				"which redis-cli 2>/dev/null && redis-cli --version 2>&1 || echo 'not found'",
			},
		},
		{
			Name: "runtimes",
			Commands: []string{
				"docker ps --format '{{.Names}}\\t{{.Image}}\\t{{.Status}}\\t{{.Ports}}' 2>/dev/null || echo 'docker not available'",
				"docker compose ls --format json 2>/dev/null || docker-compose ls --format json 2>/dev/null || echo 'compose not available'",
				"pm2 jlist 2>/dev/null || echo 'pm2 not available'",
				"systemctl list-units --type=service --state=running --no-pager 2>/dev/null | head -50",
				"which node 2>/dev/null && node -v 2>&1 || echo 'not found'",
				"which python3 2>/dev/null && python3 --version 2>&1 || echo 'not found'",
				"which go 2>/dev/null && go version 2>&1 || echo 'not found'",
				"which java 2>/dev/null && java -version 2>&1 || echo 'not found'",
			},
		},
		{
			Name: "networking",
			Commands: []string{
				"ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null",
				"ufw status 2>/dev/null || echo 'ufw not available'",
				"iptables -L -n --line-numbers 2>/dev/null | head -30 || echo 'iptables not available'",
			},
		},
		{
			Name: "domains_ssl",
			Commands: []string{
				"ls /etc/nginx/sites-enabled/ 2>/dev/null || echo 'no nginx sites'",
				"grep -rh server_name /etc/nginx/sites-enabled/ 2>/dev/null | sort -u || echo 'no domains'",
				"certbot certificates 2>/dev/null || echo 'certbot not available'",
				"ls /etc/letsencrypt/live/ 2>/dev/null || echo 'no certs'",
			},
		},
		{
			Name: "resources",
			Commands: []string{
				"top -bn1 | head -5",
				"cat /proc/loadavg",
				"swapon --show 2>/dev/null || echo 'no swap'",
				"cat /proc/net/dev | tail -n +3",
			},
		},
	}
}
