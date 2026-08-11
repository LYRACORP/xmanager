package dbmanager

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type MongoManager struct {
	exec *ssh.Executor
}

func (m *MongoManager) Type() DBType { return MongoDB }

func (m *MongoManager) useDocker() bool {
	return dockerContainerRunning(m.exec, ContainerMongoDB)
}

func (m *MongoManager) IsAvailable() bool {
	if m.useDocker() {
		return true
	}
	return m.exec.RunQuiet("which mongosh 2>/dev/null || which mongo 2>/dev/null") != ""
}

func (m *MongoManager) mongosh(args string) string {
	if m.useDocker() {
		return fmt.Sprintf("mongosh %s", args)
	}
	return fmt.Sprintf("mongosh %s", args)
}

func (m *MongoManager) runMongo(args string) (*ssh.ExecResult, error) {
	cmd := m.mongosh(args)
	if m.useDocker() {
		return dockerExec(m.exec, ContainerMongoDB, cmd)
	}
	return m.exec.Run(cmd)
}

func (m *MongoManager) ListDatabases() ([]Database, error) {
	result, err := m.runMongo("--quiet --eval 'JSON.stringify(db.adminCommand({listDatabases:1}))'")
	if err := requireOK(result, err, "mongosh listDatabases failed"); err != nil {
		return nil, err
	}

	var resp struct {
		Databases []struct {
			Name       string  `json:"name"`
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
	cmd := fmt.Sprintf("%s --quiet --eval 'db.createCollection(\"init\")'", shellQuote(name))
	result, err := m.runMongo(cmd)
	return requireOK(result, err, "create database failed")
}

func (m *MongoManager) DropDatabase(name string) error {
	cmd := fmt.Sprintf("%s --quiet --eval 'db.dropDatabase()'", shellQuote(name))
	result, err := m.runMongo(cmd)
	return requireOK(result, err, "drop database failed")
}

func (m *MongoManager) ListUsers() ([]DBUser, error) {
	result, err := m.runMongo("admin --quiet --eval 'JSON.stringify(db.getUsers())'")
	if err := requireOK(result, err, "mongosh getUsers failed"); err != nil {
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
	eval := fmt.Sprintf(`db.createUser({user:"%s",pwd:"%s",roles:["readWriteAnyDatabase"]})`,
		strings.ReplaceAll(name, `"`, ``), strings.ReplaceAll(password, `"`, ``))
	result, err := m.runMongo("admin --quiet --eval " + shellQuote(eval))
	return requireOK(result, err, "create user failed")
}

func (m *MongoManager) Backup(dbName, destPath string) error {
	if m.useDocker() {
		cmd := fmt.Sprintf("docker exec %s mongodump --db %s --archive --gzip > %s",
			ContainerMongoDB, shellQuote(dbName), shellQuote(destPath))
		result, err := m.exec.Run(cmd)
		return requireOK(result, err, "backup failed")
	}
	cmd := fmt.Sprintf("mongodump --db %s --archive=%s --gzip", shellQuote(dbName), shellQuote(destPath))
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "backup failed")
}

func (m *MongoManager) Restore(dbName, srcPath string) error {
	if m.useDocker() {
		cmd := fmt.Sprintf("docker exec -i %s mongorestore --db %s --archive --gzip < %s",
			ContainerMongoDB, shellQuote(dbName), shellQuote(srcPath))
		result, err := m.exec.Run(cmd)
		return requireOK(result, err, "restore failed")
	}
	cmd := fmt.Sprintf("mongorestore --db %s --archive=%s --gzip", shellQuote(dbName), shellQuote(srcPath))
	result, err := m.exec.Run(cmd)
	return requireOK(result, err, "restore failed")
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
