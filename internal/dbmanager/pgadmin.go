package dbmanager

import (
	"fmt"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const (
	PgAdminDir    = "/opt/xmanager/pgadmin"
	PgAdminImage  = "dpage/pgadmin4:8.14"
	PgAdminVolume = "xm-pgadmin-data"
)

// PostgresContainerPassword reads POSTGRES_PASSWORD from the xm-postgres container.
func PostgresContainerPassword(exec *ssh.Executor) string {
	return dockerEnv(exec, ContainerPostgres, "POSTGRES_PASSWORD")
}

func writePgAdminConfig(exec *ssh.Executor) error {
	pass := PostgresContainerPassword(exec)
	if pass == "" {
		return fmt.Errorf("postgres container not running or POSTGRES_PASSWORD not set")
	}
	serversJSON := fmt.Sprintf(`{
  "Servers": {
    "1": {
      "Name": "XManager PostgreSQL",
      "Group": "Servers",
      "Host": %q,
      "Port": 5432,
      "MaintenanceDB": "postgres",
      "Username": "postgres",
      "PassFile": ".pgpass",
      "SSLMode": "prefer",
      "Shared": true
    }
  }
}`, ContainerPostgres)
	pgpass := fmt.Sprintf("%s:5432:*:postgres:%s\n", ContainerPostgres, pass)

	_, _ = exec.Run(fmt.Sprintf("mkdir -p %s", PgAdminDir))
	for name, content := range map[string]string{
		"servers.json": serversJSON,
		"pgpass":       pgpass,
	} {
		cmd := fmt.Sprintf("cat > %s/%s << 'XMEOF'\n%sXMEOF", PgAdminDir, name, content)
		if res, err := exec.Run(cmd); err != nil || (res != nil && res.ExitCode != 0) {
			msg := ""
			if res != nil {
				msg = res.Stdout + res.Stderr
			}
			return fmt.Errorf("write pgadmin %s: %v %s", name, err, msg)
		}
	}
	_, _ = exec.Run(fmt.Sprintf("chmod 600 %s/pgpass", PgAdminDir))
	return nil
}

func savePgAdminLogin(exec *ssh.Executor, email, password string) {
	_, _ = exec.Run(fmt.Sprintf("mkdir -p %s", PgAdminDir))
	cmd := fmt.Sprintf("cat > %s/login.env << 'XMEOF'\nemail=%s\npassword=%s\nXMEOF", PgAdminDir, email, password)
	_, _ = exec.Run(cmd)
}

func loadPgAdminLogin(exec *ssh.Executor) (email, password string) {
	out := exec.RunQuiet(fmt.Sprintf("cat %s/login.env 2>/dev/null", PgAdminDir))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "email=") {
			email = strings.TrimPrefix(line, "email=")
		}
		if strings.HasPrefix(line, "password=") {
			password = strings.TrimPrefix(line, "password=")
		}
	}
	return email, password
}

func pgAdminConfigCurrent(exec *ssh.Executor) string {
	return strings.TrimSpace(exec.RunQuiet(fmt.Sprintf("cat %s/pgpass 2>/dev/null", PgAdminDir)))
}

func pgAdminConfigExpected(exec *ssh.Executor) string {
	pass := PostgresContainerPassword(exec)
	if pass == "" {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%s:5432:*:postgres:%s", ContainerPostgres, pass))
}

// PgAdminContainerState returns docker state status (running, restarting, exited, …).
func PgAdminContainerState(exec *ssh.Executor) string {
	return strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker inspect -f '{{.State.Status}}' %s 2>/dev/null`, PgAdminName),
	))
}

// PgAdminReady reports whether the web UI responds on the published port.
func PgAdminReady(exec *ssh.Executor) bool {
	if !ContainerRunning(exec, PgAdminName) {
		return false
	}
	code := strings.TrimSpace(exec.RunQuiet(fmt.Sprintf(
		`curl -sf --max-time 2 -o /dev/null -w '%%{http_code}' http://127.0.0.1:%s/misc/ping 2>/dev/null || echo 000`,
		PgAdminPort,
	)))
	return code == "200"
}

// PgAdminNeedsRepair is true when the container is crash-looping or the UI is unreachable.
func PgAdminNeedsRepair(exec *ssh.Executor) bool {
	state := PgAdminContainerState(exec)
	if state == "restarting" || state == "exited" || state == "dead" {
		return true
	}
	if !ContainerRunning(exec, PgAdminName) {
		return false
	}
	if PgAdminReady(exec) {
		return false
	}
	// Not ready yet — only treat as broken if started > 60s ago.
	age := strings.TrimSpace(exec.RunQuiet(fmt.Sprintf(
		`started=$(docker inspect -f '{{.State.StartedAt}}' %s 2>/dev/null); `+
			`[ -n "$started" ] || { echo 999; exit 0; }; `+
			`echo $(($(date +%%s) - $(date -d "$started" +%%s 2>/dev/null || echo 0)))`,
		PgAdminName,
	)))
	sec := 0
	fmt.Sscanf(age, "%d", &sec)
	return sec > 60
}

