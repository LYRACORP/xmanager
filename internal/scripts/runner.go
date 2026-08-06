package scripts

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Runner struct {
	pool *ssh.Pool
	db   *gorm.DB
}

func NewRunner(pool *ssh.Pool, db *gorm.DB) *Runner {
	return &Runner{pool: pool, db: db}
}

// RunResult holds per-server output from a script execution.
type RunResult struct {
	ServerID uint
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Run executes script on a single server and saves a ScriptRun record.
func (r *Runner) Run(serverID uint, name, scriptType, content string) (*RunResult, error) {
	exec, ok := r.pool.GetExecutor(serverID)
	if !ok {
		return nil, fmt.Errorf("server %d not connected", serverID)
	}

	cmd, err := buildCmd(scriptType, content)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	res, execErr := exec.Run(cmd)

	rec := &storage.ScriptRun{
		Name:       name,
		ScriptType: scriptType,
		Content:    content,
		TargetIDs:  fmt.Sprintf("%d", serverID),
		StartedAt:  start,
	}
	if execErr != nil {
		rec.ExitCode = -1
		rec.Output = execErr.Error()
		_ = r.db.Create(rec).Error
		return &RunResult{ServerID: serverID, Err: execErr, ExitCode: -1}, execErr
	}
	rec.ExitCode = res.ExitCode
	rec.Output = res.Stdout
	if res.Stderr != "" {
		rec.Output += "\n" + res.Stderr
	}
	_ = r.db.Create(rec).Error

	return &RunResult{
		ServerID: serverID,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
	}, nil
}

// RunMany executes a script on all provided server IDs concurrently.
func (r *Runner) RunMany(serverIDs []uint, name, scriptType, content string) []RunResult {
	cmd, err := buildCmd(scriptType, content)
	if err != nil {
		results := make([]RunResult, len(serverIDs))
		for i, id := range serverIDs {
			results[i] = RunResult{ServerID: id, Err: err, ExitCode: -1}
		}
		return results
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results []RunResult
	)
	start := time.Now()
	idStr := joinIDs(serverIDs)

	for _, id := range serverIDs {
		wg.Add(1)
		go func(serverID uint) {
			defer wg.Done()
			exec, ok := r.pool.GetExecutor(serverID)
			res := RunResult{ServerID: serverID}
			if !ok {
				res.Err = fmt.Errorf("server %d not connected", serverID)
				res.ExitCode = -1
				mu.Lock()
				results = append(results, res)
				mu.Unlock()
				return
			}
			execRes, execErr := exec.Run(cmd)

			rec := &storage.ScriptRun{
				Name:       name,
				ScriptType: scriptType,
				Content:    content,
				TargetIDs:  idStr,
				StartedAt:  start,
			}
			if execErr != nil {
				res.Err = execErr
				res.ExitCode = -1
				rec.ExitCode = -1
				rec.Output = execErr.Error()
			} else {
				res.Stdout = execRes.Stdout
				res.Stderr = execRes.Stderr
				res.ExitCode = execRes.ExitCode
				rec.ExitCode = execRes.ExitCode
				rec.Output = execRes.Stdout
				if execRes.Stderr != "" {
					rec.Output += "\n" + execRes.Stderr
				}
			}
			_ = r.db.Create(rec).Error

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	return results
}

// RunAll executes a script on every server currently connected in the pool.
func (r *Runner) RunAll(name, scriptType, content string) []RunResult {
	return r.RunMany(r.pool.ActiveConnections(), name, scriptType, content)
}

// ListRuns returns recent script run records.
func (r *Runner) ListRuns(limit int) ([]storage.ScriptRun, error) {
	var runs []storage.ScriptRun
	if limit <= 0 {
		limit = 100
	}
	err := r.db.Order("started_at DESC").Limit(limit).Find(&runs).Error
	return runs, err
}

// buildCmd wraps the script content for the requested interpreter.
func buildCmd(scriptType, content string) (string, error) {
	switch strings.ToLower(scriptType) {
	case "bash", "sh", "":
		return fmt.Sprintf("bash -c %s", shellQuote(content)), nil
	case "python", "python3":
		return fmt.Sprintf("python3 -c %s", shellQuote(content)), nil
	case "node", "nodejs":
		return fmt.Sprintf("node -e %s", shellQuote(content)), nil
	default:
		return "", fmt.Errorf("unsupported script type: %s", scriptType)
	}
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

func joinIDs(ids []uint) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, ",")
}
