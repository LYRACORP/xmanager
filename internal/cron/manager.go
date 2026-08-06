package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const cronDir = "/etc/cron.d"

type Manager struct {
	exec *ssh.Executor
	db   *gorm.DB
}

func NewManager(exec *ssh.Executor, db *gorm.DB) *Manager {
	return &Manager{exec: exec, db: db}
}

// List returns all cron jobs stored in the DB for this server (identified by
// matching serverID). It also refreshes NextRun.
func (m *Manager) List(serverID uint) ([]storage.CronJob, error) {
	var jobs []storage.CronJob
	if err := m.db.Where("server_id = ?", serverID).Find(&jobs).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range jobs {
		if jobs[i].Expression != "" {
			if next, err := nextRun(jobs[i].Expression, now); err == nil {
				jobs[i].NextRun = &next
				_ = m.db.Save(&jobs[i]).Error
			}
		}
	}
	return jobs, nil
}

// Add creates the cron file on the server and records the job in the DB.
func (m *Manager) Add(serverID uint, name, expression, command string) (*storage.CronJob, error) {
	if _, err := parseExpr(expression); err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	safeName := sanitizeName(name)
	filePath := fmt.Sprintf("%s/xmanager-%s", cronDir, safeName)
	content := fmt.Sprintf("# managed by xmanager\n%s root %s\n", expression, command)

	if err := m.writeRemoteFile(filePath, content); err != nil {
		return nil, fmt.Errorf("writing cron file: %w", err)
	}

	now := time.Now()
	next, _ := nextRun(expression, now)
	job := &storage.CronJob{
		ServerID:   serverID,
		Name:       name,
		Expression: expression,
		Command:    command,
		Enabled:    true,
		NextRun:    &next,
		Status:     "idle",
	}
	if err := m.db.Create(job).Error; err != nil {
		return nil, fmt.Errorf("saving cron job: %w", err)
	}
	return job, nil
}

// Remove deletes the cron file from the server and the DB record.
func (m *Manager) Remove(jobID uint) error {
	var job storage.CronJob
	if err := m.db.First(&job, jobID).Error; err != nil {
		return fmt.Errorf("cron job not found: %w", err)
	}

	safeName := sanitizeName(job.Name)
	filePath := fmt.Sprintf("%s/xmanager-%s", cronDir, safeName)
	_, _ = m.exec.Run(fmt.Sprintf("rm -f %s", filePath))

	return m.db.Delete(&job).Error
}

// Enable or disable a cron job by rewriting the file with/without comment prefix.
func (m *Manager) SetEnabled(jobID uint, enabled bool) error {
	var job storage.CronJob
	if err := m.db.First(&job, jobID).Error; err != nil {
		return fmt.Errorf("cron job not found: %w", err)
	}

	safeName := sanitizeName(job.Name)
	filePath := fmt.Sprintf("%s/xmanager-%s", cronDir, safeName)

	line := fmt.Sprintf("%s root %s", job.Expression, job.Command)
	if !enabled {
		line = "# " + line
	}
	content := fmt.Sprintf("# managed by xmanager\n%s\n", line)

	if err := m.writeRemoteFile(filePath, content); err != nil {
		return fmt.Errorf("updating cron file: %w", err)
	}

	return m.db.Model(&job).Update("enabled", enabled).Error
}

// Logs greps syslog for recent runs of a named job and saves CronRun records.
func (m *Manager) Logs(serverID, jobID uint) ([]storage.CronRun, error) {
	var job storage.CronJob
	if err := m.db.First(&job, jobID).Error; err != nil {
		return nil, fmt.Errorf("cron job not found: %w", err)
	}

	result, _ := m.exec.Run(
		fmt.Sprintf(`grep -i 'CRON\|cron' /var/log/syslog 2>/dev/null | grep -F %s | tail -50`,
			shellQuote(job.Name)),
	)

	if result != nil && result.Stdout != "" {
		for _, line := range strings.Split(result.Stdout, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			run := &storage.CronRun{
				CronJobID: jobID,
				StartedAt: time.Now(),
				ExitCode:  0,
				Log:       line,
			}
			_ = m.db.Create(run).Error
		}
	}

	var runs []storage.CronRun
	err := m.db.Where("cron_job_id = ?", jobID).Order("started_at DESC").Limit(100).Find(&runs).Error
	return runs, err
}

func (m *Manager) writeRemoteFile(path, content string) error {
	cmd := fmt.Sprintf("echo %s | sudo tee %s > /dev/null && sudo chmod 644 %s",
		shellQuote(content), path, path)
	res, err := m.exec.Run(cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("remote write failed (exit %d): %s", res.ExitCode, res.Stderr)
	}
	return nil
}

// ---- simple 5-field cron expression parser ----

type parsedExpr struct {
	minute  []int
	hour    []int
	dom     []int // day of month
	month   []int
	dow     []int // day of week
}

func parseExpr(expr string) (*parsedExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	dow, err := parseField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("day-of-week: %w", err)
	}
	// normalize Sunday: 7 -> 0
	for i, d := range dow {
		if d == 7 {
			dow[i] = 0
		}
	}
	return &parsedExpr{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func parseField(s string, min, max int) ([]int, error) {
	if s == "*" {
		return makeRange(min, max, 1), nil
	}

	var result []int
	for _, part := range strings.Split(s, ",") {
		if strings.Contains(part, "/") {
			sub := strings.SplitN(part, "/", 2)
			step, err := strconv.Atoi(sub[1])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("bad step in %q", part)
			}
			base := min
			end := max
			if sub[0] != "*" {
				if strings.Contains(sub[0], "-") {
					r := strings.SplitN(sub[0], "-", 2)
					b, err := strconv.Atoi(r[0])
					if err != nil {
						return nil, fmt.Errorf("bad range in %q", part)
					}
					e, err := strconv.Atoi(r[1])
					if err != nil {
						return nil, fmt.Errorf("bad range in %q", part)
					}
					base, end = b, e
				} else {
					b, err := strconv.Atoi(sub[0])
					if err != nil {
						return nil, fmt.Errorf("bad value in %q", part)
					}
					base = b
				}
			}
			result = append(result, makeRange(base, end, step)...)
		} else if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(r[0])
			hi, err2 := strconv.Atoi(r[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("bad range in %q", part)
			}
			result = append(result, makeRange(lo, hi, 1)...)
		} else {
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("bad value in %q", part)
			}
			result = append(result, v)
		}
	}
	return result, nil
}

func makeRange(lo, hi, step int) []int {
	var r []int
	for i := lo; i <= hi; i += step {
		r = append(r, i)
	}
	return r
}

func contains(vals []int, v int) bool {
	for _, x := range vals {
		if x == v {
			return true
		}
	}
	return false
}

// nextRun computes the next scheduled time after `from` for the expression.
// It iterates minute-by-minute up to one year.
func nextRun(expr string, from time.Time) (time.Time, error) {
	pe, err := parseExpr(expr)
	if err != nil {
		return time.Time{}, err
	}

	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := from.Add(366 * 24 * time.Hour)
	for t.Before(limit) {
		if !contains(pe.month, int(t.Month())) {
			t = t.AddDate(0, 1, 0)
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(pe.dom, t.Day()) || !contains(pe.dow, int(t.Weekday())) {
			t = t.AddDate(0, 0, 1)
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			continue
		}
		if !contains(pe.hour, t.Hour()) {
			t = t.Add(time.Hour)
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
			continue
		}
		if !contains(pe.minute, t.Minute()) {
			t = t.Add(time.Minute)
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("no next run found within a year")
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}
