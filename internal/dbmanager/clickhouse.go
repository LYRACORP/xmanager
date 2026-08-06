package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const ClickHouse DBType = "clickhouse"

type ClickHouseManager struct {
	exec     *ssh.Executor
	host     string
	port     string
	user     string
	password string
}

func newClickHouseManager(exec *ssh.Executor) *ClickHouseManager {
	return &ClickHouseManager{exec: exec, host: "127.0.0.1", port: "9000", user: "default"}
}

func (c *ClickHouseManager) Type() DBType { return ClickHouse }

func (c *ClickHouseManager) IsAvailable() bool {
	return c.exec.RunQuiet("which clickhouse-client") != ""
}

func (c *ClickHouseManager) chCmd(query string) string {
	args := fmt.Sprintf("--host=%s --port=%s --user=%s", c.host, c.port, c.user)
	if c.password != "" {
		args += " --password=" + shellQuote(c.password)
	}
	return fmt.Sprintf("clickhouse-client %s --query=%s 2>&1", args, shellQuote(query))
}

var chSystemDBs = map[string]bool{
	"system":             true,
	"information_schema": true,
	"INFORMATION_SCHEMA": true,
}

func (c *ClickHouseManager) ListDatabases() ([]Database, error) {
	result, err := c.exec.Run(c.chCmd("SHOW DATABASES"))
	if err := requireOK(result, err, "clickhouse SHOW DATABASES failed"); err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || chSystemDBs[name] {
			continue
		}
		dbs = append(dbs, Database{Name: name, Owner: "-", Size: "-"})
	}
	return dbs, nil
}

func (c *ClickHouseManager) CreateDatabase(name string) error {
	result, err := c.exec.Run(c.chCmd(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", name)))
	return requireOK(result, err, "create database failed")
}

func (c *ClickHouseManager) DropDatabase(name string) error {
	result, err := c.exec.Run(c.chCmd(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)))
	return requireOK(result, err, "drop database failed")
}

func (c *ClickHouseManager) ListUsers() ([]DBUser, error) {
	result, err := c.exec.Run(c.chCmd("SHOW USERS"))
	if err := requireOK(result, err, "clickhouse SHOW USERS failed"); err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		users = append(users, DBUser{Name: name, Roles: "-"})
	}
	return users, nil
}

func (c *ClickHouseManager) CreateUser(name, password string) error {
	sql := fmt.Sprintf("CREATE USER IF NOT EXISTS %s IDENTIFIED BY '%s'", name, password)
	result, err := c.exec.Run(c.chCmd(sql))
	return requireOK(result, err, "create user failed")
}

// GrantUser grants SELECT on the given database to the user.
func (c *ClickHouseManager) GrantUser(username, dbName string) error {
	sql := fmt.Sprintf("GRANT SELECT ON `%s`.* TO %s", dbName, username)
	result, err := c.exec.Run(c.chCmd(sql))
	return requireOK(result, err, "grant failed")
}

func (c *ClickHouseManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf(
		"clickhouse-backup create --tables='%s.*' 2>&1 || clickhouse-client --query=\"BACKUP DATABASE `%s` TO File('%s')\" 2>&1",
		dbName, dbName, destPath,
	)
	result, err := c.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (c *ClickHouseManager) Restore(dbName, srcPath string) error {
	sql := fmt.Sprintf("RESTORE DATABASE `%s` FROM File('%s')", dbName, srcPath)
	result, err := c.exec.Run(c.chCmd(sql))
	return requireOK(result, err, "restore failed")
}
