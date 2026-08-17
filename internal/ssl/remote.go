package ssl

import (
	"fmt"
	"path"
	"strings"

	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/ssh"
)

func nginxMgr(exec *ssh.Executor) *proxy.NginxManager {
	if exec == nil {
		return nil
	}
	m := proxy.NewManager(proxy.Nginx, exec)
	if m == nil {
		return nil
	}
	nm, ok := m.(*proxy.NginxManager)
	if !ok {
		return nil
	}
	return nm
}

func writeRemoteFile(exec *ssh.Executor, filePath, content string) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	dir := path.Dir(filePath)
	_, _ = exec.Run("sudo mkdir -p " + dir)
	cmd := fmt.Sprintf("sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", filePath, content)
	res, err := exec.Run(cmd)
	if err != nil {
		return err
	}
	if res != nil && res.ExitCode != 0 {
		return fmt.Errorf("write %s: %s", filePath, strings.TrimSpace(res.Stdout+res.Stderr))
	}
	return nil
}

func trimOutput(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
