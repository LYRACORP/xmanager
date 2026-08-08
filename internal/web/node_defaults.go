package web

import (
	"log"
	"strings"
	"time"

	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

// defaultNodeServices are started automatically when the node panel boots
// (unless the operator has explicitly disabled them via Disable).
var defaultNodeServices = []string{
	"registry",
	"gitea",
	"rustfs",
	"powerdns",
	"mailinbox",
}

func isDefaultNodeService(name string) bool {
	for _, n := range defaultNodeServices {
		if n == name {
			return true
		}
	}
	return false
}

func isUserDisabledInstance(inst storage.ServiceInstance) bool {
	return strings.Contains(inst.ConfigJSON, `"user_disabled":true`)
}

// ensureDefaultNodeServices starts default stacks in the background (with retries).
func (h *handler) ensureDefaultNodeServices() {
	if !h.nodeMode || h.opts.DB == nil {
		return
	}
	go func() {
		// Give docker/systemd a moment after panel start / first boot.
		delays := []time.Duration{2 * time.Second, 15 * time.Second, 45 * time.Second}
		for i, wait := range delays {
			time.Sleep(wait)
			exec := h.localExec()
			sid := h.localServerID()
			if sid == 0 {
				log.Printf("node: default services skipped (no local server id), attempt %d", i+1)
				continue
			}
			for _, name := range defaultNodeServices {
				h.ensureOneDefaultService(exec, sid, name)
			}
		}
	}()
}

func (h *handler) ensureOneDefaultService(exec *ssh.Executor, serverID uint, name string) {
	svc := h.lookupNodeService(name)
	if svc == nil {
		return
	}

	var inst storage.ServiceInstance
	err := h.opts.DB.Where("server_id = ? AND service_type = ?", serverID, name).First(&inst).Error
	if err == nil && isUserDisabledInstance(inst) {
		return
	}

	// Only treat as done when a container is actually running — not merely a DB flag.
	status := svc.Status(exec)
	running := status != "" && status != "stopped"
	if running {
		bd := &svcs.BaseDeployer{DB: h.opts.DB}
		cfg := inst.ConfigJSON
		if strings.Contains(cfg, "user_disabled") {
			cfg = `{}`
		}
		_ = bd.SaveInstance(serverID, name, "running", cfg)
		return
	}

	log.Printf("node: enabling default service %s (status=%q)…", name, status)
	if err := svc.Enable(exec, map[string]string{}); err != nil {
		log.Printf("node: default service %s enable failed: %v", name, err)
		return
	}
	log.Printf("node: default service %s enabled", name)
}
