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

func (m *MySQLManager) ListDatabases() ([]Database, error) {
	result, err := m.exec.Run("mysql -N -e \"SELECT table_schema, 'root', CONCAT(ROUND(SUM(data_length+index_length)/1024/1024, 2), ' MB') FROM information_schema.tables GROUP BY table_schema\" 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		dbs = append(dbs, Database{Name: parts[0], Owner: parts[1], Size: strings.Join(parts[2:], " ")})
	}
	return dbs, nil
}

func (m *MySQLManager) CreateDatabase(name string) error {
	_, err := m.exec.Run(fmt.Sprintf("mysql -e \"CREATE DATABASE %s\"", name))
	return err
}

func (m *MySQLManager) DropDatabase(name string) error {
	_, err := m.exec.Run(fmt.Sprintf("mysql -e \"DROP DATABASE %s\"", name))
	return err
}

func (m *MySQLManager) ListUsers() ([]DBUser, error) {
	result, err := m.exec.Run("mysql -N -e \"SELECT user, host FROM mysql.user ORDER BY user\" 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		users = append(users, DBUser{Name: parts[0], Roles: parts[1]})
	}
	return users, nil
}

func (m *MySQLManager) CreateUser(name, password string) error {
	cmd := fmt.Sprintf("mysql -e \"CREATE USER '%s'@'localhost' IDENTIFIED BY '%s'\"", name, password)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MySQLManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf("mysqldump %s | gzip > %s", dbName, destPath)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MySQLManager) Restore(dbName, srcPath string) error {
	cmd := fmt.Sprintf("gunzip -c %s | mysql %s", srcPath, dbName)
	_, err := m.exec.Run(cmd)
	return err
}
