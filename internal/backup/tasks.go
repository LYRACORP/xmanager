package backup

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

// Task type constants.
const (
	TaskBackupDatabase  = "backup_database"
	TaskBackupDirectory = "backup_directory"
	TaskCutLog          = "cut_log"
	TaskFullBackup      = "full_backup"
	TaskSyncTime        = "sync_time"
	TaskFreeRAM         = "free_ram"
	TaskAccessURL       = "access_url"
	TaskShell           = "shell"
)

// TaskConfig is stored in Backup.TaskConfig as JSON.
type TaskConfig struct {
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	Compress  bool   `json:"compress,omitempty"`
	KeepLines int    `json:"keep_lines,omitempty"`
	URL       string `json:"url,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	Script    string `json:"script,omitempty"`
}

func ParseTaskConfig(raw string) TaskConfig {
	var c TaskConfig
	_ = json.Unmarshal([]byte(raw), &c)
	return c
}

func (c TaskConfig) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// IsTaskType reports whether typ is one of the scheduled task types (or legacy DB engine).
func IsTaskType(typ string) bool {
	switch typ {
	case TaskBackupDatabase, TaskBackupDirectory, TaskCutLog, TaskFullBackup,
		TaskSyncTime, TaskFreeRAM, TaskAccessURL, TaskShell,
		"all", "postgres", "mysql", "mariadb", "mongodb", "volume":
		return true
	default:
		return false
	}
}

// TaskTypeLabel returns a human-readable label.
func TaskTypeLabel(typ string) string {
	switch typ {
	case TaskBackupDatabase, "postgres", "mysql", "mariadb", "mongodb", "all":
		return "Backup Database"
	case TaskBackupDirectory:
		return "Backup Directory"
	case TaskCutLog:
		return "Cut Log"
	case TaskFullBackup:
		return "Full Backup"
	case TaskSyncTime:
		return "Sync Time"
	case TaskFreeRAM:
		return "Free RAM"
	case TaskAccessURL:
		return "Access URL"
	case TaskShell:
		return "Shell Script"
	default:
		return typ
	}
}

// RunTask executes a scheduled job by type. For multi-DB tasks it returns the last result;
// callers that need per-DB history should use RunSchedule.
func RunTask(exec *ssh.Executor, job storage.Backup) Result {
	cfg := ParseTaskConfig(job.TaskConfig)
	name := strings.TrimSpace(job.Service)
	if cfg.Name != "" {
		name = cfg.Name
	}
	typ := strings.TrimSpace(job.Type)

	switch typ {
	case TaskBackupDatabase, "postgres", "mysql", "mariadb", "mongodb", "all":
		return runBackupDatabaseTask(exec, job)
	case TaskBackupDirectory:
		return runBackupDirectory(exec, name, cfg)
	case TaskCutLog:
		return runCutLog(exec, name, cfg)
	case TaskFullBackup:
		return runFullBackup(exec, name)
	case TaskSyncTime:
		return runSyncTime(exec, name)
	case TaskFreeRAM:
		return runFreeRAM(exec, name)
	case TaskAccessURL:
		return runAccessURL(exec, name, cfg)
	case TaskShell:
		return runShell(exec, name, cfg)
	default:
		// Legacy: treat as DB engine dump of Service.
		if job.Service != "" {
			return RunOne(exec, dbmanager.DBType(typ), job.Service, DefaultDir)
		}
		return Result{TaskType: typ, Name: name, Err: fmt.Errorf("unknown task type %q", typ)}
	}
}

func runBackupDatabaseTask(exec *ssh.Executor, job storage.Backup) Result {
	svc := strings.TrimSpace(job.Service)
	if job.Type == "all" || svc == "__all__" || svc == "all" ||
		(job.Type == TaskBackupDatabase && (svc == "" || svc == "__all__" || svc == "all")) {
		targets := ListDumpable(exec)
		if len(targets) == 0 {
			return Result{TaskType: TaskBackupDatabase, Name: "all", Err: fmt.Errorf("no dumpable databases")}
		}
		var last Result
		for _, t := range targets {
			last = RunOne(exec, t.Type, t.Name, DefaultDir)
			last.TaskType = TaskBackupDatabase
			if last.Err != nil {
				return last
			}
		}
		return last
	}
	engine := job.Type
	dbName := svc
	if engine == TaskBackupDatabase {
		engine = "postgres"
		if parts := strings.SplitN(svc, ":", 2); len(parts) == 2 {
			engine, dbName = parts[0], parts[1]
		}
	}
	res := RunOne(exec, dbmanager.DBType(engine), dbName, DefaultDir)
	res.TaskType = TaskBackupDatabase
	return res
}

// CmdBackupDirectory builds the tar command (exported for tests).
func CmdBackupDirectory(name, dirPath string, compress bool) (cmd, outPath, filename string) {
	ts := time.Now().Format("20060102_150405")
	safe := sanitizeName(name)
	if safe == "" {
		safe = "dir"
	}
	ext := ".tar"
	if compress {
		ext = ".tar.gz"
	}
	filename = fmt.Sprintf("%s_%s%s", safe, ts, ext)
	outPath = path.Join(DefaultDir, filename)
	flag := "cf"
	if compress {
		flag = "czf"
	}
	cmd = fmt.Sprintf("mkdir -p %s && tar %s %s -C %s .",
		shellQuote(DefaultDir), flag, shellQuote(outPath), shellQuote(dirPath))
	return cmd, outPath, filename
}

func runBackupDirectory(exec *ssh.Executor, name string, cfg TaskConfig) Result {
	dirPath := strings.TrimSpace(cfg.Path)
	if dirPath == "" {
		return Result{TaskType: TaskBackupDirectory, Name: name, Err: fmt.Errorf("directory path required")}
	}
	if !path.IsAbs(dirPath) {
		return Result{TaskType: TaskBackupDirectory, Name: name, Err: fmt.Errorf("path must be absolute")}
	}
	compress := cfg.Compress
	cmd, outPath, filename := CmdBackupDirectory(name, dirPath, compress)
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil || (res != nil && res.ExitCode != 0) {
		msg := out
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return Result{TaskType: TaskBackupDirectory, Name: name, Path: outPath, Filename: filename, Output: out, Err: fmt.Errorf("backup directory failed: %s", msg)}
	}
	return Result{
		TaskType: TaskBackupDirectory,
		Name:     name,
		Path:     outPath,
		Filename: filename,
		Size:     fileSize(exec, outPath),
		Output:   out,
	}
}

// CmdCutLog builds the log truncation command.
func CmdCutLog(filePath string, keepLines int) string {
	if keepLines <= 0 {
		keepLines = 1000
	}
	tmp := filePath + ".xmtrim"
	return fmt.Sprintf("tail -n %d %s > %s && mv %s %s",
		keepLines, shellQuote(filePath), shellQuote(tmp), shellQuote(tmp), shellQuote(filePath))
}

func runCutLog(exec *ssh.Executor, name string, cfg TaskConfig) Result {
	filePath := strings.TrimSpace(cfg.Path)
	if filePath == "" {
		return Result{TaskType: TaskCutLog, Name: name, Err: fmt.Errorf("log path required")}
	}
	keep := cfg.KeepLines
	if keep <= 0 {
		keep = 1000
	}
	cmd := CmdCutLog(filePath, keep)
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil || (res != nil && res.ExitCode != 0) {
		msg := out
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return Result{TaskType: TaskCutLog, Name: name, Output: out, Err: fmt.Errorf("cut log failed: %s", msg)}
	}
	return Result{TaskType: TaskCutLog, Name: name, Output: fmt.Sprintf("trimmed %s to last %d lines", filePath, keep)}
}

func runFullBackup(exec *ssh.Executor, name string) Result {
	if name == "" {
		name = "full"
	}
	var notes []string
	for _, t := range ListDumpable(exec) {
		r := RunOne(exec, t.Type, t.Name, DefaultDir)
		if r.Err != nil {
			notes = append(notes, fmt.Sprintf("%s/%s: %v", t.Type, t.Name, r.Err))
		} else {
			notes = append(notes, fmt.Sprintf("%s/%s ok", t.Type, t.Name))
		}
	}
	dirs := []string{"/var/www", "/etc", "/opt/xmanager"}
	ts := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("full_%s_%s.tar.gz", sanitizeName(name), ts)
	outPath := path.Join(DefaultDir, filename)
	var existing []string
	for _, d := range dirs {
		check := exec.RunQuiet(fmt.Sprintf("test -d %s && echo ok", shellQuote(d)))
		if strings.TrimSpace(check) == "ok" {
			existing = append(existing, d)
		}
	}
	if len(existing) > 0 {
		cmd := fmt.Sprintf("mkdir -p %s && tar czf %s %s",
			shellQuote(DefaultDir), shellQuote(outPath), strings.Join(quoteAll(existing), " "))
		res, err := exec.Run(cmd)
		if err != nil || (res != nil && res.ExitCode != 0) {
			msg := ""
			if res != nil {
				msg = strings.TrimSpace(res.Stderr)
			}
			if msg == "" && err != nil {
				msg = err.Error()
			}
			notes = append(notes, "dirs: "+msg)
			return Result{TaskType: TaskFullBackup, Name: name, Output: strings.Join(notes, "; "), Err: fmt.Errorf("full backup partial failure")}
		}
		notes = append(notes, "dirs ok")
		return Result{
			TaskType: TaskFullBackup,
			Name:     name,
			Path:     outPath,
			Filename: filename,
			Size:     fileSize(exec, outPath),
			Output:   strings.Join(notes, "; "),
		}
	}
	return Result{TaskType: TaskFullBackup, Name: name, Output: strings.Join(notes, "; "), Err: fmt.Errorf("no system dirs found")}
}

// CmdSyncTime is the NTP sync command.
func CmdSyncTime() string {
	return `ntpdate -u pool.ntp.org 2>/dev/null || chronyc makestep 2>/dev/null || timedatectl timesync-status 2>/dev/null || date`
}

func runSyncTime(exec *ssh.Executor, name string) Result {
	if name == "" {
		name = "sync-time"
	}
	cmd := CmdSyncTime()
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil && (res == nil || res.ExitCode != 0) {
		return Result{TaskType: TaskSyncTime, Name: name, Output: out, Err: fmt.Errorf("sync time failed: %v", err)}
	}
	return Result{TaskType: TaskSyncTime, Name: name, Output: out}
}

// CmdFreeRAM drops page caches.
func CmdFreeRAM() string {
	return `sync && echo 3 > /proc/sys/vm/drop_caches 2>/dev/null || true; free -h`
}

func runFreeRAM(exec *ssh.Executor, name string) Result {
	if name == "" {
		name = "free-ram"
	}
	cmd := CmdFreeRAM()
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil && (res == nil || res.ExitCode != 0) {
		return Result{TaskType: TaskFreeRAM, Name: name, Output: out, Err: fmt.Errorf("free ram failed: %v", err)}
	}
	return Result{TaskType: TaskFreeRAM, Name: name, Output: out}
}

// CmdAccessURL builds the curl health-check command.
func CmdAccessURL(url string, timeoutSec int) string {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	return fmt.Sprintf("curl -sS --fail --max-time %d %s", timeoutSec, shellQuote(url))
}

func runAccessURL(exec *ssh.Executor, name string, cfg TaskConfig) Result {
	u := strings.TrimSpace(cfg.URL)
	if u == "" {
		return Result{TaskType: TaskAccessURL, Name: name, Err: fmt.Errorf("url required")}
	}
	if name == "" {
		name = u
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	cmd := CmdAccessURL(u, timeout)
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil || (res != nil && res.ExitCode != 0) {
		msg := out
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return Result{TaskType: TaskAccessURL, Name: name, Output: out, Err: fmt.Errorf("access url failed: %s", msg)}
	}
	return Result{TaskType: TaskAccessURL, Name: name, Output: out}
}

func runShell(exec *ssh.Executor, name string, cfg TaskConfig) Result {
	script := strings.TrimSpace(cfg.Script)
	if script == "" {
		return Result{TaskType: TaskShell, Name: name, Err: fmt.Errorf("script required")}
	}
	if name == "" {
		name = "shell"
	}
	cmd := "bash -c " + shellQuote(script)
	res, err := exec.Run(cmd)
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err != nil || (res != nil && res.ExitCode != 0) {
		msg := out
		if msg == "" && err != nil {
			msg = err.Error()
		}
		return Result{TaskType: TaskShell, Name: name, Output: out, Err: fmt.Errorf("shell failed: %s", msg)}
	}
	return Result{TaskType: TaskShell, Name: name, Output: out}
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

func quoteAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = shellQuote(p)
	}
	return out
}

// ParseKeepLines parses keep_lines form value.
func ParseKeepLines(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n <= 0 {
		return 1000
	}
	return n
}

// cronDateStamp is a date(1) format safe inside /etc/cron.d (%% → %).
const cronDateStamp = `$(date +\%Y\%m\%d_\%H\%M\%S)`

// BuildHostCommand returns a shell command suitable for system cron (/etc/cron.d).
// Percent signs in date formats are escaped for cron.
func BuildHostCommand(typ, service string, cfg TaskConfig) (string, error) {
	name := strings.TrimSpace(service)
	if cfg.Name != "" {
		name = cfg.Name
	}
	safe := sanitizeName(name)
	if safe == "" {
		safe = "task"
	}

	switch typ {
	case TaskSyncTime:
		return CmdSyncTime(), nil
	case TaskFreeRAM:
		return CmdFreeRAM(), nil
	case TaskAccessURL:
		u := strings.TrimSpace(cfg.URL)
		if u == "" {
			return "", fmt.Errorf("url required")
		}
		return CmdAccessURL(u, cfg.Timeout), nil
	case TaskCutLog:
		p := strings.TrimSpace(cfg.Path)
		if p == "" {
			return "", fmt.Errorf("log path required")
		}
		return CmdCutLog(p, cfg.KeepLines), nil
	case TaskShell:
		script := strings.TrimSpace(cfg.Script)
		if script == "" {
			return "", fmt.Errorf("script required")
		}
		return "bash -c " + shellQuote(script), nil
	case TaskBackupDirectory:
		dirPath := strings.TrimSpace(cfg.Path)
		if dirPath == "" {
			return "", fmt.Errorf("directory path required")
		}
		if !path.IsAbs(dirPath) {
			return "", fmt.Errorf("path must be absolute")
		}
		ext := ".tar"
		flag := "cf"
		if cfg.Compress {
			ext = ".tar.gz"
			flag = "czf"
		}
		return fmt.Sprintf("TS=%s; mkdir -p %s && tar %s %s/%s-$TS%s -C %s .",
			cronDateStamp, shellQuote(DefaultDir), flag, shellQuote(DefaultDir), safe, ext, shellQuote(dirPath)), nil
	case TaskFullBackup:
		return fmt.Sprintf(
			`TS=%s; mkdir -p %s && tar czf %s/full_%s-$TS.tar.gz /var/www /etc /opt/xmanager 2>/dev/null; `+
				`for d in $(docker exec -u postgres %s psql -Atc "SELECT datname FROM pg_database WHERE datistemplate = false" 2>/dev/null); do `+
				`docker exec -u postgres %s pg_dump "$d" | gzip > %s/full_%s-"$d"-$TS.sql.gz; done || true`,
			cronDateStamp, shellQuote(DefaultDir), shellQuote(DefaultDir), safe,
			dbmanager.ContainerPostgres, dbmanager.ContainerPostgres, shellQuote(DefaultDir), safe,
		), nil
	case TaskBackupDatabase, "postgres", "mysql", "mariadb", "mongodb", "all":
		return buildHostDBCommand(typ, service)
	default:
		if typ == "" {
			return "", fmt.Errorf("task type required")
		}
		return "", fmt.Errorf("unknown task type %q", typ)
	}
}

func buildHostDBCommand(typ, service string) (string, error) {
	svc := strings.TrimSpace(service)
	engine := typ
	dbName := svc
	if typ == TaskBackupDatabase || typ == "all" || svc == "__all__" || svc == "all" {
		if typ == "all" || svc == "__all__" || svc == "all" || svc == "" {
			return fmt.Sprintf(
				`TS=%s; mkdir -p %s; for d in $(docker exec -u postgres %s psql -Atc "SELECT datname FROM pg_database WHERE datistemplate = false"); do docker exec -u postgres %s pg_dump "$d" | gzip > %s/pg-"$d"-$TS.sql.gz; done`,
				cronDateStamp, shellQuote(DefaultDir), dbmanager.ContainerPostgres, dbmanager.ContainerPostgres, shellQuote(DefaultDir),
			), nil
		}
		if parts := strings.SplitN(svc, ":", 2); len(parts) == 2 {
			engine, dbName = parts[0], parts[1]
		} else {
			engine = "postgres"
		}
	}
	if dbName == "" {
		return "", fmt.Errorf("database name required")
	}
	safeDB := sanitizeName(dbName)
	mkdir := fmt.Sprintf("TS=%s; mkdir -p %s", cronDateStamp, shellQuote(DefaultDir))
	switch engine {
	case "postgres", "postgresql":
		return fmt.Sprintf("%s && docker exec -u postgres %s pg_dump %s | gzip > %s/%s-%s-$TS.sql.gz",
			mkdir, dbmanager.ContainerPostgres, shellQuote(dbName), shellQuote(DefaultDir), engine, safeDB), nil
	case "mysql":
		return fmt.Sprintf(
			`%s && docker exec %s sh -c 'mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" %s' | gzip > %s/%s-%s-$TS.sql.gz`,
			mkdir, dbmanager.ContainerMySQL, shellQuote(dbName), shellQuote(DefaultDir), engine, safeDB), nil
	case "mariadb":
		return fmt.Sprintf(
			`%s && docker exec %s sh -c 'PASS="$MARIADB_ROOT_PASSWORD"; [ -n "$PASS" ] || PASS="$MYSQL_ROOT_PASSWORD"; mariadb-dump -uroot -p"$PASS" %s' | gzip > %s/%s-%s-$TS.sql.gz`,
			mkdir, dbmanager.ContainerMariaDB, shellQuote(dbName), shellQuote(DefaultDir), engine, safeDB), nil
	case "mongodb", "mongo":
		return fmt.Sprintf("%s && docker exec %s mongodump --db %s --archive --gzip > %s/%s-%s-$TS.archive.gz",
			mkdir, dbmanager.ContainerMongoDB, shellQuote(dbName), shellQuote(DefaultDir), engine, safeDB), nil
	default:
		return "", fmt.Errorf("unsupported database engine %q", engine)
	}
}
