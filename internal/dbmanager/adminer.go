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
	AdminerImage   = "xm-adminer:6.0.1"
	PgAdminPort    = "5050"
	PgAdminName    = "xm-pgadmin"
	DBNetwork      = "xm-db"
	AdminerDir     = "/opt/xmanager/adminer"
)

// EnsureDBNetwork creates the shared Docker network used by DB engines + Adminer.
func EnsureDBNetwork(exec *ssh.Executor) {
	_, _ = exec.Run(fmt.Sprintf(`docker network create %s 2>/dev/null || true`, DBNetwork))
}

// InstallAdminer builds and starts Adminer 6 with drivers for all supported engines.
// Rebuilds the image and recreates the container (safe to call repeatedly).
func InstallAdminer(exec *ssh.Executor) (string, error) {
	EnsureDBNetwork(exec)

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

// AdminerNeedsUpgrade reports whether the running container uses an outdated image tag.
func AdminerNeedsUpgrade(exec *ssh.Executor) bool {
	if !ContainerRunning(exec, AdminerName) {
		return false
	}
	img := strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker inspect %s --format '{{.Config.Image}}' 2>/dev/null`, AdminerName),
	))
	return img != AdminerImage
}

func adminerDockerfile() string {
	return `# XManager Adminer — Adminer 6 + all DB drivers
FROM php:8.3-apache-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
      libssl-dev pkg-config curl \
      libpq-dev \
      default-libmysqlclient-dev \
    && docker-php-ext-install pdo pdo_mysql mysqli pgsql pdo_pgsql \
    && pecl install mongodb redis \
    && docker-php-ext-enable mongodb redis \
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

func randomPassword(n int) string {
	if n <= 0 {
		n = 12
	}
	b := make([]byte, (n+1)/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
