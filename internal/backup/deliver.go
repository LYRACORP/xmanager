package backup

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const DefaultDir = "/opt/xmanager/backups"

// DumpableTypes are engines that support dbmanager.Backup.
var DumpableTypes = []dbmanager.DBType{
	dbmanager.PostgreSQL,
	dbmanager.MySQL,
	dbmanager.MariaDB,
	dbmanager.MongoDB,
}

// DestConfig is stored in BackupDestination.ConfigJSON.
type DestConfig struct {
	Host      string `json:"host"`
	Port      string `json:"port"`
	User      string `json:"user"`
	Password  string `json:"password"`
	Path      string `json:"path"`
	KeyPath   string `json:"key_path"`
	ChannelID uint   `json:"channel_id"` // telegram AlertChannel id
	BotToken  string `json:"bot_token"`
	ChatID    string `json:"chat_id"`
}

func ParseDestConfig(raw string) DestConfig {
	var c DestConfig
	_ = json.Unmarshal([]byte(raw), &c)
	return c
}

func (c DestConfig) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// Target is one database to dump.
type Target struct {
	Type dbmanager.DBType
	Name string
}

// ListDumpable returns available dumpable databases on the host.
func ListDumpable(exec *ssh.Executor) []Target {
	var out []Target
	avail := dbmanager.DetectAvailable(exec)
	for _, t := range DumpableTypes {
		if !avail[t] {
			continue
		}
		mgr := dbmanager.NewManager(t, exec)
		if mgr == nil {
			continue
		}
		list, err := mgr.ListDatabases()
		if err != nil {
			continue
		}
		for _, db := range list {
			name := strings.TrimSpace(db.Name)
			if name == "" || skipSystemDB(t, name) {
				continue
			}
			out = append(out, Target{Type: t, Name: name})
		}
	}
	return out
}

func skipSystemDB(t dbmanager.DBType, name string) bool {
	switch t {
	case dbmanager.PostgreSQL:
		return name == "postgres" || name == "template0" || name == "template1"
	case dbmanager.MySQL, dbmanager.MariaDB:
		return name == "mysql" || name == "information_schema" || name == "performance_schema" || name == "sys"
	case dbmanager.MongoDB:
		return name == "admin" || name == "local" || name == "config"
	default:
		return false
	}
}

// Result is one completed dump or task run.
type Result struct {
	Type     dbmanager.DBType
	TaskType string // preferred over Type when set (shell, sync_time, …)
	Name     string
	Path     string
	Filename string
	Size     int64
	Output   string
	Err      error
}

// RunOne dumps a single database into destDir.
func RunOne(exec *ssh.Executor, t dbmanager.DBType, name, destDir string) Result {
	if destDir == "" {
		destDir = DefaultDir
	}
	_, _ = exec.Run(fmt.Sprintf("mkdir -p %s", shellQuote(destDir)))
	ts := time.Now().Format("20060102_150405")
	ext := ".sql.gz"
	if t == dbmanager.MongoDB {
		ext = ".archive.gz"
	}
	filename := fmt.Sprintf("%s-%s-%s%s", t, name, ts, ext)
	path := filepath.ToSlash(filepath.Join(destDir, filename))

	mgr := dbmanager.NewManager(t, exec)
	if mgr == nil {
		return Result{Type: t, Name: name, Err: fmt.Errorf("unsupported engine %s", t)}
	}
	if err := mgr.Backup(name, path); err != nil {
		return Result{Type: t, Name: name, Path: path, Filename: filename, Err: err}
	}
	size := fileSize(exec, path)
	return Result{Type: t, Name: name, Path: path, Filename: filename, Size: size}
}

