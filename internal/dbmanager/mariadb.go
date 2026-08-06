package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// MariaDB uses the same client binary as MySQL but has its own type constant
// and slightly different system database filtering.
const MariaDB DBType = "mariadb"

type MariaDBManager struct {
	exec *ssh.Executor
}

func (m *MariaDBManager) Type() DBType { return MariaDB }

func (m *MariaDBManager) IsAvailable() bool {
	return m.exec.RunQuiet("which mariadb || which mysql") != ""
}

var mariaSystemDBs = map[string]bool{
	"information_schema": true,
	"performance_schema": true,
	"mysql":              true,
	"sys":                true,
}

func (m *MariaDBManager) mariaCmd(sql string) string {
	// prefer mariadb binary, fall back to mysql
	bin := "mariadb"
	if m.exec.RunQuiet("which mariadb") == "" {
		bin = "mysql"
	}
	return fmt.Sprintf(`%s -N -e %s`, bin, shellQuote(sql))
}

func (m *MariaDBManager) ListDatabases() ([]Database, error) {
	result, err := m.exec.Run(m.mariaCmd("SHOW DATABASES"))
	if err := requireOK(result, err, "mariadb SHOW DATABASES failed"); err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || mariaSystemDBs[name] {
			continue
		}
		dbs = append(dbs, Database{Name: name, Owner: "-", Size: "-"})
	}
	return dbs, nil
}

func (m *MariaDBManager) CreateDatabase(name string) error {
	result, err := m.exec.Run(m.mariaCmd(fmt.Sprintf("CREATE DATABASE `%s`", name)))
	return requireOK(result, err, "create database failed")
}

func (m *MariaDBManager) DropDatabase(name string) error {
	result, err := m.exec.Run(m.mariaCmd(fmt.Sprintf("DROP DATABASE `%s`", name)))
	return requireOK(result, err, "drop database failed")
}

func (m *MariaDBManager) ListUsers() ([]DBUser, error) {
	result, err := m.exec.Run(m.mariaCmd("SELECT user, host FROM mysql.user ORDER BY user, host"))
	if err := requireOK(result, err, "mariadb user list failed"); err != nil {
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

func (m *MariaDBManager) CreateUser(name, password string) error {
	sql := fmt.Sprintf("CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'", name, password)
	result, err := m.exec.Run(m.mariaCmd(sql))
	return requireOK(result, err, "create user failed")
}

// GrantUser grants all privileges on dbName to the given user.
func (m *MariaDBManager) GrantUser(username, dbName string) error {
	sql := fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost'; FLUSH PRIVILEGES", dbName, username)
	result, err := m.exec.Run(m.mariaCmd(sql))
	return requireOK(result, err, "grant failed")
}

// RevokeUser revokes all privileges on dbName from the given user.
func (m *MariaDBManager) RevokeUser(username, dbName string) error {
	sql := fmt.Sprintf("REVOKE ALL PRIVILEGES ON `%s`.* FROM '%s'@'localhost'; FLUSH PRIVILEGES", dbName, username)
	result, err := m.exec.Run(m.mariaCmd(sql))
	return requireOK(result, err, "revoke failed")
}

func (m *MariaDBManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf("mariadb-dump %s 2>/dev/null || mysqldump %s | gzip > %s", dbName, dbName, destPath)
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (m *MariaDBManager) Restore(dbName, srcPath string) error {
	bin := "mariadb"
	if m.exec.RunQuiet("which mariadb") == "" {
		bin = "mysql"
	}
	cmd := fmt.Sprintf("gunzip -c %s | %s %s", srcPath, bin, dbName)
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "restore failed")
}
