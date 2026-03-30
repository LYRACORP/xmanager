package backup

import (
	"fmt"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type Runner struct {
	exec *ssh.Executor
}

func NewRunner(exec *ssh.Executor) *Runner {
	return &Runner{exec: exec}
}

func (r *Runner) BackupPostgres(dbName, destDir string) (string, int64, error) {
	timestamp := time.Now().Format("20060102_150405")
	path := fmt.Sprintf("%s/%s_%s.sql.gz", destDir, dbName, timestamp)

	cmd := fmt.Sprintf("sudo -u postgres pg_dump %s | gzip > %s && stat -c %%s %s", dbName, path, path)
	result, err := r.exec.Run(cmd)
	if err != nil {
		return "", 0, fmt.Errorf("postgres backup: %w", err)
	}

	var size int64
	_, _ = fmt.Sscanf(result.Stdout, "%d", &size)
	return path, size, nil
}

func (r *Runner) BackupMySQL(dbName, destDir string) (string, int64, error) {
	timestamp := time.Now().Format("20060102_150405")
	path := fmt.Sprintf("%s/%s_%s.sql.gz", destDir, dbName, timestamp)

	cmd := fmt.Sprintf("mysqldump %s | gzip > %s && stat -c %%s %s", dbName, path, path)
	result, err := r.exec.Run(cmd)
	if err != nil {
		return "", 0, fmt.Errorf("mysql backup: %w", err)
	}

	var size int64
	_, _ = fmt.Sscanf(result.Stdout, "%d", &size)
	return path, size, nil
}

func (r *Runner) BackupMongoDB(dbName, destDir string) (string, int64, error) {
	timestamp := time.Now().Format("20060102_150405")
	path := fmt.Sprintf("%s/%s_%s.archive.gz", destDir, dbName, timestamp)

	cmd := fmt.Sprintf("mongodump --db %s --archive=%s --gzip 2>/dev/null && stat -c %%s %s", dbName, path, path)
	result, err := r.exec.Run(cmd)
	if err != nil {
		return "", 0, fmt.Errorf("mongodb backup: %w", err)
	}

	var size int64
	_, _ = fmt.Sscanf(result.Stdout, "%d", &size)
	return path, size, nil
}

func (r *Runner) BackupDockerVolume(volumeName, destDir string) (string, int64, error) {
	timestamp := time.Now().Format("20060102_150405")
	path := fmt.Sprintf("%s/%s_%s.tar.gz", destDir, volumeName, timestamp)

	cmd := fmt.Sprintf("docker run --rm -v %s:/data -v %s:/backup alpine tar czf /backup/%s_%s.tar.gz -C /data . && stat -c %%s %s",
		volumeName, destDir, volumeName, timestamp, path)
	result, err := r.exec.Run(cmd)
	if err != nil {
		return "", 0, fmt.Errorf("volume backup: %w", err)
	}

	var size int64
	_, _ = fmt.Sscanf(result.Stdout, "%d", &size)
	return path, size, nil
}

func (r *Runner) EnsureDir(path string) error {
	_, err := r.exec.Run(fmt.Sprintf("mkdir -p %s", path))
	return err
}
