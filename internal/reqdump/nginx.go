package reqdump

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/ssh"
)

const nginxSnippetPath = "/etc/nginx/snippets/xmanager-reqdump.conf"
const nginxIncludeMarker = "# xmanager-reqdump"

// ConfigureNginxMirror writes mirror snippet and patches XManager vhosts.
func ConfigureNginxMirror(exec *ssh.Executor, sinkURL string) error {
	if exec == nil || strings.TrimSpace(sinkURL) == "" {
		return fmt.Errorf("sink url required")
	}
	snippet := fmt.Sprintf(`# xmanager-reqdump — nginx request mirror (loopback only)
location = /xmanager-reqdump-mirror {
    internal;
    proxy_pass %s;
    proxy_pass_request_body on;
    proxy_set_header X-Original-URI $request_uri;
    proxy_set_header X-Original-Method $request_method;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header Content-Type $content_type;
}
`, sinkURL)
	write := fmt.Sprintf("sudo mkdir -p /etc/nginx/snippets && sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", nginxSnippetPath, snippet)
	if out := exec.RunQuiet(write); strings.Contains(strings.ToLower(out), "error") && strings.TrimSpace(out) != "" {
		return fmt.Errorf("write snippet: %s", out)
	}
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
		patch := fmt.Sprintf(`if ! grep -q 'xmanager-reqdump-mirror' %s 2>/dev/null; then sudo sed -i '/location \\/ {/a\\        mirror /xmanager-reqdump-mirror;\\n        mirror_request_body on;\\n        include %s;' %s; fi`,
			shellSafe(v.ConfigFile), nginxSnippetPath, shellSafe(v.ConfigFile))
		_ = exec.RunQuiet(patch)
	}
	_ = mgr.ReloadConfig()
	return nil
}

// RemoveNginxMirror removes mirror includes and snippet.
func RemoveNginxMirror(exec *ssh.Executor) error {
	if exec == nil {
		return nil
	}
	_ = exec.RunQuiet(fmt.Sprintf("sudo rm -f %s", nginxSnippetPath))
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr != nil {
		vhosts, _ := mgr.ListVHosts()
		for _, v := range vhosts {
			if v.ConfigFile == "" {
				continue
			}
			_ = exec.RunQuiet(fmt.Sprintf("sudo sed -i '/xmanager-reqdump/d' %s", shellSafe(v.ConfigFile)))
		}
		_ = mgr.ReloadConfig()
	}
	return nil
}

func shellSafe(s string) string {
	return strings.NewReplacer("'", "", ";", "", "&", "", "|", "", "`", "").Replace(s)
}
