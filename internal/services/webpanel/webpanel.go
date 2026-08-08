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
}

func New(db *gorm.DB, serverID uint) *WebPanel {
	return &WebPanel{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (w *WebPanel) SetHost(host string) { w.host = host }

// SetSSH stores credentials used for reconnect / scp fallback.
func (w *WebPanel) SetSSH(cfg ssh.ClientConfig) { w.sshCfg = cfg }

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
	port := cfg["port"]
	if port == "" {
		port = "8080"
	}
	force := cfg["force"] == "true" || cfg["force"] == "1"

	if err := w.enableBinary(exec, port, force); err != nil {
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

func (w *WebPanel) enableBinary(exec *ssh.Executor, port string, force bool) error {
	if err := w.run(exec, "mkdir -p "+installDir+" /root/.config/xmanager"); err != nil {
		return fmt.Errorf("creating dirs: %w", err)
	}

	if err := w.ensureBinary(exec, force); err != nil {
		return err
	}

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
	if err := w.run(exec, cmd); err != nil {
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
	if err := w.run(exec, unitCmd); err != nil {
		return fmt.Errorf("writing systemd unit: %w", err)
	}

	if err := w.run(exec, "systemctl daemon-reload && systemctl enable --now xmanager-web && systemctl restart xmanager-web 2>&1"); err != nil {
		return fmt.Errorf("starting xmanager-web: %w", err)
	}
	return nil
}

// run executes a remote command; surfaces stdout/stderr on non-zero exit.
func (w *WebPanel) run(exec *ssh.Executor, cmd string) error {
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

func (w *WebPanel) ensureBinary(exec *ssh.Executor, force bool) error {
	if !force && exec.RunQuiet("test -x "+binPath+" && "+binPath+` web --help >/dev/null 2>&1 && echo yes`) == "yes" {
		return nil
	}

	var errs []string

	if err := w.crossBuildAndUpload(exec); err == nil {
		return nil
	} else {
		errs = append(errs, "cross-build: "+err.Error())
	}

	remoteArch := normalizeArch(exec.RunQuiet("uname -m"))
	if runtime.GOOS == "linux" && remoteArch == runtime.GOARCH {
		if err := w.uploadFile(exec, mustExecutable()); err == nil {
			return nil
		} else {
			errs = append(errs, "upload local: "+err.Error())
		}
	}

	// Download release directly on the remote host (own TCP, no local SFTP).
	res, err := exec.Run("curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash 2>&1")
	if err == nil && res.ExitCode == 0 && exec.RunQuiet("test -x "+binPath+" && echo yes") == "yes" {
		return nil
	}
	if err != nil {
		errs = append(errs, "install.sh: "+err.Error())
	} else if res != nil {
		errs = append(errs, "install.sh: "+strings.TrimSpace(res.Stdout+" "+res.Stderr))
	}

	return fmt.Errorf("could not install xmanager on remote (%s)", strings.Join(errs, "; "))
}

func mustExecutable() string {
	exe, _ := os.Executable()
	return exe
}

func (w *WebPanel) crossBuildAndUpload(remote *ssh.Executor) error {
	goBin, err := execcmd.LookPath("go")
	if err != nil {
		return fmt.Errorf("go not found on local machine")
	}

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	arch := normalizeArch(remote.RunQuiet("uname -m"))
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

	return w.uploadFile(remote, out)
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

func (w *WebPanel) uploadFile(remote *ssh.Executor, localPath string) error {
	// 1) SFTP over existing connection
	if err := w.uploadViaSFTP(remote, localPath); err == nil {
		return nil
	} else {
		sftpErr := err
		// 2) scp using a fresh local OpenSSH client (avoids dead pooled sessions / disabled subsystem)
		if err := w.uploadViaSCP(localPath); err == nil {
			return nil
		} else {
			return fmt.Errorf("sftp: %v; scp: %w", sftpErr, err)
		}
	}
}

func (w *WebPanel) uploadViaSFTP(remote *ssh.Executor, localPath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("reading binary: %w", err)
	}
	client := remote.UnderlyingClient()
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
	return w.run(remote, fmt.Sprintf("mv -f %s %s && chmod +x %s && ln -sfn %s /usr/local/bin/vpsm", tmp, binPath, binPath, binPath))
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

	// Fix permissions / symlink via a one-shot ssh command (also fresh connection).
	sshArgs := []string{
		"-p", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	}
	if keyPath != "" {
		sshArgs = append(sshArgs, "-i", keyPath)
	}
	sshArgs = append(sshArgs, fmt.Sprintf("%s@%s", w.sshCfg.User, w.sshCfg.Host),
		fmt.Sprintf("chmod +x %s && ln -sfn %s /usr/local/bin/vpsm && mkdir -p %s /root/.config/xmanager", binPath, binPath, installDir))
	sshCmd := execcmd.Command("ssh", sshArgs...)
	out, err = sshCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("post-scp ssh: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	_, _ = exec.Run("systemctl disable --now xmanager-web 2>/dev/null || true")
	_, _ = exec.Run("rm -f " + unitPath + " && systemctl daemon-reload 2>/dev/null || true")
	_ = w.ComposeDown(exec, installDir)
	_, _ = exec.Run("docker rm -f xmanager-web 2>/dev/null || true")
	return w.SaveInstance(w.serverID, ServiceType, "stopped", "")
}
