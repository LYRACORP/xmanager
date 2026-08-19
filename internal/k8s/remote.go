package k8s

import (
	"context"
	"fmt"

	"github.com/lyracorp/xmanager/internal/ssh"
)

func RunOnExecutor(ctx context.Context, exec *ssh.Executor, spec InventorySpec, playbook string, extras []string, onLine func(string)) error {
	dir := "/opt/xmanager/services/k8s/inventory"
	if _, err := exec.Run("mkdir -p " + dir + "/group_vars/k8s_cluster"); err != nil {
		return fmt.Errorf("mkdir inventory: %w", err)
	}
	ini := HostsINI(spec)
	gv := GroupVars(spec)
	write := fmt.Sprintf("cat > %s/hosts.ini << 'XMINI'\n%sXMINI\ncat > %s/group_vars/k8s_cluster/k8s-cluster.yml << 'XMGV'\n%sXMGV",
		dir, ini, dir, gv)
	if res, err := exec.Run(write); err != nil || (res != nil && res.ExitCode != 0) {
		return fmt.Errorf("write inventory: %w", err)
	}
	key := SSHKeyPath(spec)
	if key == "" {
		key = "/root/.ssh/id_rsa"
	}
	user := "root"
	if h, ok := FirstControlPlane(spec); ok {
		user = h.AnsibleUser
	}
	cmd := DockerRunCmd(spec.KubesprayImage, dir, key, user, NeedsBecome(spec), extras, playbook)
	return exec.StreamWait(ctx, cmd, onLine)
}
