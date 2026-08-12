package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type MySQLManager struct {
	exec *ssh.Executor
}

func (m *MySQLManager) Type() DBType { return MySQL }

func (m *MySQLManager) useDocker() bool {
	return ContainerRunning(m.exec, ContainerMySQL)
}

func (m *MySQLManager) IsAvailable() bool {
	if m.useDocker() {
		return true
	}
	return m.exec.RunQuiet("which mysql") != ""
}

var mysqlSystemDBs = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

func (m *MySQLManager) mysqlCmd(sql string) string {
	if m.useDocker() {
		// Use container env password; -p"$VAR" must expand inside the container shell.
		return fmt.Sprintf(
			`mysql -uroot -p"$MYSQL_ROOT_PASSWORD" -N -e %s`,
			shellQuote(sql),
		)
	}
	return fmt.Sprintf(`mysql -N -e %s`, shellQuote(sql))
}

func (m *MySQLManager) runSQL(sql string) (*ssh.ExecResult, error) {
	if m.useDocker() {
		return dockerExec(m.exec, ContainerMySQL, m.mysqlCmd(sql))
	}
	return m.exec.Run(m.mysqlCmd(sql))
}

func (m *MySQLManager) ListDatabases() ([]Database, error) {
	result, err := m.runSQL("SHOW DATABASES")
	if err := requireOK(result, err, "mysql SHOW DATABASES failed (check credentials for SSH user)"); err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || mysqlSystemDBs[name] {
			continue
		}
		dbs = append(dbs, Database{Name: name, Owner: "-", Size: "-"})
	}
	return dbs, nil
}

func (m *MySQLManager) CreateDatabase(name string) error {
	result, err := m.runSQL(fmt.Sprintf("CREATE DATABASE `%s`", name))
	return requireOK(result, err, "create database failed")
}

func (m *MySQLManager) DropDatabase(name string) error {
	result, err := m.runSQL(fmt.Sprintf("DROP DATABASE `%s`", name))
	return requireOK(result, err, "drop database failed")
}

func (m *MySQLManager) ListUsers() ([]DBUser, error) {
	result, err := m.runSQL("SELECT user, host FROM mysql.user ORDER BY user, host")
	if err := requireOK(result, err, "mysql user list failed (check credentials for SSH user)"); err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		users = append(users, DBUser{Name: parts[0], Roles: parts[1]})
	}
	return users, nil
}

func (m *MySQLManager) CreateUser(name, password string) error {
	result, err := m.runSQL(fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", name, password))
	return requireOK(result, err, "create user failed")
}

func (m *MySQLManager) Backup(dbName, destPath string) error {
	if m.useDocker() {
		cmd := fmt.Sprintf(
			`docker exec %s sh -c 'mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" %s' | gzip > %s`,
			ContainerMySQL, shellQuote(dbName), shellQuote(destPath),
		)
		result, err := m.exec.Run(cmd)
		return requireOK(result, err, "backup failed")
	}
	cmd := fmt.Sprintf("mysqldump %s | gzip > %s", shellQuote(dbName), shellQuote(destPath))
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (m *MySQLManager) Restore(dbName, srcPath string) error {
	if m.useDocker() {
		cmd := fmt.Sprintf(
			`gunzip -c %s | docker exec -i %s sh -c 'mysql -uroot -p"$MYSQL_ROOT_PASSWORD" %s'`,
			shellQuote(srcPath), ContainerMySQL, shellQuote(dbName),
		)
		result, err := m.exec.Run(cmd)
		return requireOK(result, err, "restore failed")
	}
	cmd := fmt.Sprintf("gunzip -c %s | mysql %s", shellQuote(srcPath), shellQuote(dbName))
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "restore failed")
}
