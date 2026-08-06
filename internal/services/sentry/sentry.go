// Package sentry deploys GlitchTip, an open-source Sentry-compatible error tracker.
package sentry

import (
	"fmt"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "sentry"
const dir = "/opt/xmanager/services/sentry"

type Sentry struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *Sentry {
	return &Sentry{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (s *Sentry) Name() string { return serviceType }

func (s *Sentry) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker inspect glitchtip-web 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (s *Sentry) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "8000"
	}
	secretKey := cfg["secret_key"]
	if secretKey == "" {
		secretKey = "changemeinproduction"
	}
	dbPass := cfg["db_password"]
	if dbPass == "" {
		dbPass = "glitchtip"
	}
	email := cfg["email"]
	if email == "" {
		email = "admin@example.com"
	}

	compose := fmt.Sprintf(`services:
  postgres:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      - POSTGRES_DB=glitchtip
      - POSTGRES_USER=glitchtip
      - POSTGRES_PASSWORD=%s
    volumes:
      - gt_postgres:/var/lib/postgresql/data
  redis:
    image: redis:7-alpine
    restart: unless-stopped
  web:
    image: glitchtip/glitchtip:latest
    container_name: glitchtip-web
    restart: unless-stopped
    depends_on:
      - postgres
      - redis
    environment:
      - DATABASE_URL=postgres://glitchtip:%s@postgres:5432/glitchtip
      - REDIS_URL=redis://redis:6379
      - SECRET_KEY=%s
      - PORT=8000
      - EMAIL_URL=consolemail://
      - DEFAULT_FROM_EMAIL=%s
      - GLITCHTIP_DOMAIN=http://localhost:%s
    ports:
      - "%s:8000"
    volumes:
      - gt_uploads:/code/uploads
  worker:
    image: glitchtip/glitchtip:latest
    restart: unless-stopped
    command: ./bin/run-celery-with-beat.sh
    depends_on:
      - postgres
      - redis
    environment:
      - DATABASE_URL=postgres://glitchtip:%s@postgres:5432/glitchtip
      - REDIS_URL=redis://redis:6379
      - SECRET_KEY=%s
volumes:
  gt_postgres:
  gt_uploads:
`, dbPass, dbPass, secretKey, email, port, port, dbPass, secretKey)

	if err := s.WriteCompose(exec, dir, compose); err != nil {
		return fmt.Errorf("sentry enable: %w", err)
	}

	// run migrations
	_, _ = exec.Run(fmt.Sprintf("cd %s && docker compose run --rm web ./manage.py migrate 2>&1", dir))
	_, _ = exec.Run(fmt.Sprintf(
		"cd %s && docker compose run --rm web ./manage.py createsuperuser --noinput --email %s 2>&1",
		dir, email,
	))

	return s.SaveInstance(s.serverID, serviceType, "running",
		fmt.Sprintf(`{"port":"%s","email":"%s"}`, port, email))
}

func (s *Sentry) Disable(exec *ssh.Executor) error {
	if err := s.ComposeDown(exec, dir); err != nil {
		return fmt.Errorf("sentry disable: %w", err)
	}
	return s.SaveInstance(s.serverID, serviceType, "stopped", "")
}

func (s *Sentry) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("docker inspect --format='{{.State.Status}}' glitchtip-web 2>/dev/null")
	if out == "" {
		return "stopped"
	}
	return out
}
