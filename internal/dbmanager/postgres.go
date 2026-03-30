package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type PostgresManager struct {
	exec *ssh.Executor
}

func (p *PostgresManager) Type() DBType { return PostgreSQL }

func (p *PostgresManager) IsAvailable() bool {
	return p.exec.RunQuiet("which psql") != ""
}

func (p *PostgresManager) ListDatabases() ([]Database, error) {
	result, err := p.exec.Run("sudo -u postgres psql -t -A -c \"SELECT datname, pg_catalog.pg_get_userbyid(datdba), pg_size_pretty(pg_database_size(datname)) FROM pg_database WHERE datistemplate = false ORDER BY datname\" 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		dbs = append(dbs, Database{Name: parts[0], Owner: parts[1], Size: parts[2]})
	}
	return dbs, nil
}

func (p *PostgresManager) CreateDatabase(name string) error {
	_, err := p.exec.Run(fmt.Sprintf("sudo -u postgres createdb %s", name))
	return err
}

func (p *PostgresManager) DropDatabase(name string) error {
	_, err := p.exec.Run(fmt.Sprintf("sudo -u postgres dropdb %s", name))
	return err
}

func (p *PostgresManager) ListUsers() ([]DBUser, error) {
	result, err := p.exec.Run("sudo -u postgres psql -t -A -c \"SELECT usename, CASE WHEN usesuper THEN 'superuser' ELSE 'user' END FROM pg_user ORDER BY usename\" 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		users = append(users, DBUser{Name: parts[0], Roles: parts[1]})
	}
	return users, nil
}

func (p *PostgresManager) CreateUser(name, password string) error {
	cmd := fmt.Sprintf("sudo -u postgres psql -c \"CREATE USER %s WITH PASSWORD '%s'\"", name, password)
	_, err := p.exec.Run(cmd)
	return err
}

func (p *PostgresManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf("sudo -u postgres pg_dump %s | gzip > %s", dbName, destPath)
	_, err := p.exec.Run(cmd)
	return err
}

func (p *PostgresManager) Restore(dbName, srcPath string) error {
	cmd := fmt.Sprintf("gunzip -c %s | sudo -u postgres psql %s", srcPath, dbName)
	_, err := p.exec.Run(cmd)
	return err
}
