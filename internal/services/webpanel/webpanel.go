package webpanel

import (
	"fmt"
	"os"
	execcmd "os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const (
	ServiceType = "webpanel"
	installDir  = "/opt/xmanager/webpanel"
	binPath     = "/usr/local/bin/xmanager"
	unitPath    = "/etc/systemd/system/xmanager-web.service"
	configPath  = "/root/.config/xmanager/config.yaml"
)

// WebPanel installs the XManager HTMX web UI on a remote server (systemd + binary).
type WebPanel struct {
	services.BaseDeployer
	serverID uint
	host     string
	sshCfg   ssh.ClientConfig
	pool     *ssh.Pool
	exec     *ssh.Executor
}

func New(db *gorm.DB, serverID uint) *WebPanel {
	return &WebPanel{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (w *WebPanel) SetHost(host string) { w.host = host }

// SetSSH stores credentials used for reconnect / scp / system-ssh fallback.
func (w *WebPanel) SetSSH(cfg ssh.ClientConfig) { w.sshCfg = cfg }

// SetPool enables reconnect after stale sessions (upload / long install).
func (w *WebPanel) SetPool(p *ssh.Pool) { w.pool = p }

func (w *WebPanel) Name() string { return ServiceType }

func (w *WebPanel) IsEnabled(exec *ssh.Executor) bool {
	out := exec.RunQuiet("systemctl is-active xmanager-web 2>/dev/null || true")
	if strings.TrimSpace(out) == "active" {
		return true
	}
	return exec.RunQuiet("docker inspect xmanager-web 2>/dev/null | grep -q running && echo yes") == "yes"
}

func (w *WebPanel) Status(exec *ssh.Executor) string {
	if w.IsEnabled(exec) {
		port := w.readPort(exec)
		host := w.host
		if host == "" {
			host = "server"
		}
		return fmt.Sprintf("running — http://%s:%s", host, port)
	}
	return "stopped"
}

func (w *WebPanel) readPort(exec *ssh.Executor) string {
	out := exec.RunQuiet(`grep -E '^\s*port:' ` + configPath + ` 2>/dev/null | awk '{print $2}' | head -1`)
	out = strings.TrimSpace(out)
	if out == "" {
		return "8080"
	}
	return out
}

// Enable installs or upgrades the panel on the remote host via binary + systemd.
// cfg: port (default 8080), force ("true" to re-upload binary + rewrite node config even if already installed).
func (w *WebPanel) Enable(exec *ssh.Executor, cfg map[string]string) error {
	w.exec = exec
	port := cfg["port"]
	if port == "" {
		port = "8080"
	}
	force := cfg["force"] == "true" || cfg["force"] == "1"

	if err := w.enableBinary(port, force); err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", w.hostOr("host"), port)
	return w.SaveInstance(w.serverID, ServiceType, "running",
		fmt.Sprintf(`{"port":"%s","url":"%s"}`, port, url))
}

// Upgrade force-reinstalls the node panel (new binary + role:node config + restart).
func (w *WebPanel) Upgrade(exec *ssh.Executor, port string) error {
	if port == "" {
		port = "8080"
	}
	return w.Enable(exec, map[string]string{"port": port, "force": "true"})
}

func (w *WebPanel) hostOr(fallback string) string {
	if w.host != "" {
		return w.host
	}
	return fallback
}

func normalizeArch(a string) string {
	a = strings.TrimSpace(strings.ToLower(a))
	switch a {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return a
	}
}

func (w *WebPanel) enableBinary(port string, force bool) error {
	if err := w.run("mkdir -p " + installDir + " /root/.config/xmanager"); err != nil {
		return fmt.Errorf("creating dirs: %w", err)
	}

	if err := w.ensureBinary(force); err != nil {
		return err
	}

	// Upload / SFTP often leaves the pooled session dead — refresh before systemd steps.
	_ = w.reconnect()

	configYAML := fmt.Sprintf(`web:
  enabled: true
  host: "0.0.0.0"
  port: %s
  role: node
ui:
  theme: dark
  refresh_rate: 5
poller:
  interval_sec: 30
  metric_retention: 288
  uptime_interval_sec: 60
`, port)

	cmd := fmt.Sprintf("cat > %s << 'XEOF'\n%sXEOF", configPath, configYAML)
	if err := w.run(cmd); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	unit := fmt.Sprintf(`[Unit]
Description=XManager Node Web Panel
After=network.target

[Service]
Type=simple
User=root
ExecStart=%s web
WorkingDirectory=/root
Restart=on-failure
RestartSec=5
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
`, binPath)

	unitCmd := fmt.Sprintf("cat > %s << 'XEOF'\n%sXEOF", unitPath, unit)
	if err := w.run(unitCmd); err != nil {
		return fmt.Errorf("writing systemd unit: %w", err)
	}

	// Fresh connection again — restart can race with MaxSessions / stale TCP.
	_ = w.reconnect()
	start := "systemctl daemon-reload && systemctl enable xmanager-web && systemctl restart xmanager-web && systemctl is-active xmanager-web"
	if err := w.run(start); err != nil {
		// Last resort: system OpenSSH (independent of the Go pool).
		if err2 := w.runSystemSSH(start); err2 != nil {
			return fmt.Errorf("starting xmanager-web: %v (ssh fallback: %w)", err, err2)
		}
	}
	return nil
}

func isSSHSessionError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, frag := range []string{
		"creating session",
		"connect failed",
		"connection reset",
		"connection lost",
		"broken pipe",
		"eof",
		"use of closed network connection",
		"session already closed",
	} {
		if strings.Contains(s, frag) {
			return true
		}
	}
	return false
}

func (w *WebPanel) reconnect() error {
	if w.pool == nil || w.sshCfg.Host == "" {
		return fmt.Errorf("no pool/ssh config")
	}
	if _, err := w.pool.Reconnect(w.serverID, w.sshCfg); err != nil {
		return err
	}
	exec, ok := w.pool.GetExecutor(w.serverID)
	if !ok {
		return fmt.Errorf("executor missing after reconnect")
	}
	w.exec = exec
	return nil
}

// run executes a remote command; on dead SSH sessions, reconnects once then falls back to system ssh.
func (w *WebPanel) run(cmd string) error {
	if w.exec != nil {
		err := w.runOnce(w.exec, cmd)
		if err == nil {
			return nil
		}
		if !isSSHSessionError(err) {
			return err
		}
		if rerr := w.reconnect(); rerr == nil {
			if err2 := w.runOnce(w.exec, cmd); err2 == nil {
				return nil
			} else if !isSSHSessionError(err2) {
				return err2
			}
		}
	}
	return w.runSystemSSH(cmd)
}

func (w *WebPanel) runOnce(exec *ssh.Executor, cmd string) error {
	res, err := exec.Run(cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stdout + " " + res.Stderr)
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.ExitCode)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (w *WebPanel) runSystemSSH(cmd string) error {
	if w.sshCfg.Host == "" {
		return fmt.Errorf("no SSH config for system-ssh fallback")
	}
	port := w.sshCfg.Port
	if port == 0 {
		port = 22
	}
	keyPath := expandHome(w.sshCfg.KeyPath)
	args := []string{
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=20",
	}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	args = append(args, fmt.Sprintf("%s@%s", w.sshCfg.User, w.sshCfg.Host), cmd)
	out, err := execcmd.Command("ssh", args...).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (w *WebPanel) ensureBinary(force bool) error {
	check := "test -x " + binPath + " && " + binPath + ` web --help >/dev/null 2>&1 && echo yes`
	if !force {
		if w.exec != nil && w.exec.RunQuiet(check) == "yes" {
			return nil
		}
		if out, err := w.systemSSHOutput(check); err == nil && strings.TrimSpace(out) == "yes" {
			return nil
		}
	}

	var errs []string

	if err := w.crossBuildAndUpload(); err == nil {
		return nil
	} else {
		errs = append(errs, "cross-build: "+err.Error())
	}

	remoteArch := ""
	if w.exec != nil {
		remoteArch = normalizeArch(w.exec.RunQuiet("uname -m"))
	}
	if remoteArch == "" {
		if out, err := w.systemSSHOutput("uname -m"); err == nil {
			remoteArch = normalizeArch(out)
		}
	}
	if runtime.GOOS == "linux" && remoteArch == runtime.GOARCH {
		if err := w.uploadFile(mustExecutable()); err == nil {
			return nil
		} else {
			errs = append(errs, "upload local: "+err.Error())
		}
	}

	install := "curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash 2>&1"
	if err := w.run(install); err == nil {
		if w.exec != nil && w.exec.RunQuiet("test -x "+binPath+" && echo yes") == "yes" {
			return nil
		}
		if out, err2 := w.systemSSHOutput("test -x " + binPath + " && echo yes"); err2 == nil && strings.TrimSpace(out) == "yes" {
			return nil
		}
	} else {
		errs = append(errs, "install.sh: "+err.Error())
	}

	return fmt.Errorf("could not install xmanager on remote (%s)", strings.Join(errs, "; "))
}

func (w *WebPanel) systemSSHOutput(cmd string) (string, error) {
	if w.sshCfg.Host == "" {
		return "", fmt.Errorf("no SSH config")
	}
	port := w.sshCfg.Port
	if port == 0 {
		port = 22
	}
	keyPath := expandHome(w.sshCfg.KeyPath)
	args := []string{
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=20",
	}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	args = append(args, fmt.Sprintf("%s@%s", w.sshCfg.User, w.sshCfg.Host), cmd)
	out, err := execcmd.Command("ssh", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func mustExecutable() string {
	exe, _ := os.Executable()
	return exe
}

func (w *WebPanel) crossBuildAndUpload() error {
	goBin, err := execcmd.LookPath("go")
	if err != nil {
		return fmt.Errorf("go not found on local machine")
	}

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	arch := ""
	if w.exec != nil {
		arch = normalizeArch(w.exec.RunQuiet("uname -m"))
	}
	if arch == "" {
		if out, err := w.systemSSHOutput("uname -m"); err == nil {
			arch = normalizeArch(out)
		}
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported remote arch %q", arch)
	}

	tmpDir, err := os.MkdirTemp("", "xmanager-linux-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	out := filepath.Join(tmpDir, "xmanager")
	cmd := execcmd.Command(goBin, "build", "-o", out, "./cmd/xmanager")
	cmd.Dir = modRoot
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH="+arch,
		"CGO_ENABLED=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build linux/%s: %w (%s)", arch, err, strings.TrimSpace(string(output)))
	}

	return w.uploadFile(out)
}

func findModuleRoot() (string, error) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), ".."), filepath.Join(filepath.Dir(exe), "../.."))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(home, "Code/shared/BuildRoom/xmanager"),
		filepath.Join(home, "src/xmanager"),
	)

	for _, c := range candidates {
		if root := findGoModUp(c); root != "" {
			return root, nil
		}
	}

	cmd := execcmd.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/lyracorp/xmanager")
	out, err := cmd.CombinedOutput()
	if err == nil {
		dir := strings.TrimSpace(string(out))
		if dir != "" && dir != "github.com/lyracorp/xmanager" {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return dir, nil
			}
		}
	}

	return "", fmt.Errorf("xmanager module root not found (run from the repo or keep the checkout)")
}

