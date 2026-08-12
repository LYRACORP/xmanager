package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const PgAdminDir = "/opt/xmanager/pgadmin"

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
      "PassFile": "/pgpass",
      "SSLMode": "prefer"
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

// EnsurePgAdminConfig writes servers.json/pgpass and recreates pgAdmin when config is stale.
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

// InstallPgAdmin starts pgAdmin 4 with a pre-configured connection to xm-postgres.
func InstallPgAdmin(exec *ssh.Executor, email, password string) (string, error) {
	EnsureDBNetwork(exec)
	if err := writePgAdminConfig(exec); err != nil {
		return "", err
	}
	if email == "" {
		email = "admin@xmanager.local"
	}
	if password == "" {
		password = randomPassword(12)
	}

	_, _ = exec.Run(fmt.Sprintf("docker rm -f %s 2>/dev/null || true", PgAdminName))

	run := fmt.Sprintf(
		`docker run -d --name %s --restart unless-stopped --network %s`+
			` -e PGADMIN_DEFAULT_EMAIL=%s -e PGADMIN_DEFAULT_PASSWORD=%s`+
			` -v xm-pgadmin-data:/var/lib/pgadmin`+
			` -v %s/servers.json:/pgadmin4/servers.json:ro`+
			` -v %s/pgpass:/pgpass:ro`+
			` -p %s:80 dpage/pgadmin4:latest 2>&1`,
		PgAdminName, DBNetwork, email, password, PgAdminDir, PgAdminDir, PgAdminPort,
	)
	res, err := exec.Run(run)
	if err != nil {
		return "", fmt.Errorf("start pgadmin: %w", err)
	}
	if res.ExitCode != 0 && !strings.Contains(res.Stdout+res.Stderr, "already in use") {
		return "", fmt.Errorf("start pgadmin: %s", res.Stdout+res.Stderr)
	}
	return fmt.Sprintf("pgAdmin4 at :%s (%s / %s) — server %s pre-configured", PgAdminPort, email, password, ContainerPostgres), nil
}
