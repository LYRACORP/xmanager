package web

import (
	"fmt"
	"log"

	svcs "github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

// defaultNodeServices are enabled automatically the first time a node panel starts
// (unless the operator has explicitly disabled them).
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

// ensureDefaultNodeServices starts default stacks in the background.
func (h *handler) ensureDefaultNodeServices() {
	if !h.nodeMode || h.opts.DB == nil {
		return
	}
	go func() {
		exec := h.localExec()
		sid := h.localServerID()
		if sid == 0 {
			return
		}
		for _, name := range defaultNodeServices {
			h.ensureOneDefaultService(exec, sid, name)
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
	if err == nil && !inst.Enabled {
		// Operator explicitly disabled — leave off.
		return
	}

	if svc.IsEnabled(exec) {
		// Already up; make sure DB reflects enabled.
		bd := &svcs.BaseDeployer{DB: h.opts.DB}
		_ = bd.SaveInstance(serverID, name, "running", inst.ConfigJSON)
		return
	}

	log.Printf("node: enabling default service %s…", name)
	if err := svc.Enable(exec, nil); err != nil {
		log.Printf("node: default service %s enable failed: %v", name, err)
		return
	}
	fmt.Printf("node: default service %s enabled\n", name)
}