// PgAdminNeedsUpgrade reports whether the running container uses an outdated image tag.
func PgAdminNeedsUpgrade(exec *ssh.Executor) bool {
	if !ContainerRunning(exec, PgAdminName) {
		return false
	}
	img := strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker inspect -f '{{.Config.Image}}' %s 2>/dev/null`, PgAdminName),
	))
	return img != PgAdminImage
}

func waitPgAdminReady(exec *ssh.Executor, timeoutSec int) bool {
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		if PgAdminReady(exec) {
			return true
		}
		_, _ = exec.Run("sleep 2")
	}
	return false
}

// PgAdminConfigStale is true when postgres credentials changed since pgAdmin was configured.
func PgAdminConfigStale(exec *ssh.Executor) bool {
	if !ContainerRunning(exec, PgAdminName) {
		return false
	}
	exp := pgAdminConfigExpected(exec)
	if exp == "" {
		return false
	}
	return pgAdminConfigCurrent(exec) != exp
}

// EnsurePgAdminConfig recreates pgAdmin when postgres credentials changed.
func EnsurePgAdminConfig(exec *ssh.Executor, email, password string) error {
	if !ContainerRunning(exec, PgAdminName) {
		return nil
	}
	if pgAdminConfigCurrent(exec) == pgAdminConfigExpected(exec) && pgAdminConfigCurrent(exec) != "" {
		return nil
	}
	_, err := InstallPgAdmin(exec, email, password)
	return err
}

// InstallPgAdmin starts pgAdmin 4 with xm-postgres pre-registered.
func InstallPgAdmin(exec *ssh.Executor, email, password string) (string, error) {
	EnsureDBNetwork(exec)
	if err := writePgAdminConfig(exec); err != nil {
		return "", err
	}

	savedEmail, savedPass := loadPgAdminLogin(exec)
	if email == "" {
		email = savedEmail
	}
	if email == "" {
		email = "admin@xmanager.local"
	}
	if password == "" {
		password = savedPass
	}
	if password == "" {
		password = randomPassword(12)
	}
	savePgAdminLogin(exec, email, password)

	_, _ = exec.Run(fmt.Sprintf("docker rm -f %s 2>/dev/null || true", PgAdminName))
	_, _ = exec.Run(fmt.Sprintf("docker volume rm %s 2>/dev/null || true", PgAdminVolume))

	run := fmt.Sprintf(
		`docker run -d --name %s --restart unless-stopped --network %s`+
			` -e PGADMIN_DEFAULT_EMAIL=%s`+
			` -e PGADMIN_DEFAULT_PASSWORD=%s`+
			` -e PGPASS_FILE=/pgpass`+
			` -e PGADMIN_REPLACE_SERVERS_ON_STARTUP=True`+
			` -e PGADMIN_CONFIG_ENHANCED_COOKIE_PROTECTION=False`+
			` -e PGADMIN_CONFIG_MASTER_PASSWORD_REQUIRED=False`+
			` -v %s:/var/lib/pgadmin`+
			` -v %s/servers.json:/pgadmin4/servers.json:ro`+
			` -v %s/pgpass:/pgpass:ro`+
			` -p %s:80 %s 2>&1`,
		PgAdminName, DBNetwork,
		shellQuote(email), shellQuote(password),
		PgAdminVolume,
		PgAdminDir, PgAdminDir,
		PgAdminPort, PgAdminImage,
	)
	res, err := exec.Run(run)
	if err != nil {
		return "", fmt.Errorf("start pgadmin: %w", err)
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stdout+res.Stderr, "already in use") {
		return "", fmt.Errorf("start pgadmin: %s", res.Stdout+res.Stderr)
	}

	if !waitPgAdminReady(exec, 90) {
		logs := exec.RunQuiet(fmt.Sprintf("docker logs %s --tail 40 2>&1", PgAdminName))
		return "", fmt.Errorf("pgadmin did not become ready on :%s — %s", PgAdminPort, strings.TrimSpace(logs))
	}

	EnsureDBToolNetworking(exec)

	return fmt.Sprintf("pgAdmin4 at :%s (%s / %s) — server %s pre-configured (all DBs listed under it)", PgAdminPort, email, password, ContainerPostgres), nil
}
