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

func (m *MySQLManager) IsAvailable() bool {
	return m.exec.RunQuiet("which mysql") != ""
}

var mysqlSystemDBs = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

func (m *MySQLManager) ListDatabases() ([]Database, error) {
	result, err := m.exec.Run(`mysql -N -e "SHOW DATABASES"`)
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
	result, err := m.exec.Run(fmt.Sprintf(`mysql -e "CREATE DATABASE %s"`, name))
	return requireOK(result, err, "create database failed")
}

func (m *MySQLManager) DropDatabase(name string) error {
	result, err := m.exec.Run(fmt.Sprintf(`mysql -e "DROP DATABASE %s"`, name))
	return requireOK(result, err, "drop database failed")
}

func (m *MySQLManager) ListUsers() ([]DBUser, error) {
	result, err := m.exec.Run(`mysql -N -e "SELECT user, host FROM mysql.user ORDER BY user, host"`)
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
	cmd := fmt.Sprintf(`mysql -e "CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'"`, name, password)
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "create user failed")
}

func (m *MySQLManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf("mysqldump %s | gzip > %s", dbName, destPath)
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (m *MySQLManager) Restore(dbName, srcPath string) error {
	cmd := fmt.Sprintf("gunzip -c %s | mysql %s", srcPath, dbName)
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "restore failed")
}
