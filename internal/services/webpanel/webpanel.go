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

// ProgressFunc reports install/upgrade progress. pct is 0..1; detail is a short status line.
type ProgressFunc func(pct float64, detail string)

// WebPanel installs the XManager HTMX web UI on a remote server (systemd + binary).
type WebPanel struct {
	services.BaseDeployer
	serverID   uint
	host       string
	sshCfg     ssh.ClientConfig
	pool       *ssh.Pool
	exec       *ssh.Executor
	onProgress ProgressFunc
}

func New(db *gorm.DB, serverID uint) *WebPanel {
	return &WebPanel{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (w *WebPanel) SetHost(host string) { w.host = host }

// SetSSH stores credentials used for reconnect / scp / system-ssh fallback.
func (w *WebPanel) SetSSH(cfg ssh.ClientConfig) { w.sshCfg = cfg }

// SetPool enables reconnect after stale sessions (upload / long install).
func (w *WebPanel) SetPool(p *ssh.Pool) { w.pool = p }

// SetProgress registers a callback for step-by-step install/upgrade progress.
func (w *WebPanel) SetProgress(fn ProgressFunc) { w.onProgress = fn }

func (w *WebPanel) needsSudo() bool {
	u := strings.TrimSpace(w.sshCfg.User)
	return u != "" && u != "root"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// sudoPrefix returns a pipe/sudo prefix for privileged commands (non-root only).
func (w *WebPanel) sudoPrefix() string {
	if pass := w.sshCfg.Password; pass != "" {
		return fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p ''", shellQuote(pass))
	}
	return "sudo -n"
}

// priv wraps cmd so it runs as root when the SSH user is not root.
func (w *WebPanel) priv(cmd string) string {
	if !w.needsSudo() {
		return cmd
	}
	return w.sudoPrefix() + " bash -c " + shellQuote(cmd)
}

// ensurePriv fails early when non-root install cannot elevate.
func (w *WebPanel) ensurePriv() error {
	if !w.needsSudo() {
		return nil
	}
	if strings.TrimSpace(w.sshCfg.Password) != "" {
		// Probe sudo -S with the stored password.
		if err := w.run(w.priv("true")); err != nil {
			return fmt.Errorf("sudo failed for user %q — check the Password on the server entry (same as sudo -i): %w", w.sshCfg.User, err)
		}
		return nil
	}
	if err := w.run("sudo -n true"); err != nil {
		return fmt.Errorf("user %q cannot sudo without a password — store the Ubuntu password on the server entry in the TUI, or configure NOPASSWD sudo", w.sshCfg.User)
	}
	return nil
}

func (w *WebPanel) report(pct float64, detail string) {
	if w.onProgress == nil {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	w.onProgress(pct, detail)
}

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

	w.report(0.02, "Starting web panel install…")
	if err := w.enableBinary(port, force); err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", w.hostOr("host"), port)
	w.report(0.98, "Saving service instance…")
	if err := w.SaveInstance(w.serverID, ServiceType, "running",
		fmt.Sprintf(`{"port":"%s","url":"%s"}`, port, url)); err != nil {
		return err
	}
	w.report(1.0, "Web panel ready")
	return nil
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
	if fields := strings.Fields(a); len(fields) > 0 {
		a = fields[0]
	} else {
		return ""
	}
	switch a {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	default:
		return ""
	}
}

// detectRemoteArch returns amd64 or arm64. Reconnects first — pooled sessions
// often die after SFTP, which made uname return "" and blocked cross-build.
// Defaults to amd64 (typical VPS) when detection fails so Mac→Linux installs still work.
func (w *WebPanel) detectRemoteArch() string {
	_ = w.reconnect()
	try := func(cmd string) string {
		if w.exec != nil {
			if a := normalizeArch(w.exec.RunQuiet(cmd)); a != "" {
				return a
			}
		}
		if out, err := w.systemSSHOutput(cmd); err == nil {
			if a := normalizeArch(out); a != "" {
				return a
			}
		}
		return ""
	}
	for _, cmd := range []string{
		"uname -m",
		"arch 2>/dev/null || true",
		"dpkg --print-architecture 2>/dev/null || true",
	} {
		if a := try(cmd); a != "" {
			return a
		}
	}
	return "amd64"
}

func (w *WebPanel) enableBinary(port string, force bool) error {
	w.report(0.03, "Checking remote privileges…")
	if err := w.ensurePriv(); err != nil {
		return err
	}

	w.report(0.05, "Creating install directories…")
	if err := w.run(w.priv("mkdir -p " + installDir + " /root/.config/xmanager")); err != nil {
		return fmt.Errorf("creating dirs: %w", err)
	}

	w.report(0.10, "Preparing xmanager binary…")
	if err := w.ensureBinary(force); err != nil {
		return err
	}

	// Upload / SFTP often leaves the pooled session dead — refresh before systemd steps.
	w.report(0.55, "Refreshing SSH session…")
	if err := w.reconnect(); err != nil {
		return fmt.Errorf("refreshing SSH after binary upload: %w", err)
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

	w.report(0.60, "Writing node web panel config…")
	tmpCfg := fmt.Sprintf("/tmp/xm-web-config.%d.yaml", time.Now().UnixNano())
	writeTmp := fmt.Sprintf("cat > %s << 'XEOF'\n%sXEOF", tmpCfg, configYAML)
	if err := w.run(writeTmp); err != nil {
		return fmt.Errorf("writing config staging: %w", err)
	}
	if err := w.run(w.priv(fmt.Sprintf("mkdir -p /root/.config/xmanager && mv -f %s %s && chmod 600 %s", tmpCfg, configPath, configPath))); err != nil {
		_, _ = w.exec.Run("rm -f " + tmpCfg)
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

	w.report(0.68, "Installing systemd unit…")
	tmpUnit := fmt.Sprintf("/tmp/xm-web.%d.service", time.Now().UnixNano())
	unitStage := fmt.Sprintf("cat > %s << 'XEOF'\n%sXEOF", tmpUnit, unit)
	if err := w.run(unitStage); err != nil {
		return fmt.Errorf("writing systemd unit staging: %w", err)
	}
	if err := w.run(w.priv(fmt.Sprintf("mv -f %s %s && chmod 644 %s", tmpUnit, unitPath, unitPath))); err != nil {
		_, _ = w.exec.Run("rm -f " + tmpUnit)
		return fmt.Errorf("writing systemd unit: %w", err)
	}

	// Fresh connection again — restart can race with MaxSessions / stale TCP.
	if err := w.reconnect(); err != nil {
		// Still try start via run() (pool retry + system ssh); do not abort solely on this.
		w.report(0.72, "SSH refresh soft-fail, continuing…")
	}
	// Wait for the process to stay up (crash-loop Restart=on-failure can briefly look active).
	w.report(0.78, "Starting xmanager-web service…")
	start := w.priv(`systemctl daemon-reload && systemctl enable xmanager-web && systemctl restart xmanager-web && sleep 2 && systemctl is-active xmanager-web`)
	if err := w.run(start); err != nil {
		// Last resort: system OpenSSH (independent of the Go pool).
		if err2 := w.runSystemSSH(start); err2 != nil {
			logs := ""
			if w.exec != nil {
				logs = w.exec.RunQuiet(w.priv("journalctl -u xmanager-web -n 40 --no-pager 2>/dev/null || true"))
			}
			return fmt.Errorf("starting xmanager-web: %v (ssh fallback: %w)\n%s", err, err2, logs)
		}
	}
	// Confirm listener actually binds (is-active alone is not enough after a panic).
	_ = w.reconnect()
	w.report(0.88, fmt.Sprintf("Verifying listener on :%s…", port))
	listenCheck := fmt.Sprintf(`for i in 1 2 3 4 5; do ss -ltn 2>/dev/null | grep -q ':%s ' && exit 0; sleep 1; done; %s; exit 1`,
		port, w.priv("journalctl -u xmanager-web -n 40 --no-pager"))
	if err := w.run(listenCheck); err != nil {
		if err2 := w.runSystemSSH(listenCheck); err2 != nil {
			return fmt.Errorf("xmanager-web not listening on :%s: %v (ssh fallback: %w)", port, err, err2)
		}
	}

	// Kick default node stacks (gitea, registry, …) over SSH so they don't
	// depend solely on the panel process noticing them after restart.
	w.report(0.93, "Enabling default node stacks…")
	w.enableDefaultNodeStacks()
	return nil
}

// enableDefaultNodeStacks deploys the default-on services on the managed host.
func (w *WebPanel) enableDefaultNodeStacks() {
	_ = w.reconnect()
	if w.exec == nil {
		return
	}
	for _, item := range defaultRemoteStacks(w.DB, w.serverID) {
		if item == nil {
			continue
		}
		st := item.Status(w.exec)
		if st != "" && st != "stopped" {
			continue
		}
		cfg := map[string]string{}
		if item.Name() == "mailinbox" {
			cfg["https_port"] = "8085"
			cfg["smtp_port"] = "25"
		}
		if err := item.Enable(w.exec, cfg); err != nil {
			// Non-fatal: panel is up; node boot ensure will retry.
			fmt.Printf("webpanel: default stack %s: %v\n", item.Name(), err)
		} else {
			fmt.Printf("webpanel: default stack %s enabled\n", item.Name())
		}
	}
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
	cfg := w.sshCfg
	if cfg.Timeout < 25*time.Second {
		cfg.Timeout = 25 * time.Second
	}
	// Drop stale client first; upload / restart often trips MaxStartups or brief blips.
	w.pool.Disconnect(w.serverID)
	if _, err := w.pool.ConnectWithRetry(w.serverID, cfg, 4); err != nil {
		return err
	}
	exec, ok := w.pool.GetExecutor(w.serverID)
	if !ok {
		return fmt.Errorf("executor missing after reconnect")
	}
	w.exec = exec
	return nil
}

// run executes a remote command; on dead SSH sessions, reconnects then falls back to system ssh.
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
	} else if rerr := w.reconnect(); rerr == nil {
		if err2 := w.runOnce(w.exec, cmd); err2 == nil {
			return nil
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
	out, err := w.systemSSHOutput(cmd)
	if err != nil {
		if out == "" {
			return err
		}
		return fmt.Errorf("%s", out)
	}
	return nil
}

func (w *WebPanel) ensureBinary(force bool) error {
	check := "test -x " + binPath + " && " + binPath + ` web --help >/dev/null 2>&1 && echo yes`
	if !force {
		w.report(0.12, "Checking existing remote binary…")
		if w.exec != nil && w.exec.RunQuiet(check) == "yes" {
			w.report(0.50, "Remote binary already present")
			return nil
		}
		if out, err := w.systemSSHOutput(check); err == nil && strings.TrimSpace(out) == "yes" {
			w.report(0.50, "Remote binary already present")
			return nil
		}
	}

	var errs []string

	// Prefer local cross-compile so unreleased fixes (and missing GitHub assets) still install.
	w.report(0.15, "Cross-compiling binary for remote…")
	if err := w.crossBuildAndUpload(); err == nil {
		return nil
	} else {
		errs = append(errs, "cross-build: "+err.Error())
	}

	w.report(0.35, "Trying alternate binary upload…")
	remoteArch := w.detectRemoteArch()
	if runtime.GOOS == "linux" && remoteArch == runtime.GOARCH {
		if err := w.uploadFile(mustExecutable()); err == nil {
			w.report(0.50, "Uploaded local binary")
			return nil
		} else {
			errs = append(errs, "upload local: "+err.Error())
		}
	}

	// Published release installer — only when we cannot build from a checkout.
	// install.sh often 404s when release assets don't match the expected name.
	if _, modErr := findModuleRoot(); modErr != nil {
		w.report(0.40, "Downloading via install.sh…")
		install := "curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash 2>&1"
		if err := w.run(install); err == nil {
			if w.exec != nil && w.exec.RunQuiet("test -x "+binPath+" && echo yes") == "yes" {
				w.report(0.50, "Installed via install.sh")
				return nil
			}
			if out, err2 := w.systemSSHOutput("test -x " + binPath + " && echo yes"); err2 == nil && strings.TrimSpace(out) == "yes" {
				w.report(0.50, "Installed via install.sh")
				return nil
			}
			errs = append(errs, "install.sh ran but binary missing at "+binPath)
		} else {
			errs = append(errs, "install.sh: "+err.Error())
		}
	} else {
		errs = append(errs, "skipped install.sh (building from local checkout; GitHub release tarball may 404)")
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
		"-o", "ConnectTimeout=25",
	}
	if keyPath != "" {
		args = append(args, "-o", "BatchMode=yes", "-i", keyPath)
	} else if w.sshCfg.Password == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, fmt.Sprintf("%s@%s", w.sshCfg.User, w.sshCfg.Host), cmd)

	c := execcmd.Command("ssh", args...)
	cleanup, err := attachSSHPassword(c, w.sshCfg.Password, keyPath)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return "", err
	}
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// attachSSHPassword enables OpenSSH password auth via SSH_ASKPASS when no key is configured.
// BatchMode alone cannot supply passwords — that produced "Permission denied (publickey,password)".
func attachSSHPassword(c *execcmd.Cmd, password, keyPath string) (func(), error) {
	if password == "" || keyPath != "" {
		return nil, nil
	}
	if _, err := execcmd.LookPath("sshpass"); err == nil {
		// Prefer sshpass when present (no askpass display tricks).
		c.Path, _ = execcmd.LookPath("sshpass")
		c.Args = append([]string{"sshpass", "-e"}, c.Args...)
		c.Env = append(os.Environ(), "SSHPASS="+password)
		return nil, nil
	}
	f, err := os.CreateTemp("", "xm-askpass-*.sh")
	if err != nil {
		return nil, fmt.Errorf("ssh password fallback: %w (install sshpass or use an SSH key)", err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$SSH_PASSWORD\"\n"
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	_ = f.Close()
	_ = os.Chmod(f.Name(), 0o700)
	c.Env = append(os.Environ(),
		"SSH_PASSWORD="+password,
		"SSH_ASKPASS="+f.Name(),
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=:0",
	)
	return func() { os.Remove(f.Name()) }, nil
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

	w.report(0.18, "Detecting remote architecture…")
	arch := w.detectRemoteArch()
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported remote arch %q", arch)
	}

	tmpDir, err := os.MkdirTemp("", "xmanager-linux-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	out := filepath.Join(tmpDir, "xmanager")
	w.report(0.22, fmt.Sprintf("Building linux/%s binary…", arch))
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

	w.report(0.40, "Uploading binary to remote…")
	if err := w.uploadFile(out); err != nil {
		return err
	}
	w.report(0.52, "Binary uploaded")
	return nil
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

	// Non-root users cannot write /opt or /usr/local — stage in /tmp then sudo install.
	tmp := fmt.Sprintf("/tmp/xmanager.%d", time.Now().UnixNano())
	total := int64(len(data))
	w.report(0.40, fmt.Sprintf("Uploading binary to remote… 0/%s", formatByteSize(total)))
	lastPct := -1
	err = sftp.WriteFileProgress(tmp, data, 0755, func(written, tot int64) {
		if tot <= 0 {
			return
		}
		frac := float64(written) / float64(tot)
		pct := int(frac * 100)
		// Throttle UI updates to whole percents.
		if pct == lastPct && written != tot {
			return
		}
		lastPct = pct
		// Map upload into 0.40–0.52 of overall progress.
		w.report(0.40+0.12*frac, fmt.Sprintf("Uploading binary to remote… %s/%s (%d%%)",
			formatByteSize(written), formatByteSize(tot), pct))
	})
	if err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	installCmd := fmt.Sprintf(
		"install -m 755 %s %s && ln -sfn %s /usr/local/bin/vpsm && mkdir -p %s /root/.config/xmanager && rm -f %s",
		tmp, binPath, binPath, installDir, tmp,
	)
	if err := w.run(w.priv(installCmd)); err != nil {
		_, _ = w.exec.Run("rm -f " + tmp)
		return err
	}
	return nil
}

func formatByteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
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
	tmp := fmt.Sprintf("/tmp/xmanager.%d", time.Now().UnixNano())
	target := fmt.Sprintf("%s@%s:%s", w.sshCfg.User, w.sshCfg.Host, tmp)

	args := []string{
		"-P", strconv.Itoa(port),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=25",
	}
	if keyPath != "" {
		args = append(args, "-o", "BatchMode=yes", "-i", keyPath)
	} else if w.sshCfg.Password == "" {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, localPath, target)

	cmd := execcmd.Command("scp", args...)
	cleanup, err := attachSSHPassword(cmd, w.sshCfg.Password, keyPath)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(string(out)))
	}

	installCmd := fmt.Sprintf(
		"install -m 755 %s %s && ln -sfn %s /usr/local/bin/vpsm && mkdir -p %s /root/.config/xmanager && rm -f %s",
		tmp, binPath, binPath, installDir, tmp,
	)
	return w.runSystemSSH(w.priv(installCmd))
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
	w.report(0.20, "Stopping xmanager-web…")
	_ = w.run(w.priv("systemctl disable --now xmanager-web 2>/dev/null || true"))
	w.report(0.50, "Removing systemd unit…")
	_ = w.run(w.priv("rm -f " + unitPath + " && systemctl daemon-reload 2>/dev/null || true"))
	if exec != nil {
		w.report(0.75, "Cleaning leftover containers…")
		_ = w.ComposeDown(exec, installDir)
		_, _ = exec.Run("docker rm -f xmanager-web 2>/dev/null || true")
	}
	w.report(0.95, "Updating service record…")
	if err := w.SaveInstance(w.serverID, ServiceType, "stopped", ""); err != nil {
		return err
	}
	w.report(1.0, "Web panel uninstalled")
	return nil
}
