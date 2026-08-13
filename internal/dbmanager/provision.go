package dbmanager

import (
	"fmt"
	"strings"
)

func sqlString(s string) string {
	return strings.ReplaceAll(s, `'`, `''`)
}

func sqlIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// ProvisionDatabase creates a database and optional login user with access to it.
// If username is empty, it defaults to the database name. If password is empty, user creation is skipped.
func ProvisionDatabase(mgr Manager, dbName, username, password string) (user string, err error) {
	if mgr == nil {
		return "", fmt.Errorf("no database manager")
	}
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return "", fmt.Errorf("database name required")
	}
	if err := mgr.CreateDatabase(dbName); err != nil {
		return "", err
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	username = strings.TrimSpace(username)
	if username == "" {
		username = dbName
	}
	if err := mgr.CreateUser(username, password); err != nil {
		// User may already exist — try grant anyway.
		if !strings.Contains(strings.ToLower(err.Error()), "already exists") &&
			!strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return username, fmt.Errorf("database created but user failed: %w", err)
		}
	}
	if err := GrantDatabaseAccess(mgr, username, dbName); err != nil {
		return username, fmt.Errorf("database+user created but grant failed: %w", err)
	}
	return username, nil
}

// GrantDatabaseAccess grants the user privileges on dbName for engines that support it.
func GrantDatabaseAccess(mgr Manager, username, dbName string) error {
	switch m := mgr.(type) {
	case *PostgresManager:
		return m.GrantUser(username, dbName)
	case *MySQLManager:
		return m.GrantUser(username, dbName)
	case *MariaDBManager:
		return m.GrantUser(username, dbName)
	case *ClickHouseManager:
		return m.GrantUser(username, dbName)
	default:
		return nil
	}
}