func findGoModUp(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
			if strings.Contains(string(data), "github.com/lyracorp/xmanager") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (w *WebPanel) uploadFile(localPath string) error {
	if err := w.uploadViaSFTP(localPath); err == nil {
		_ = w.reconnect()
		return nil
	} else {
		sftpErr := err
		if err := w.uploadViaSCP(localPath); err == nil {
			_ = w.reconnect()
			return nil
		} else {
			return fmt.Errorf("sftp: %v; scp: %w", sftpErr, err)
		}
	}
}

func (w *WebPanel) uploadViaSFTP(localPath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("reading binary: %w", err)
	}
	if w.exec == nil {
		return fmt.Errorf("no SSH executor")
	}
	client := w.exec.UnderlyingClient()
	if client == nil {
		return fmt.Errorf("no SSH client")
	}
	sftp, err := ssh.NewSFTPClient(client)
	if err != nil {
		return err
	}
	defer sftp.Close()

	if err := sftp.MkdirAll(installDir); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s/xmanager.%d", installDir, time.Now().UnixNano())
	if err := sftp.WriteFile(tmp, data, 0755); err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	return w.run(fmt.Sprintf("mv -f %s %s && chmod +x %s && ln -sfn %s /usr/local/bin/vpsm", tmp, binPath, binPath, binPath))
}

