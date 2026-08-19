package k8s

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
)

const (
	DefaultKubesprayImage = "quay.io/kubespray/kubespray:v2.31.0"
	DefaultKubeVersion    = "v1.34.0"
	DefaultNetworkPlugin  = "calico"
	RoleControlPlane      = "control-plane"
	RoleEtcd              = "etcd"
	RoleWorker            = "worker"
)

var invNameRe = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// Host is one Ansible inventory node generated from a fleet server.
type Host struct {
	Name         string `json:"name"`
	ServerID     uint   `json:"server_id"`
	AnsibleHost  string `json:"ansible_host"`
	AnsibleUser  string `json:"ansible_user"`
	AnsiblePort  int    `json:"ansible_port"`
	ControlPlane bool   `json:"control_plane"`
	Etcd         bool   `json:"etcd"`
	Worker       bool   `json:"worker"`
	KeyPath      string `json:"key_path,omitempty"`
}

type InventorySpec struct {
	KubeVersion    string `json:"kube_version"`
	NetworkPlugin  string `json:"network_plugin"`
	KubesprayImage string `json:"kubespray_image"`
	Hosts          []Host `json:"hosts"`
}

func ParseRoles(role string) (cp, etcd, worker bool) {
	for _, p := range strings.Split(strings.ToLower(role), ",") {
		switch strings.TrimSpace(p) {
		case RoleControlPlane, "cp", "master":
			cp = true
		case RoleEtcd:
			etcd = true
		case RoleWorker, "node":
			worker = true
		case "all":
			cp, etcd, worker = true, true, true
		}
	}
	return
}

func RoleString(cp, etcd, worker bool) string {
	var parts []string
	if cp {
		parts = append(parts, RoleControlPlane)
	}
	if etcd {
		parts = append(parts, RoleEtcd)
	}
	if worker {
		parts = append(parts, RoleWorker)
	}
	if len(parts) == 0 {
		return RoleWorker
	}
	return strings.Join(parts, ",")
}

func InventoryName(name string, id uint) string {
	s := invNameRe.ReplaceAllString(strings.TrimSpace(name), "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = fmt.Sprintf("node%d", id)
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "n_" + s
	}
	return s
}

func HostFromServer(srv storage.Server, role string) Host {
	cp, etcd, worker := ParseRoles(role)
	port := srv.Port
	if port == 0 {
		port = 22
	}
	user := srv.User
	if user == "" {
		user = "root"
	}
	return Host{
		Name:         InventoryName(srv.Name, srv.ID),
		ServerID:     srv.ID,
		AnsibleHost:  srv.Host,
		AnsibleUser:  user,
		AnsiblePort:  port,
		ControlPlane: cp,
		Etcd:         etcd,
		Worker:       worker,
		KeyPath:      srv.SSHKeyPath,
	}
}

func Normalize(spec InventorySpec) (InventorySpec, error) {
	if strings.TrimSpace(spec.KubeVersion) == "" {
		spec.KubeVersion = DefaultKubeVersion
	}
	if strings.TrimSpace(spec.NetworkPlugin) == "" {
		spec.NetworkPlugin = DefaultNetworkPlugin
	}
	if strings.TrimSpace(spec.KubesprayImage) == "" {
		spec.KubesprayImage = DefaultKubesprayImage
	}
	if len(spec.Hosts) == 0 {
		return spec, fmt.Errorf("at least one host is required")
	}
	var nCP, nEtcd, nWorker int
	seen := map[string]int{}
	for i := range spec.Hosts {
		h := &spec.Hosts[i]
		if h.Name == "" {
			h.Name = fmt.Sprintf("node%d", h.ServerID)
		}
		if n := seen[h.Name]; n > 0 {
			h.Name = fmt.Sprintf("%s_%d", h.Name, h.ServerID)
		}
		seen[h.Name]++
		if h.AnsiblePort == 0 {
			h.AnsiblePort = 22
		}
		if h.AnsibleUser == "" {
			h.AnsibleUser = "root"
		}
		if h.ControlPlane {
			nCP++
		}
		if h.Etcd {
			nEtcd++
		}
		if h.Worker {
			nWorker++
		}
	}
	if nCP == 0 {
		spec.Hosts[0].ControlPlane = true
		nCP = 1
	}
	if nEtcd == 0 {
		for i := range spec.Hosts {
			if spec.Hosts[i].ControlPlane {
				spec.Hosts[i].Etcd = true
				nEtcd++
			}
		}
	}
	if nWorker == 0 {
		for i := range spec.Hosts {
			if spec.Hosts[i].ControlPlane {
				spec.Hosts[i].Worker = true
			}
		}
	}
	return spec, nil
}

func HostsINI(spec InventorySpec) string {
	var b strings.Builder
	b.WriteString("[all]\n")
	for _, h := range spec.Hosts {
		fmt.Fprintf(&b, "%s ansible_host=%s ansible_user=%s ansible_port=%d\n",
			h.Name, h.AnsibleHost, h.AnsibleUser, h.AnsiblePort)
	}
	b.WriteString("\n[kube_control_plane]\n")
	for _, h := range spec.Hosts {
		if h.ControlPlane {
			b.WriteString(h.Name + "\n")
		}
	}
	b.WriteString("\n[etcd]\n")
	for _, h := range spec.Hosts {
		if h.Etcd {
			b.WriteString(h.Name + "\n")
		}
	}
	b.WriteString("\n[kube_node]\n")
	for _, h := range spec.Hosts {
		if h.Worker {
			b.WriteString(h.Name + "\n")
		}
	}
	b.WriteString("\n[k8s_cluster:children]\nkube_control_plane\nkube_node\n")
	return b.String()
}

func GroupVars(spec InventorySpec) string {
	return fmt.Sprintf("kube_version: %s\nkube_network_plugin: %s\ncontainer_manager: containerd\n",
		spec.KubeVersion, spec.NetworkPlugin)
}

func FirstControlPlane(spec InventorySpec) (Host, bool) {
	for _, h := range spec.Hosts {
		if h.ControlPlane {
			return h, true
		}
	}
	return Host{}, false
}

func SSHKeyPath(spec InventorySpec) string {
	if h, ok := FirstControlPlane(spec); ok && strings.TrimSpace(h.KeyPath) != "" {
		return h.KeyPath
	}
	for _, h := range spec.Hosts {
		if strings.TrimSpace(h.KeyPath) != "" {
			return h.KeyPath
		}
	}
	return ""
}

func NeedsBecome(spec InventorySpec) bool {
	for _, h := range spec.Hosts {
		if h.AnsibleUser != "" && h.AnsibleUser != "root" {
			return true
		}
	}
	return false
}
