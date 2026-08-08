package webpanel

import (
	"fmt"
	"os"
	execcmd "os/exec"
	"path/filepath"
	"runtime"
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
}

func New(db *gorm.DB, serverID uint) *WebPanel {
	return &WebPanel{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (w *WebPanel) SetHost(host string) { w.host = host }

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

// Enable installs the panel on the remote host.
// cfg: port (default 8080), method (binary|docker|auto — default binary).
// Auto/binary never pulls ghcr.io (image may not be published); use method=docker explicitly.
func (w *WebPanel) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "8080"
	}
	method := cfg["method"]
	if method == "" || method == "auto" {
		method = "binary"
	}

	var err error
	switch method {
	case "docker":
		err = w.enableDocker(exec, port)
	default:
		err = w.enableBinary(exec, port)
	}
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s:%s", w.hostOr("host"), port)
	return w.SaveInstance(w.serverID, ServiceType, "running",
		fmt.Sprintf(`{"port":"%s","url":"%s"}`, port, url))
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

func (w *WebPanel) enableBinary(exec *ssh.Executor, port string) error {
	if _, err := exec.Run("mkdir -p " + installDir + " /root/.config/xmanager"); err != nil {
		return fmt.Errorf("creating dirs: %w", err)
	}

	if err := w.ensureBinary(exec); err != nil {
		return err
	}

	configYAML := fmt.Sprintf(`web:
  enabled: true
  host: "0.0.0.0"
  port: %s
ui:
  theme: dark
  refresh_rate: 5
poller:
  interval_sec: 30
  metric_retention: 288
  uptime_interval_sec: 60
`, port)

	cmd := fmt.Sprintf("cat > %s << 'XEOF'\n%sXEOF", configPath, configYAML)
	if res, err := exec.Run(cmd); err != nil {
		return fmt.Errorf("writing config: %w", err)
	} else if res.ExitCode != 0 {
		return fmt.Errorf("writing config: %s", res.Stderr)
	}

	unit := fmt.Sprintf(`[Unit]
Description=XManager Web Panel
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
	if res, err := exec.Run(unitCmd); err != nil {
		return fmt.Errorf("writing systemd unit: %w", err)
	} else if res.ExitCode != 0 {
		return fmt.Errorf("writing systemd unit: %s", res.Stderr)
	}

	if res, err := exec.Run("systemctl daemon-reload && systemctl enable --now xmanager-web 2>&1"); err != nil {
		return fmt.Errorf("starting xmanager-web: %w", err)
	} else if res.ExitCode != 0 {
		return fmt.Errorf("starting xmanager-web: %s%s", res.Stdout, res.Stderr)
	}
	return nil
}

func (w *WebPanel) ensureBinary(exec *ssh.Executor) error {
	// Reuse remote binary only if it already has the `web` subcommand.
	if exec.RunQuiet("test -x "+binPath+" && "+binPath+` web --help >/dev/null 2>&1 && echo yes`) == "yes" {
		return nil
	}

	var errs []string

	// 1) Cross-compile Linux binary on this machine (works from macOS) and upload.
	if err := w.crossBuildAndUpload(exec); err == nil {
		return nil
	} else {
		errs = append(errs, "cross-build: "+err.Error())
	}

	// 2) Upload current binary only when already a Linux build of matching arch.
	remoteArch := normalizeArch(exec.RunQuiet("uname -m"))
	if runtime.GOOS == "linux" && remoteArch == runtime.GOARCH {
		if err := w.uploadFile(exec, mustExecutable()); err == nil {
			return nil
		} else {
			errs = append(errs, "upload local: "+err.Error())
		}
	}

	// 3) Remote install.sh (GitHub release — may be older than local).
	res, err := exec.Run("curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash 2>&1")
	if err == nil && res.ExitCode == 0 {
		if exec.RunQuiet("test -x "+binPath+" && echo yes") == "yes" {
			return nil
		}
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
	// Prefer directory of the running binary's build (repo checkout).
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), ".."), filepath.Join(filepath.Dir(exe), "../.."))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	// Common local path when developing
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(home, "Code/shared/BuildRoom/xmanager"),
		filepath.Join(home, "src/xmanager"),
	)

	for _, c := range candidates {
		root := findGoModUp(c)
		if root != "" {
			return root, nil
		}
	}

	// go list -m -f '{{.Dir}}'
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
			// sanity: must be this module
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
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("reading binary: %w", err)
	}
	client := remote.UnderlyingClient()
	if client == nil {
		return fmt.Errorf("no SSH client for binary upload")
	}
	sftp, err := ssh.NewSFTPClient(client)
	if err != nil {
		return fmt.Errorf("sftp: %w", err)
	}
	defer sftp.Close()

	if err := sftp.MkdirAll(installDir); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s/xmanager.%d", installDir, time.Now().UnixNano())
	if err := sftp.WriteFile(tmp, data, 0755); err != nil {
		return fmt.Errorf("uploading binary: %w", err)
	}
	res, err := remote.Run(fmt.Sprintf("mv -f %s %s && chmod +x %s && ln -sfn %s /usr/local/bin/vpsm", tmp, binPath, binPath, binPath))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("installing binary: %s", res.Stderr)
	}
	return nil
}

func (w *WebPanel) enableDocker(exec *ssh.Executor, port string) error {
	return fmt.Errorf("docker method unavailable: ghcr.io/lyracorp/xmanager image is not published; use binary install (default)")
}

func (w *WebPanel) Disable(exec *ssh.Executor) error {
	_, _ = exec.Run("systemctl disable --now xmanager-web 2>/dev/null || true")
	_, _ = exec.Run("rm -f " + unitPath + " && systemctl daemon-reload 2>/dev/null || true")
	_ = w.ComposeDown(exec, installDir)
	_, _ = exec.Run("docker rm -f xmanager-web 2>/dev/null || true")
	return w.SaveInstance(w.serverID, ServiceType, "stopped", "")
}
