// Package k8s bootstraps a Kubernetes cluster on managed servers using Kubespray
// executed locally via docker run (no agent installed on targets).
package k8s

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/services"
	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

const serviceType = "k8s"
const dir = "/opt/xmanager/services/k8s"

type K8s struct {
	services.BaseDeployer
	serverID uint
}

func New(db *gorm.DB, serverID uint) *K8s {
	return &K8s{BaseDeployer: services.BaseDeployer{DB: db}, serverID: serverID}
}

func (k *K8s) Name() string { return serviceType }

func (k *K8s) IsEnabled(exec *ssh.Executor) bool {
	return exec.RunQuiet("kubectl cluster-info 2>/dev/null | grep -q running && echo yes") == "yes"
}

// Enable runs the Kubespray cluster.yml playbook via Docker to install k8s on
// the current server. cfg keys: ansible_user, ssh_key_path, k8s_version.
func (k *K8s) Enable(exec *ssh.Executor, cfg map[string]string) error {
	ansibleUser := cfg["ansible_user"]
	if ansibleUser == "" {
		ansibleUser = "root"
	}
	sshKeyPath := cfg["ssh_key_path"]
	if sshKeyPath == "" {
		sshKeyPath = "/root/.ssh/id_rsa"
	}
	k8sVersion := cfg["k8s_version"]
	if k8sVersion == "" {
		k8sVersion = "v1.29.4"
	}
	// get the host IP reachable for ansible
	hostIP := k.resolveHost(exec)

	// write a minimal inventory
	inventory := fmt.Sprintf(`[all]
node1 ansible_host=%s

[kube_control_plane]
node1

[etcd]
node1

[kube_node]
node1

[k8s_cluster:children]
kube_control_plane
kube_node
`, hostIP)

	if _, err := exec.Run(fmt.Sprintf("mkdir -p %s/inventory", dir)); err != nil {
		return fmt.Errorf("k8s mkdir: %w", err)
	}
	writeCmd := fmt.Sprintf("cat > %s/inventory/hosts.ini << 'XEOF'\n%s\nXEOF", dir, inventory)
	if res, err := exec.Run(writeCmd); err != nil || res.ExitCode != 0 {
		return fmt.Errorf("k8s write inventory: %w", err)
	}

	// run kubespray via docker on the managed host itself
	kubesprayCmd := fmt.Sprintf(
		"docker run --rm -it --net=host -v %s/inventory:/kubespray/inventory/hosts "+
			"-v %s:/root/.ssh/id_rsa:ro "+
			"quay.io/kubespray/kubespray:%s "+
			"ansible-playbook -i inventory/hosts/hosts.ini "+
			"--user=%s --private-key=/root/.ssh/id_rsa "+
			"-e kube_version=%s cluster.yml 2>&1",
		dir, sshKeyPath, k8sVersion, ansibleUser, k8sVersion,
	)
	res, err := exec.Run(kubesprayCmd)
	if err != nil {
		return fmt.Errorf("kubespray run: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("kubespray failed: %s", res.Stdout+res.Stderr)
	}

	return k.SaveInstance(k.serverID, serviceType, "running",
		fmt.Sprintf(`{"k8s_version":"%s","host":"%s"}`, k8sVersion, hostIP))
}

func (k *K8s) Disable(exec *ssh.Executor) error {
	// run reset.yml to tear down the cluster
	kubesprayReset := fmt.Sprintf(
		"docker run --rm -v %s/inventory:/kubespray/inventory/hosts "+
			"quay.io/kubespray/kubespray:latest "+
			"ansible-playbook -i inventory/hosts/hosts.ini reset.yml -e reset_confirmation=yes 2>&1",
		dir,
	)
	_, _ = exec.Run(kubesprayReset)
	return k.SaveInstance(k.serverID, serviceType, "stopped", "")
}

func (k *K8s) Status(exec *ssh.Executor) string {
	out := exec.RunQuiet("kubectl get nodes --no-headers 2>/dev/null | head -1")
	if out == "" {
		return "stopped"
	}
	return "running"
}

func (k *K8s) resolveHost(exec *ssh.Executor) string {
	ip := exec.RunQuiet("hostname -I 2>/dev/null | awk '{print $1}'")
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "127.0.0.1"
	}
	return ip
}
