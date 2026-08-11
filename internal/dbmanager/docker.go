package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// Canonical Docker container names used by the node web panel installer.
const (
	ContainerPostgres   = "xm-postgres"
	ContainerMySQL      = "xm-mysql"
	ContainerMariaDB    = "xm-mariadb"
	ContainerMongoDB    = "xm-mongodb"
	ContainerRedis      = "xm-redis"
	ContainerClickHouse = "xm-clickhouse"
)

func containerFor(t DBType) string {
	switch t {
	case PostgreSQL:
		return ContainerPostgres
	case MySQL:
		return ContainerMySQL
	case MariaDB:
		return ContainerMariaDB
	case MongoDB:
		return ContainerMongoDB
	case Redis:
		return ContainerRedis
	case ClickHouse:
		return ContainerClickHouse
	default:
		return ""
	}
}

func dockerContainerRunning(exec *ssh.Executor, name string) bool {
	if exec == nil || name == "" {
		return false
	}
	out := strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker ps --filter name=^/%s$ --format '{{.Names}}' 2>/dev/null`, name),
	))
	return out == name
}

func dockerEnv(exec *ssh.Executor, container, key string) string {
	if exec == nil || container == "" || key == "" {
		return ""
	}
	return strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker exec %s printenv %s 2>/dev/null`, container, key),
	))
}

// dockerExec runs a command inside a container; argv is joined as a shell string.
func dockerExec(exec *ssh.Executor, container, shellCmd string) (*ssh.ExecResult, error) {
	cmd := fmt.Sprintf("docker exec %s sh -c %s", container, shellQuote(shellCmd))
	return exec.Run(cmd)
}

func dockerExecUser(exec *ssh.Executor, container, user, shellCmd string) (*ssh.ExecResult, error) {
	cmd := fmt.Sprintf("docker exec -u %s %s sh -c %s", user, container, shellQuote(shellCmd))
	return exec.Run(cmd)
}

// redisContainerPassword reads --requirepass from the container command line.
func redisContainerPassword(exec *ssh.Executor, container string) string {
	raw := strings.TrimSpace(exec.RunQuiet(
		fmt.Sprintf(`docker inspect -f '{{range .Args}}{{.}} {{end}}{{range .Config.Cmd}}{{.}} {{end}}' %s 2>/dev/null`, container),
	))
	fields := strings.Fields(raw)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "--requirepass" {
			return fields[i+1]
		}
	}
	return ""
}
