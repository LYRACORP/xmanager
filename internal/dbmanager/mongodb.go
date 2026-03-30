package dbmanager

import (
	"encoding/json"
	"fmt"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type MongoManager struct {
	exec *ssh.Executor
}

func (m *MongoManager) Type() DBType { return MongoDB }

func (m *MongoManager) IsAvailable() bool {
	return m.exec.RunQuiet("which mongosh 2>/dev/null || which mongo 2>/dev/null") != ""
}

func (m *MongoManager) ListDatabases() ([]Database, error) {
	result, err := m.exec.Run("mongosh --quiet --eval 'JSON.stringify(db.adminCommand({listDatabases:1}))' 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Databases []struct {
			Name  string  `json:"name"`
			SizeOnDisk float64 `json:"sizeOnDisk"`
		} `json:"databases"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		return nil, fmt.Errorf("parsing MongoDB output: %w", err)
	}

	dbs := make([]Database, len(resp.Databases))
	for i, d := range resp.Databases {
		dbs[i] = Database{
			Name:  d.Name,
			Owner: "admin",
			Size:  formatBytes(int64(d.SizeOnDisk)),
		}
	}
	return dbs, nil
}

func (m *MongoManager) CreateDatabase(name string) error {
	cmd := fmt.Sprintf("mongosh %s --quiet --eval 'db.createCollection(\"init\")' 2>/dev/null", name)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MongoManager) DropDatabase(name string) error {
	cmd := fmt.Sprintf("mongosh %s --quiet --eval 'db.dropDatabase()' 2>/dev/null", name)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MongoManager) ListUsers() ([]DBUser, error) {
	result, err := m.exec.Run("mongosh admin --quiet --eval 'JSON.stringify(db.getUsers())' 2>/dev/null")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Users []struct {
			User  string `json:"user"`
			Roles []struct {
				Role string `json:"role"`
			} `json:"roles"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &resp); err != nil {
		return nil, nil
	}

	var users []DBUser
	for _, u := range resp.Users {
		roles := ""
		for i, r := range u.Roles {
			if i > 0 {
				roles += ","
			}
			roles += r.Role
		}
		users = append(users, DBUser{Name: u.User, Roles: roles})
	}
	return users, nil
}

func (m *MongoManager) CreateUser(name, password string) error {
	cmd := fmt.Sprintf("mongosh admin --quiet --eval 'db.createUser({user:\"%s\",pwd:\"%s\",roles:[\"readWriteAnyDatabase\"]})' 2>/dev/null", name, password)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MongoManager) Backup(dbName, destPath string) error {
	cmd := fmt.Sprintf("mongodump --db %s --archive=%s --gzip 2>/dev/null", dbName, destPath)
	_, err := m.exec.Run(cmd)
	return err
}

func (m *MongoManager) Restore(dbName, srcPath string) error {
	cmd := fmt.Sprintf("mongorestore --db %s --archive=%s --gzip 2>/dev/null", dbName, srcPath)
	_, err := m.exec.Run(cmd)
	return err
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
