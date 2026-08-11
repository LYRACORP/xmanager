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

func (c *ClickHouseManager) useDocker() bool {
	return dockerContainerRunning(c.exec, ContainerClickHouse)
}

func (c *ClickHouseManager) IsAvailable() bool {
	if c.useDocker() {
		return true
	}
	return c.exec.RunQuiet("which clickhouse-client") != ""
}

func (c *ClickHouseManager) chCmd(query string) string {
	if c.useDocker() {
		return fmt.Sprintf("clickhouse-client --query=%s 2>&1", shellQuote(query))
	}
	args := fmt.Sprintf("--host=%s --port=%s --user=%s", c.host, c.port, c.user)
	if c.password != "" {
		args += " --password=" + shellQuote(c.password)
	}
	return fmt.Sprintf("clickhouse-client %s --query=%s 2>&1", args, shellQuote(query))
}

func (c *ClickHouseManager) runQuery(query string) (*ssh.ExecResult, error) {
	if c.useDocker() {
		return dockerExec(c.exec, ContainerClickHouse, c.chCmd(query))
	}
	return c.exec.Run(c.chCmd(query))
}

var chSystemDBs = map[string]bool{
	"system":             true,
	"information_schema": true,
	"INFORMATION_SCHEMA": true,
}

func (c *ClickHouseManager) ListDatabases() ([]Database, error) {
	result, err := c.runQuery("SHOW DATABASES")
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
	result, err := c.runQuery(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", name))
	return requireOK(result, err, "create database failed")
}

func (c *ClickHouseManager) DropDatabase(name string) error {
	result, err := c.runQuery(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name))
	return requireOK(result, err, "drop database failed")
}

func (c *ClickHouseManager) ListUsers() ([]DBUser, error) {
	result, err := c.runQuery("SHOW USERS")
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
	result, err := c.runQuery(sql)
	return requireOK(result, err, "create user failed")
}

// GrantUser grants SELECT on the given database to the user.
func (c *ClickHouseManager) GrantUser(username, dbName string) error {
	sql := fmt.Sprintf("GRANT SELECT ON `%s`.* TO %s", dbName, username)
	result, err := c.runQuery(sql)
	return requireOK(result, err, "grant failed")
}

func (c *ClickHouseManager) Backup(dbName, destPath string) error {
	if c.useDocker() {
		sql := fmt.Sprintf("BACKUP DATABASE `%s` TO Disk('backups', '%s.zip')", dbName, dbName)
		result, err := c.runQuery(sql)
		return requireOK(result, err, "backup failed")
	}
	cmd := fmt.Sprintf(
		"clickhouse-backup create --tables='%s.*' 2>&1 || clickhouse-client --query=%s 2>&1",
		dbName, shellQuote(fmt.Sprintf("BACKUP DATABASE `%s` TO File('%s')", dbName, destPath)),
	)
	result, err := c.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (c *ClickHouseManager) Restore(dbName, srcPath string) error {
	sql := fmt.Sprintf("RESTORE DATABASE `%s` FROM File('%s')", dbName, srcPath)
	result, err := c.runQuery(sql)
	return requireOK(result, err, "restore failed")
}
