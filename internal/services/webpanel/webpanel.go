package webpanel

import (
	"fmt"
	"os"
	"runtime"
	"strings"

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
	host     string // public host for status URL
}

func New(db *gorm.DB, serverID uint) *WebPanel {
	return &WebPanel{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

// SetHost sets the server hostname/IP used in status messages.
func (w *WebPanel) SetHost(host string) { w.host = host }

func (w *WebPanel) Name() string { return ServiceType }

func (w *WebPanel) IsEnabled(exec *ssh.Executor) bool {
	out := exec.RunQuiet("systemctl is-active xmanager-web 2>/dev/null || true")
	if strings.TrimSpace(out) == "active" {
		return true
	}
	// Docker fallback
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

// Enable installs the binary (upload or curl), writes config, and starts systemd.
// cfg keys: port (default 8080), method (binary|docker, default auto).
func (w *WebPanel) Enable(exec *ssh.Executor, cfg map[string]string) error {
	port := cfg["port"]
	if port == "" {
		port = "8080"
	}
	method := cfg["method"]
	if method == "" {
		method = "auto"
	}

	if method == "docker" || (method == "auto" && dockerAvailable(exec) && !binaryInstallPreferred(exec)) {
		if err := w.enableDocker(exec, port); err != nil {
			return err
		}
	} else {
		if err := w.enableBinary(exec, port); err != nil {
			return err
		}
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

func dockerAvailable(exec *ssh.Executor) bool {
	return exec.RunQuiet("docker --version >/dev/null 2>&1 && echo yes") == "yes"
}

func binaryInstallPreferred(exec *ssh.Executor) bool {
	// Prefer binary when we can match arch with the local binary.
	remote := normalizeArch(exec.RunQuiet("uname -m"))
	return remote == runtime.GOARCH && runtime.GOOS == "linux"
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
	// Already installed?
	if exec.RunQuiet("test -x " + binPath + " && echo yes") == "yes" {
		return nil
	}

	remoteArch := normalizeArch(exec.RunQuiet("uname -m"))
	if remoteArch == runtime.GOARCH && runtime.GOOS == "linux" {
		if err := w.uploadLocalBinary(exec); err == nil {
			return nil
		}
	}

	// Fall back to official install script (Linux releases).
	res, err := exec.Run("curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash 2>&1")
	if err != nil {
		return fmt.Errorf("installing xmanager binary: %w", err)
	}
	if res.ExitCode != 0 {
		// Last resort: try uploading local binary anyway (may fail on arch mismatch).
		if upErr := w.uploadLocalBinary(exec); upErr != nil {
			return fmt.Errorf("install failed (%s); upload fallback: %w", strings.TrimSpace(res.Stdout+" "+res.Stderr), upErr)
		}
	}
	return nil
}

func (w *WebPanel) uploadLocalBinary(exec *ssh.Executor) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating local binary: %w", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		return fmt.Errorf("reading local binary: %w", err)
	}

	tmp := installDir + "/xmanager.new"
	// Write via base64 to avoid needing SFTP in Enable signature (works over exec).
	// For large binaries this is heavy — use SFTP when pool provides it via helper.
	_ = data
	_ = tmp

	// Prefer chunked SFTP if the executor's client is available.
	client := exec.UnderlyingClient()
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
	if err := sftp.WriteFile(tmp, data, 0755); err != nil {
		return fmt.Errorf("uploading binary: %w", err)
	}
	res, err := exec.Run(fmt.Sprintf("mv -f %s %s && chmod +x %s && ln -sfn %s /usr/local/bin/vpsm", tmp, binPath, binPath, binPath))
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("installing binary: %s", res.Stderr)
	}
	return nil
}

func (w *WebPanel) enableDocker(exec *ssh.Executor, port string) error {
	if _, err := exec.Run("mkdir -p " + installDir); err != nil {
		return err
	}

	configYAML := fmt.Sprintf(`web:
  enabled: true
  host: "0.0.0.0"
  port: %s
ui:
  theme: dark
`, port)
	cmd := fmt.Sprintf("cat > %s/config.yaml << 'XEOF'\n%sXEOF", installDir, configYAML)
	if _, err := exec.Run(cmd); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	compose := fmt.Sprintf(`services:
  xmanager-web:
    image: ghcr.io/lyracorp/xmanager:latest
    container_name: xmanager-web
    restart: unless-stopped
    command: ["web"]
    ports:
      - "%s:8080"
    volumes:
      - %s/config.yaml:/root/.config/xmanager/config.yaml:ro
      - xmanager_web_data:/var/lib/xmanager
      - /root/.ssh:/root/.ssh:ro
    environment:
      - HOME=/root
volumes:
  xmanager_web_data:
`, port, installDir)

	if err := w.WriteCompose(exec, installDir, compose); err != nil {
		return fmt.Errorf("webpanel docker enable: %w", err)
	}
	return nil
}

func (w *WebPanel) Disable(exec *ssh.Executor) error {
	// Stop systemd if present
	_, _ = exec.Run("systemctl disable --now xmanager-web 2>/dev/null || true")
	_, _ = exec.Run("rm -f " + unitPath + " && systemctl daemon-reload 2>/dev/null || true")

	// Stop docker if present
	_ = w.ComposeDown(exec, installDir)
	_, _ = exec.Run("docker rm -f xmanager-web 2>/dev/null || true")

	return w.SaveInstance(w.serverID, ServiceType, "stopped", "")
}
