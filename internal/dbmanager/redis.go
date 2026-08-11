package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const Redis DBType = "redis"

type RedisManager struct {
	exec     *ssh.Executor
	host     string
	port     string
	password string
}

func newRedisManager(exec *ssh.Executor) *RedisManager {
	return &RedisManager{exec: exec, host: "127.0.0.1", port: "6379"}
}

func (r *RedisManager) Type() DBType { return Redis }

func (r *RedisManager) useDocker() bool {
	return dockerContainerRunning(r.exec, ContainerRedis)
}

func (r *RedisManager) IsAvailable() bool {
	if r.useDocker() {
		return true
	}
	return r.exec.RunQuiet("which redis-cli") != ""
}

func (r *RedisManager) effectivePassword() string {
	if r.password != "" {
		return r.password
	}
	if r.useDocker() {
		return redisContainerPassword(r.exec, ContainerRedis)
	}
	return ""
}

func (r *RedisManager) cliInner(cmd string) string {
	args := ""
	if !r.useDocker() {
		args = fmt.Sprintf("-h %s -p %s ", r.host, r.port)
	}
	if pw := r.effectivePassword(); pw != "" {
		args += "-a " + shellQuote(pw) + " "
	}
	return fmt.Sprintf("redis-cli %s%s 2>&1", args, cmd)
}

func (r *RedisManager) runCLI(cmd string) (*ssh.ExecResult, error) {
	inner := r.cliInner(cmd)
	if r.useDocker() {
		return dockerExec(r.exec, ContainerRedis, inner)
	}
	return r.exec.Run(inner)
}

// ListDatabases lists Redis logical databases (0–15 by default) that contain keys.
func (r *RedisManager) ListDatabases() ([]Database, error) {
	result, err := r.runCLI("INFO keyspace")
	if err := requireOK(result, err, "redis INFO keyspace failed"); err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "db") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		info := parts[1] // keys=5,expires=0,avg_ttl=0
		size := ""
		for _, kv := range strings.Split(info, ",") {
			if strings.HasPrefix(kv, "keys=") {
				size = strings.TrimPrefix(kv, "keys=") + " keys"
			}
		}
		dbs = append(dbs, Database{Name: parts[0], Owner: "-", Size: size})
	}
	return dbs, nil
}

// CreateDatabase selects a Redis DB index and sets a marker key to "reserve" it.
func (r *RedisManager) CreateDatabase(name string) error {
	// Redis doesn't support creating named databases; name is treated as a numeric DB index.
	result, err := r.runCLI(fmt.Sprintf("SELECT %s", name))
	if err := requireOK(result, err, "select database failed"); err != nil {
		return err
	}
	// Marker so INFO keyspace lists the DB after create.
	result, err = r.runCLI(fmt.Sprintf("-n %s SET xm:reserved 1", name))
	return requireOK(result, err, "reserve database failed")
}

// DropDatabase flushes all keys in the given DB index.
func (r *RedisManager) DropDatabase(name string) error {
	result, err := r.runCLI(fmt.Sprintf("-n %s FLUSHDB", name))
	return requireOK(result, err, "flush database failed")
}

// ListUsers returns ACL users configured on this Redis instance.
func (r *RedisManager) ListUsers() ([]DBUser, error) {
	result, err := r.runCLI("ACL LIST")
	if err := requireOK(result, err, "redis ACL LIST failed"); err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "(empty array)" {
			continue
		}
		// ACL LIST returns: "user <name> on/off ..."
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		users = append(users, DBUser{Name: parts[1], Roles: parts[2]})
	}
	return users, nil
}

// CreateUser adds an ACL user with the given password and full command access.
func (r *RedisManager) CreateUser(name, password string) error {
	cmd := fmt.Sprintf("ACL SETUSER %s on >%s ~* &* +@all", name, password)
	result, err := r.runCLI(cmd)
	return requireOK(result, err, "create user failed")
}

// SetUserPassword changes a Redis ACL user's password.
func (r *RedisManager) SetUserPassword(name, password string) error {
	cmd := fmt.Sprintf("ACL SETUSER %s >%s", name, password)
	result, err := r.runCLI(cmd)
	return requireOK(result, err, "set password failed")
}

// DeleteUser removes an ACL user.
func (r *RedisManager) DeleteUser(name string) error {
	result, err := r.runCLI(fmt.Sprintf("ACL DELUSER %s", name))
	return requireOK(result, err, "delete user failed")
}

// Backup triggers BGSAVE and copies the RDB file to destPath.
func (r *RedisManager) Backup(dbName, destPath string) error {
	saveResult, err := r.runCLI("BGSAVE")
	if err := requireOK(saveResult, err, "BGSAVE failed"); err != nil {
		return err
	}

	_ = r.exec.RunQuiet("sleep 2")

	if r.useDocker() {
		cmd := fmt.Sprintf("docker cp %s:/data/dump.rdb %s", ContainerRedis, shellQuote(destPath))
		result, err := r.exec.Run(cmd)
		return requireOK(result, err, "copy rdb failed")
	}

	rdbPath := r.exec.RunQuiet(r.cliInner("CONFIG GET dir"))
	dir := ""
	for _, line := range strings.Split(rdbPath, "\n") {
		if line != "dir" {
			dir = strings.TrimSpace(line)
			break
		}
	}
	if dir == "" {
		dir = "/var/lib/redis"
	}

	result, err := r.exec.Run(fmt.Sprintf("cp %s/dump.rdb %s", dir, shellQuote(destPath)))
	return requireOK(result, err, "copy rdb failed")
}

// Restore replaces the RDB file and restarts Redis.
func (r *RedisManager) Restore(dbName, srcPath string) error {
	if r.useDocker() {
		cmd := fmt.Sprintf(
			`docker cp %s %s:/data/dump.rdb && docker restart %s`,
			shellQuote(srcPath), ContainerRedis, ContainerRedis,
		)
		result, err := r.exec.Run(cmd)
		return requireOK(result, err, "restore failed")
	}

	rdbPath := r.exec.RunQuiet(r.cliInner("CONFIG GET dir"))
	dir := "/var/lib/redis"
	for _, line := range strings.Split(rdbPath, "\n") {
		if line != "dir" {
			dir = strings.TrimSpace(line)
			break
		}
	}

	result, err := r.exec.Run(fmt.Sprintf(
		"cp %s %s/dump.rdb && sudo systemctl restart redis 2>/dev/null || sudo service redis-server restart 2>/dev/null",
		shellQuote(srcPath), dir,
	))
	return requireOK(result, err, "restore failed")
}
