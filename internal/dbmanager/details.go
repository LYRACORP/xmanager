package dbmanager

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// DatabaseDetail holds per-database metrics for the detail page.
type DatabaseDetail struct {
	Name        string
	Owner       string
	Size        string
	Connections int
	Tables      int
	Extra       map[string]string
}

// DatabaseInfo returns live metrics for a single database.
func DatabaseInfo(dbType DBType, exec *ssh.Executor, name string) (DatabaseDetail, error) {
	mgr := NewManager(dbType, exec)
	if mgr == nil {
		return DatabaseDetail{}, fmt.Errorf("unknown db type %q", dbType)
	}
	detail := DatabaseDetail{Name: name, Extra: map[string]string{}}

	switch dbType {
	case PostgreSQL:
		return postgresDatabaseInfo(exec, name)
	case MySQL, MariaDB:
		return sqlDatabaseInfo(dbType, exec, name)
	case MongoDB:
		return mongoDatabaseInfo(exec, name)
	case Redis:
		detail.Size = "key-value"
		return detail, nil
	case ClickHouse:
		return clickhouseDatabaseInfo(exec, name)
	default:
		_ = mgr
		return detail, nil
	}
}

func postgresDatabaseInfo(exec *ssh.Executor, name string) (DatabaseDetail, error) {
	pm := &PostgresManager{exec: exec}
	sql := fmt.Sprintf(
		`SELECT pg_catalog.pg_get_userbyid(d.datdba), pg_size_pretty(pg_database_size(d.datname)), (SELECT count(*)::text FROM pg_stat_activity WHERE datname = d.datname) FROM pg_database d WHERE d.datname = %s`,
		shellQuote(name),
	)
	res, err := pm.psqlQuery(sql)
	if err != nil {
		return DatabaseDetail{Name: name}, err
	}
	if err := requireOK(res, nil, "postgres database info failed"); err != nil {
		return DatabaseDetail{Name: name}, err
	}
	line := strings.TrimSpace(res.Stdout)
	if line == "" {
		return DatabaseDetail{Name: name}, fmt.Errorf("database %q not found", name)
	}
	parts := strings.SplitN(line, "|", 3)
	detail := DatabaseDetail{Name: name, Extra: map[string]string{}}
	if len(parts) > 0 {
		detail.Owner = parts[0]
	}
	if len(parts) > 1 {
		detail.Size = parts[1]
	}
	if len(parts) > 2 {
		detail.Connections, _ = strconv.Atoi(parts[2])
	}

	tablesInner := fmt.Sprintf(`psql -w -t -A -d %s -c "SELECT count(*) FROM pg_stat_user_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')"`, shellQuote(name))
	if pm.useDocker() {
		if tres, err2 := dockerExecUser(exec, ContainerPostgres, "postgres", tablesInner); err2 == nil && tres != nil && tres.ExitCode == 0 {
			detail.Tables, _ = strconv.Atoi(strings.TrimSpace(tres.Stdout))
		}
	} else if tres, err2 := exec.Run("sudo -u postgres " + tablesInner); err2 == nil && tres != nil && tres.ExitCode == 0 {
		detail.Tables, _ = strconv.Atoi(strings.TrimSpace(tres.Stdout))
	}
	return detail, nil
}

func sqlDatabaseInfo(dbType DBType, exec *ssh.Executor, name string) (DatabaseDetail, error) {
	var run func(string) (*ssh.ExecResult, error)
	switch dbType {
	case MySQL:
		m := &MySQLManager{exec: exec}
		run = func(sql string) (*ssh.ExecResult, error) { return m.runSQL(sql) }
	case MariaDB:
		m := &MariaDBManager{exec: exec}
		run = func(sql string) (*ssh.ExecResult, error) { return m.runSQL(sql) }
	default:
		return DatabaseDetail{Name: name}, fmt.Errorf("unsupported type")
	}
	sizeSQL := fmt.Sprintf(
		"SELECT ROUND(SUM(data_length+index_length)/1024/1024,2), COUNT(*) FROM information_schema.tables WHERE table_schema=%s",
		shellQuote(name),
	)
	res, err := run(sizeSQL)
	if err := requireOK(res, err, "database info failed"); err != nil {
		return DatabaseDetail{Name: name}, err
	}
	parts := strings.Split(strings.TrimSpace(res.Stdout), "\t")
	detail := DatabaseDetail{Name: name, Extra: map[string]string{}}
	if len(parts) > 0 && parts[0] != "" && parts[0] != "NULL" {
		detail.Size = parts[0] + " MB"
	}
	if len(parts) > 1 {
		detail.Tables, _ = strconv.Atoi(parts[1])
	}
	return detail, nil
}

func mongoDatabaseInfo(exec *ssh.Executor, name string) (DatabaseDetail, error) {
	m := &MongoManager{exec: exec}
	eval := `JSON.stringify(db.getCollectionNames().length)`
	res, err := m.runMongo(fmt.Sprintf("%s --quiet --eval %s", shellQuote(name), shellQuote(eval)))
	if err := requireOK(res, err, "mongo database info failed"); err != nil {
		return DatabaseDetail{Name: name}, err
	}
	detail := DatabaseDetail{Name: name, Extra: map[string]string{}}
	detail.Tables, _ = strconv.Atoi(strings.TrimSpace(res.Stdout))
	return detail, nil
}

func clickhouseDatabaseInfo(exec *ssh.Executor, name string) (DatabaseDetail, error) {
	c := &ClickHouseManager{exec: exec}
	res, err := c.runQuery(fmt.Sprintf("SELECT count() FROM system.tables WHERE database = %s", shellQuote(name)))
	if err := requireOK(res, err, "clickhouse database info failed"); err != nil {
		return DatabaseDetail{Name: name}, err
	}
	detail := DatabaseDetail{Name: name, Extra: map[string]string{}}
	detail.Tables, _ = strconv.Atoi(strings.TrimSpace(res.Stdout))
	return detail, nil
}

// AdminerURL returns a deep-link into Adminer for the given engine/database.
func AdminerURL(baseURL, dbType, dbHost, dbName, username string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	switch DBType(dbType) {
	case PostgreSQL:
		if username == "" {
			username = "postgres"
		}
		return fmt.Sprintf("%s/?pgsql=%s&username=%s&db=%s", baseURL, dbHost, username, dbName)
	case MySQL, MariaDB:
		if username == "" {
			username = "root"
		}
		return fmt.Sprintf("%s/?server=%s&username=%s&db=%s", baseURL, dbHost, username, dbName)
	case MongoDB:
		return fmt.Sprintf("%s/?mongo=%s&username=&db=%s", baseURL, dbHost, dbName)
	case Redis:
		return fmt.Sprintf("%s/?redis=%s", baseURL, dbHost)
	case ClickHouse:
		return fmt.Sprintf("%s/?clickhouse=%s&username=default&db=%s", baseURL, dbHost, dbName)
	default:
		return baseURL
	}
}
