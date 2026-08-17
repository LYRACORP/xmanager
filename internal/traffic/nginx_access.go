package traffic

import (
	"fmt"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// TailNginxAccess tails nginx access log on the host.
func TailNginxAccess(exec *ssh.Executor, lines int) string {
	if exec == nil {
		return ""
	}
	if lines <= 0 {
		lines = 200
	}
	cmds := []string{
		fmt.Sprintf("tail -n %d /var/log/nginx/access.log 2>/dev/null", lines),
		fmt.Sprintf("journalctl -u nginx -n %d --no-pager 2>/dev/null", lines),
	}
	for _, c := range cmds {
		out := exec.RunQuiet(c)
		if len(out) > 20 {
			return out
		}
	}
	return ""
}

// StartNginxPoller tails nginx access log periodically.
func StartNginxPoller(exec *ssh.Executor, serverID uint, rec *Recorder, stop <-chan struct{}) {
	if exec == nil || rec == nil {
		return
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			text := TailNginxAccess(exec, 300)
			IngestNginxLines(serverID, text, rec)
		}
	}
}
