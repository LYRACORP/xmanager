package dbmanager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const (
	AdminerPort    = "8081"
	AdminerName    = "xm-adminer"
	AdminerImage   = "xm-adminer:6.0.0"
	PgAdminPort    = "5050"
	PgAdminName    = "xm-pgadmin"
	DBNetwork      = "xm-db"
	AdminerDir     = "/opt/xmanager/adminer"
)

// EnsureDBNetwork creates the shared Docker network used by DB engines + Adminer.
func EnsureDBNetwork(exec *ssh.Executor) {
	_, _ = exec.Run(fmt.Sprintf(`docker network create %s 2>/dev/null || true`, DBNetwork))
}

// InstallAdminer builds and starts Adminer 6 with mongo/redis/elastic/clickhouse drivers.
// Safe to call repeatedly — skips rebuild if container is already running.
func InstallAdminer(exec *ssh.Executor) (string, error) {
	EnsureDBNetwork(exec)

	running := strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker ps --filter name=^/%s$ --format '{{.Names}}'`, AdminerName),
	))
	if running == AdminerName || ContainerRunning(exec, AdminerName) {
		return fmt.Sprintf("Adminer already running at :%s", AdminerPort), nil
	}

	// Write Dockerfile on the host
	dockerfile := adminerDockerfile()
	_, _ = exec.Run(fmt.Sprintf("mkdir -p %s", AdminerDir))
	writeCmd := fmt.Sprintf("cat > %s/Dockerfile << 'XMEOF'\n%s\nXMEOF", AdminerDir, dockerfile)
	if res, err := exec.Run(writeCmd); err != nil || (res != nil && res.ExitCode != 0) {
		msg := ""
		if res != nil {
			msg = res.Stdout + res.Stderr
		}
		return "", fmt.Errorf("write Adminer Dockerfile: %v %s", err, msg)
	}

	build := exec.RunQuiet(fmt.Sprintf("cd %s && docker build -t %s . 2>&1", AdminerDir, AdminerImage))
	if strings.Contains(strings.ToLower(build), "error") && !strings.Contains(build, "Successfully tagged") {
		// Still try — some docker versions print warnings with "error" substring
		if !strings.Contains(build, "Successfully") && !strings.Contains(build, "naming to") {
			return "", fmt.Errorf("adminer build failed:\n%s", build)
		}
	}

	// Remove stopped leftover
	_, _ = exec.Run(fmt.Sprintf("docker rm -f %s 2>/dev/null || true", AdminerName))

	run := fmt.Sprintf(
		`docker run -d --name %s --restart unless-stopped --network %s -p %s:80 %s 2>&1`,
		AdminerName, DBNetwork, AdminerPort, AdminerImage,
	)
	res, err := exec.Run(run)
	if err != nil {
		return "", fmt.Errorf("start adminer: %w", err)
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stdout+res.Stderr, "already in use") {
		return "", fmt.Errorf("start adminer: %s", res.Stdout+res.Stderr)
	}

	return fmt.Sprintf("Adminer 6 at :%s (drivers: pgsql/mysql/mongo/redis/elastic/clickhouse)", AdminerPort), nil
}

func adminerDockerfile() string {
	return `# XManager Adminer — Adminer 6 + mongo/redis/elastic/clickhouse drivers
FROM php:8.3-apache-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      libssl-dev pkg-config curl \
    && pecl install mongodb \
    && docker-php-ext-enable mongodb \
    && a2enmod rewrite \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /var/www/html

ADD https://github.com/vrana/adminer/releases/download/v6.0.0/adminer-6.0.0-en.php /var/www/html/index.php

RUN mkdir -p /var/www/html/adminer-plugins
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/mongo.php \
    /var/www/html/adminer-plugins/mongo.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/redis.php \
    /var/www/html/adminer-plugins/redis.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/elastic.php \
    /var/www/html/adminer-plugins/elastic.php
ADD https://raw.githubusercontent.com/vrana/adminer/refs/tags/v6.0.0/plugins/drivers/clickhouse.php \
    /var/www/html/adminer-plugins/clickhouse.php

RUN chown -R www-data:www-data /var/www/html && chmod -R a+rX /var/www/html

EXPOSE 80
`
}

// NetworkFlag returns the docker --network flag for DB containers.
func NetworkFlag() string {
	return "--network " + DBNetwork
}

// InstallPgAdmin starts pgAdmin 4 on the shared DB network. Safe to call repeatedly.
func InstallPgAdmin(exec *ssh.Executor, email, password string) (string, error) {
	EnsureDBNetwork(exec)
	if ContainerRunning(exec, PgAdminName) {
		return fmt.Sprintf("pgAdmin already running at :%s", PgAdminPort), nil
	}
	if email == "" {
		email = "admin@xmanager.local"
	}
	if password == "" {
		password = randomPassword(12)
	}
	run := fmt.Sprintf(
		`docker run -d --name %s --restart unless-stopped --network %s`+
			` -e PGADMIN_DEFAULT_EMAIL=%s -e PGADMIN_DEFAULT_PASSWORD=%s`+
			` -p %s:80 dpage/pgadmin4:latest 2>&1`,
		PgAdminName, DBNetwork, email, password, PgAdminPort,
	)
	res, err := exec.Run(run)
	if err != nil {
		return "", fmt.Errorf("start pgadmin: %w", err)
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stdout+res.Stderr, "already in use") {
		return "", fmt.Errorf("start pgadmin: %s", res.Stdout+res.Stderr)
	}
	return fmt.Sprintf("pgAdmin4 at :%s (%s / %s)", PgAdminPort, email, password), nil
}

func randomPassword(n int) string {
	if n <= 0 {
		n = 12
	}
	b := make([]byte, (n+1)/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
