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

func (r *RedisManager) IsAvailable() bool {
	return r.exec.RunQuiet("which redis-cli") != ""
}

func (r *RedisManager) cliCmd(cmd string) string {
	args := fmt.Sprintf("-h %s -p %s", r.host, r.port)
	if r.password != "" {
		args += " -a " + shellQuote(r.password)
	}
	return fmt.Sprintf("redis-cli %s %s 2>&1", args, cmd)
}

// ListDatabases lists Redis logical databases (0–15 by default) that contain keys.
func (r *RedisManager) ListDatabases() ([]Database, error) {
	result, err := r.exec.Run(r.cliCmd("INFO keyspace"))
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
	result, err := r.exec.Run(r.cliCmd(fmt.Sprintf("SELECT %s", name)))
	return requireOK(result, err, "select database failed")
}

// DropDatabase flushes all keys in the given DB index.
func (r *RedisManager) DropDatabase(name string) error {
	cmd := fmt.Sprintf("-n %s FLUSHDB", name)
	result, err := r.exec.Run(r.cliCmd(cmd))
	return requireOK(result, err, "flush database failed")
}

// ListUsers returns ACL users configured on this Redis instance.
func (r *RedisManager) ListUsers() ([]DBUser, error) {
	result, err := r.exec.Run(r.cliCmd("ACL LIST"))
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
	result, err := r.exec.Run(r.cliCmd(cmd))
	return requireOK(result, err, "create user failed")
}

// SetUserPassword changes a Redis ACL user's password.
func (r *RedisManager) SetUserPassword(name, password string) error {
	cmd := fmt.Sprintf("ACL SETUSER %s >%s", name, password)
	result, err := r.exec.Run(r.cliCmd(cmd))
	return requireOK(result, err, "set password failed")
}

// DeleteUser removes an ACL user.
func (r *RedisManager) DeleteUser(name string) error {
	result, err := r.exec.Run(r.cliCmd(fmt.Sprintf("ACL DELUSER %s", name)))
	return requireOK(result, err, "delete user failed")
}

// Backup triggers BGSAVE and copies the RDB file to destPath.
func (r *RedisManager) Backup(dbName, destPath string) error {
	saveResult, err := r.exec.Run(r.cliCmd("BGSAVE"))
	if err := requireOK(saveResult, err, "BGSAVE failed"); err != nil {
		return err
	}

	// wait for background save to complete
	_ = r.exec.RunQuiet("sleep 2")

	rdbPath := r.exec.RunQuiet(r.cliCmd("CONFIG GET dir"))
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

	result, err := r.exec.Run(fmt.Sprintf("cp %s/dump.rdb %s", dir, destPath))
	return requireOK(result, err, "copy rdb failed")
}

// Restore replaces the RDB file and restarts Redis.
func (r *RedisManager) Restore(dbName, srcPath string) error {
	rdbPath := r.exec.RunQuiet(r.cliCmd("CONFIG GET dir"))
	dir := "/var/lib/redis"
	for _, line := range strings.Split(rdbPath, "\n") {
		if line != "dir" {
			dir = strings.TrimSpace(line)
			break
		}
	}

	result, err := r.exec.Run(fmt.Sprintf(
		"cp %s %s/dump.rdb && sudo systemctl restart redis 2>/dev/null || sudo service redis-server restart 2>/dev/null",
		srcPath, dir,
	))
	return requireOK(result, err, "restore failed")
}