func fileSize(exec *ssh.Executor, path string) int64 {
	out := exec.RunQuiet(fmt.Sprintf("stat -c %%s %s 2>/dev/null || wc -c < %s", shellQuote(path), shellQuote(path)))
	var n int64
	_, _ = fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

// Deliver uploads a local backup file to a destination via the node executor.
func Deliver(exec *ssh.Executor, db *gorm.DB, dest storage.BackupDestination, localPath, filename string) error {
	cfg := ParseDestConfig(dest.ConfigJSON)
	switch strings.ToLower(dest.Type) {
	case "ftp":
		return deliverFTP(exec, cfg, localPath, filename)
	case "scp":
		return deliverSCP(exec, cfg, localPath, filename)
	case "telegram":
		return deliverTelegram(exec, db, cfg, localPath, filename)
	default:
		return fmt.Errorf("unknown destination type %q", dest.Type)
	}
}

func deliverFTP(exec *ssh.Executor, cfg DestConfig, localPath, filename string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("ftp host required")
	}
	port := cfg.Port
	if port == "" {
		port = "21"
	}
	user := cfg.User
	if user == "" {
		user = "anonymous"
	}
	remoteDir := strings.TrimRight(cfg.Path, "/")
	if remoteDir == "" {
		remoteDir = "/"
	}
	remote := remoteDir + "/" + filename
	// Prefer curl (widely available).
	pass := cfg.Password
	url := fmt.Sprintf("ftp://%s:%s%s", host, port, remote)
	cmd := fmt.Sprintf("curl -sS --fail --ftp-create-dirs -T %s --user %s:%s %s",
		shellQuote(localPath), shellQuote(user), shellQuote(pass), shellQuote(url))
	res, err := exec.Run(cmd)
	if err != nil {
		return fmt.Errorf("ftp: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("ftp failed: %s", msg)
	}
	return nil
}

func deliverSCP(exec *ssh.Executor, cfg DestConfig, localPath, filename string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("scp host required")
	}
	port := cfg.Port
	if port == "" {
		port = "22"
	}
	user := cfg.User
	if user == "" {
		user = "root"
	}
	remoteDir := strings.TrimRight(cfg.Path, "/")
	if remoteDir == "" {
		remoteDir = "~"
	}
	target := fmt.Sprintf("%s@%s:%s/%s", user, host, remoteDir, filename)
	keyArgs := ""
	if cfg.KeyPath != "" {
		keyArgs = fmt.Sprintf("-i %s ", shellQuote(cfg.KeyPath))
	}
	cmd := fmt.Sprintf("scp -o StrictHostKeyChecking=no -P %s %s%s %s",
		shellQuote(port), keyArgs, shellQuote(localPath), shellQuote(target))
	if cfg.Password != "" {
		// sshpass when password auth is configured
		cmd = fmt.Sprintf("sshpass -p %s %s", shellQuote(cfg.Password), cmd)
	}
	res, err := exec.Run(cmd)
	if err != nil {
		return fmt.Errorf("scp: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("scp failed: %s", msg)
	}
	return nil
}

func deliverTelegram(exec *ssh.Executor, db *gorm.DB, cfg DestConfig, localPath, filename string) error {
	token, chat := cfg.BotToken, cfg.ChatID
	if cfg.ChannelID > 0 && db != nil {
		var ch storage.AlertChannel
		if err := db.First(&ch, cfg.ChannelID).Error; err == nil && ch.Type == "telegram" {
			var m map[string]string
			_ = json.Unmarshal([]byte(ch.ConfigJSON), &m)
			if token == "" {
				token = m["bot_token"]
			}
			if chat == "" {
				chat = m["chat_id"]
			}
		}
	}
	if token == "" || chat == "" {
		return fmt.Errorf("telegram bot_token and chat_id required")
	}
	// Telegram Bot API document limit ~50MB — check size first.
	size := fileSize(exec, localPath)
	caption := "XManager backup: " + filename
	if size > 49*1024*1024 {
		tg := notify.NewTelegram(token, chat)
		return tg.Send(notify.Alert{
			Title:    "Backup too large for Telegram",
			Message:  fmt.Sprintf("%s is %d bytes — download from panel or use FTP/SCP. Path: %s", filename, size, localPath),
			Severity: notify.SeverityWarning,
			Service:  "backup",
		})
	}
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", token)
	cmd := fmt.Sprintf("curl -sS --fail -F chat_id=%s -F caption=%s -F document=@%s %s",
		shellQuote(chat), shellQuote(caption), shellQuote(localPath), shellQuote(api))
	res, err := exec.Run(cmd)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}
		return fmt.Errorf("telegram failed: %s", msg)
	}
	return nil
}

// Record persists a backup row.
func Record(db *gorm.DB, serverID uint, r Result, destinations string) storage.Backup {
	status := "success"
	errMsg := ""
	if r.Err != nil {
		status = "failed"
		errMsg = r.Err.Error()
	} else if r.Output != "" {
		errMsg = r.Output
	}
	typ := r.TaskType
	if typ == "" {
		typ = string(r.Type)
	}
	rec := storage.Backup{
		ServerID:     serverID,
		Type:         typ,
		Service:      r.Name,
		Path:         r.Path,
		Filename:     r.Filename,
		Size:         r.Size,
		Status:       status,
		Destinations: destinations,
		Error:        errMsg,
		BackedAt:     time.Now(),
	}
	if db != nil {
		_ = db.Create(&rec).Error
	}
	return rec
}

// SafeBackupPath ensures path is under DefaultDir.
func SafeBackupPath(path string) (string, error) {
	path = filepath.Clean(path)
	root := filepath.Clean(DefaultDir)
	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside backup directory")
	}
	return path, nil
}
