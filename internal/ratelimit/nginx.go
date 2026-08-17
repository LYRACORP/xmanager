package ratelimit

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/ssh"
)

const nginxSnippetPath = "/etc/nginx/snippets/xmanager-ratelimit.conf"

// ApplyNginx writes limit_req/limit_conn snippet and patches managed vhosts.
func ApplyNginx(exec *ssh.Executor, p security.Policy) error {
	if exec == nil {
		return nil
	}
	if !p.Enabled || p.RateRPM <= 0 {
		return RemoveNginx(exec)
	}
	rate := p.RateRPM
	if rate <= 0 {
		rate = 120
	}
	burst := p.RateBurst
	if burst <= 0 {
		burst = 30
	}
	conn := p.ConnLimit
	if conn <= 0 {
		conn = 20
	}
	snippet := fmt.Sprintf(`# xmanager-ratelimit
limit_req_zone $binary_remote_addr zone=xmanager_req:10m rate=%dr/m;
limit_conn_zone $binary_remote_addr zone=xmanager_conn:10m;
limit_req zone=xmanager_req burst=%d nodelay;
limit_conn xmanager_conn %d;
`, rate, burst, conn)
	write := fmt.Sprintf("sudo mkdir -p /etc/nginx/snippets && sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", nginxSnippetPath, snippet)
	_ = exec.RunQuiet(write)
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr == nil {
		return fmt.Errorf("nginx unavailable")
	}
	vhosts, err := mgr.ListVHosts()
	if err != nil {
		return err
	}
	for _, v := range vhosts {
		if v.ConfigFile == "" {
			continue
		}
		patch := fmt.Sprintf(`if ! grep -q 'xmanager-ratelimit' %s 2>/dev/null; then sudo sed -i '/location \\/ {/a\\        include %s;' %s; fi`,
			shellSafe(v.ConfigFile), nginxSnippetPath, shellSafe(v.ConfigFile))
		_ = exec.RunQuiet(patch)
	}
	return mgr.ReloadConfig()
}

// RemoveNginx removes rate limit includes.
func RemoveNginx(exec *ssh.Executor) error {
	if exec == nil {
		return nil
	}
	_ = exec.RunQuiet(fmt.Sprintf("sudo rm -f %s", nginxSnippetPath))
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr != nil {
		vhosts, _ := mgr.ListVHosts()
		for _, v := range vhosts {
			if v.ConfigFile != "" {
				_ = exec.RunQuiet(fmt.Sprintf("sudo sed -i '/xmanager-ratelimit/d' %s", shellSafe(v.ConfigFile)))
			}
		}
		_ = mgr.ReloadConfig()
	}
	return nil
}

func shellSafe(s string) string {
	return strings.NewReplacer("'", "", ";", "", "&", "", "|", "", "`", "").Replace(s)
}