func (w *WebPanel) uploadViaSCP(localPath string) error {
	if w.sshCfg.Host == "" {
		return fmt.Errorf("no SSH config for scp fallback")
	}
	port := w.sshCfg.Port
	if port == 0 {
		port = 22
	}
	keyPath := expandHome(w.sshCfg.KeyPath)
	target := fmt.Sprintf("%s@%s:%s", w.sshCfg.User, w.sshCfg.Host, binPath)

	args := []string{
		"-P", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
	}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	args = append(args, localPath, target)

	cmd := execcmd.Command("scp", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}

	return w.runSystemSSH(fmt.Sprintf(
		"chmod +x %s && ln -sfn %s /usr/local/bin/vpsm && mkdir -p %s /root/.config/xmanager",
		binPath, binPath, installDir,
	))
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func (w *WebPanel) Disable(exec *ssh.Executor) error {
	w.exec = exec
	_ = w.run("systemctl disable --now xmanager-web 2>/dev/null || true")
	_ = w.run("rm -f " + unitPath + " && systemctl daemon-reload 2>/dev/null || true")
	if exec != nil {
		_ = w.ComposeDown(exec, installDir)
		_, _ = exec.Run("docker rm -f xmanager-web 2>/dev/null || true")
	}
	return w.SaveInstance(w.serverID, ServiceType, "stopped", "")
}
