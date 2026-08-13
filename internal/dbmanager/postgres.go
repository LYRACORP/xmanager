package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type PostgresManager struct {
	exec           *ssh.Executor
	serverPassword string // optional; from server tags (postgres_password=...)
}

func (p *PostgresManager) Type() DBType { return PostgreSQL }

func (p *PostgresManager) useDocker() bool {
	return ContainerRunning(p.exec, ContainerPostgres)
}

func (p *PostgresManager) IsAvailable() bool {
	if p.useDocker() {
		return true
	}
	return p.exec.RunQuiet("which psql") != ""
}

func (p *PostgresManager) effectivePassword() string {
	if p.serverPassword != "" {
		return p.serverPassword
	}
	if p.useDocker() {
		if pw := dockerEnv(p.exec, ContainerPostgres, "POSTGRES_PASSWORD"); pw != "" {
			return pw
		}
	}
	return p.discoverPostgresPassword()
}

func (p *PostgresManager) psqlQuery(sql string) (*ssh.ExecResult, error) {
	if p.useDocker() {
		// Official image: local peer auth as OS user postgres (no password needed).
		inner := fmt.Sprintf(`psql -w -t -A -c %s`, shellQuote(sql))
		return dockerExecUser(p.exec, ContainerPostgres, "postgres", inner)
	}

	password := p.effectivePassword()
	psql := postgresPSQLArgs(password)
	inner := fmt.Sprintf(`%s -c %s`, psql, shellQuote(sql))
	env := postgresEnvPrefix(password)

	attempts := []string{
		"sudo -n -u postgres " + env + " " + inner,
		"sudo -u postgres " + env + " " + inner,
		"runuser -u postgres -- " + env + " " + inner,
	}
	if password != "" {
		// password auth without sudo (SSH user may have PGPASSWORD + network access)
		attempts = append(attempts,
			"env PGPASSWORD="+shellQuote(password)+" "+inner,
		)
	}

	var last *ssh.ExecResult
	var lastErr error
	for _, cmd := range attempts {
		res, err := p.exec.Run(cmd)
		last = res
		lastErr = err
		if err != nil {
			continue
		}
		if res.ExitCode == 0 {
			return res, nil
		}
		// skip password-prompt noise; try next strategy
		if strings.Contains(res.Stderr, "Password for user") {
			continue
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	if last != nil && last.ExitCode != 0 {
		hint := "could not connect to PostgreSQL as OS user postgres (peer auth) or with .pgpass"
		if password == "" {
			hint = "PostgreSQL requires a password for user postgres — add credentials to ~postgres/.pgpass on the server (host:port:db:user:password) or configure local peer auth in pg_hba.conf"
		}
		if err := requireOK(last, nil, hint); err != nil {
			return nil, err
		}
	}
	return last, nil
}

func (p *PostgresManager) runAsPostgres(shellCmd string) error {
	if p.useDocker() {
		res, err := dockerExecUser(p.exec, ContainerPostgres, "postgres", shellCmd)
		return requireOK(res, err, "postgres docker command failed")
	}

	password := p.effectivePassword()
	env := postgresEnvPrefix(password)
	attempts := []string{
		"sudo -n -u postgres " + env + " " + shellCmd,
		"sudo -u postgres " + env + " " + shellCmd,
	}
	var last *ssh.ExecResult
	var lastErr error
	for _, cmd := range attempts {
		res, err := p.exec.Run(cmd)
		last = res
		lastErr = err
		if err != nil {
			continue
		}
		if res.ExitCode == 0 {
			return nil
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return requireOK(last, nil, "postgres command failed")
}

func (p *PostgresManager) ListDatabases() ([]Database, error) {
	result, err := p.psqlQuery(`SELECT datname, pg_catalog.pg_get_userbyid(datdba), pg_size_pretty(pg_database_size(datname)) FROM pg_database WHERE datistemplate = false ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	if err := requireOK(result, nil, "postgres list databases failed"); err != nil {
		return nil, err
	}

	var dbs []Database
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		dbs = append(dbs, Database{Name: parts[0], Owner: parts[1], Size: parts[2]})
	}
	return dbs, nil
}

func (p *PostgresManager) CreateDatabase(name string) error {
	return p.runAsPostgres(fmt.Sprintf("createdb %s", shellQuote(name)))
}

func (p *PostgresManager) DropDatabase(name string) error {
	return p.runAsPostgres(fmt.Sprintf("dropdb %s", shellQuote(name)))
}

func (p *PostgresManager) ListUsers() ([]DBUser, error) {
	result, err := p.psqlQuery(`SELECT rolname, CASE WHEN rolsuper THEN 'superuser' WHEN rolcanlogin THEN 'login' ELSE 'role' END FROM pg_roles WHERE rolcanlogin OR rolsuper ORDER BY rolname`)
	if err != nil {
		return nil, err
	}
	if err := requireOK(result, nil, "postgres list users failed"); err != nil {
		return nil, err
	}

	var users []DBUser
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 || parts[0] == "" {
			continue
		}
		users = append(users, DBUser{Name: parts[0], Roles: parts[1]})
	}
	return users, nil
}

func (p *PostgresManager) CreateUser(name, password string) error {
	var sql string
	if isSimpleIdent(name) {
		sql = fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", name, sqlString(password))
	} else {
		sql = fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", sqlIdent(name), sqlString(password))
	}
	return p.runAsPostgres(fmt.Sprintf(`psql -w -c %s`, shellQuote(sql)))
}

// GrantUser makes username owner of dbName and grants schema rights.
func (p *PostgresManager) GrantUser(username, dbName string) error {
	u, d := username, dbName
	if !isSimpleIdent(username) {
		u = sqlIdent(username)
	}
	if !isSimpleIdent(dbName) {
		d = sqlIdent(dbName)
	}
	sql := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", d, u)
	if err := p.runAsPostgres(fmt.Sprintf(`psql -w -c %s`, shellQuote(sql))); err != nil {
		return err
	}
	// Schema grants inside the target database.
	inner := fmt.Sprintf(
		`psql -w -d %s -c %s`,
		shellQuote(dbName),
		shellQuote(fmt.Sprintf("GRANT ALL ON SCHEMA public TO %s", u)),
	)
	if p.useDocker() {
		res, err := dockerExecUser(p.exec, ContainerPostgres, "postgres", inner)
		return requireOK(res, err, "grant schema failed")
	}
	return p.runAsPostgres(inner)
}

func isSimpleIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (p *PostgresManager) Backup(dbName, destPath string) error {
	if p.useDocker() {
		cmd := fmt.Sprintf("docker exec -u postgres %s pg_dump %s | gzip > %s", ContainerPostgres, shellQuote(dbName), shellQuote(destPath))
		res, err := p.exec.Run(cmd)
		return requireOK(res, err, "backup failed")
	}
	return p.runAsPostgres(fmt.Sprintf("pg_dump %s | gzip > %s", shellQuote(dbName), shellQuote(destPath)))
}

func (p *PostgresManager) Restore(dbName, srcPath string) error {
	if p.useDocker() {
		cmd := fmt.Sprintf("gunzip -c %s | docker exec -i -u postgres %s psql -w %s", shellQuote(srcPath), ContainerPostgres, shellQuote(dbName))
		res, err := p.exec.Run(cmd)
		return requireOK(res, err, "restore failed")
	}
	return p.runAsPostgres(fmt.Sprintf("gunzip -c %s | psql -w %s", shellQuote(srcPath), shellQuote(dbName)))
}
