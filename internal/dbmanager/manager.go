package dbmanager

import "github.com/lyracorp/xmanager/internal/ssh"

type DBType string

const (
	PostgreSQL DBType = "postgres"
	MySQL      DBType = "mysql"
	MongoDB    DBType = "mongodb"
)

type Database struct {
	Name  string
	Owner string
	Size  string
}

type DBUser struct {
	Name  string
	Roles string
}

type Manager interface {
	Type() DBType
	IsAvailable() bool
	ListDatabases() ([]Database, error)
	CreateDatabase(name string) error
	DropDatabase(name string) error
	ListUsers() ([]DBUser, error)
	CreateUser(name, password string) error
	Backup(dbName, destPath string) error
	Restore(dbName, srcPath string) error
}

func NewManager(dbType DBType, exec *ssh.Executor) Manager {
	switch dbType {
	case PostgreSQL:
		return &PostgresManager{exec: exec}
	case MySQL:
		return &MySQLManager{exec: exec}
	case MongoDB:
		return &MongoManager{exec: exec}
	default:
		return nil
	}
}
