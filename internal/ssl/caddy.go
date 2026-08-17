package ssl

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const caddySitesDir = "/etc/caddy/sites"

func ensureCaddy(exec *ssh.Executor) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	if exec.RunQuiet("which caddy") != "" {
		return ensureCaddyImport(exec)
	}
	res, err := exec.Run("sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy 2>&1")
	if err != nil {
		return fmt.Errorf("install caddy: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("install caddy: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	if exec.RunQuiet("which caddy") == "" {
		return fmt.Errorf("caddy still not found after apt install")
	}
	_, _ = exec.Run("sudo systemctl enable --now caddy 2>/dev/null || true")
	return ensureCaddyImport(exec)
}

func ensureCaddyImport(exec *ssh.Executor) error {
	_, _ = exec.Run("sudo mkdir -p " + caddySitesDir)
	cfg := exec.RunQuiet("cat /etc/caddy/Caddyfile 2>/dev/null")
	if strings.Contains(cfg, caddySitesDir) {
		return nil
	}
	if strings.TrimSpace(cfg) == "" {
		return writeRemoteFile(exec, "/etc/caddy/Caddyfile", "import "+caddySitesDir+"/*\n")
	}
	_, _ = exec.Run(fmt.Sprintf(`echo 'import %s/*' | sudo tee -a /etc/caddy/Caddyfile > /dev/null`, caddySitesDir))
	return nil
}

func caddySiteFile(host string) string {
	return caddySitesDir + "/" + host + ".caddy"
}

func ApplyCaddy(exec *ssh.Executor, host, upstream string) error {
	if err := ensureCaddy(exec); err != nil {
		return err
	}
	if upstream == "" {
		upstream = "http://127.0.0.1:8080"
	}
	body := fmt.Sprintf("%s {\n    reverse_proxy %s\n}\n", host, upstream)
	if err := writeRemoteFile(exec, caddySiteFile(host), body); err != nil {
		return err
	}
	if nm := nginxMgr(exec); nm != nil {
		_ = nm.DisableVHost(host)
	}
	res, err := exec.Run("sudo caddy reload --config /etc/caddy/Caddyfile 2>&1 || sudo systemctl reload caddy 2>&1")
	if err != nil {
		return fmt.Errorf("caddy reload: %w", err)
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("caddy reload: %s", strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return nil
}

// RemoveCaddySite deletes the per-host Caddy site file and reloads.
func RemoveCaddySite(exec *ssh.Executor, host string) {
	if exec == nil || host == "" {
		return
	}
	_, _ = exec.Run(fmt.Sprintf("sudo rm -f %s", caddySiteFile(host)))
	_, _ = exec.Run("sudo caddy reload --config /etc/caddy/Caddyfile 2>/dev/null || sudo systemctl reload caddy 2>/dev/null || true")
}
