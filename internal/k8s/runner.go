package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	PlaybookCluster = "cluster.yml"
	PlaybookScale   = "scale.yml"
	PlaybookUpgrade = "upgrade-cluster.yml"
	PlaybookRemove  = "remove-node.yml"
	PlaybookReset   = "reset.yml"
)

func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func WriteInventoryDir(dir string, spec InventorySpec) error {
	if err := os.MkdirAll(filepath.Join(dir, "group_vars", "k8s_cluster"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "hosts.ini"), []byte(HostsINI(spec)), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "group_vars", "k8s_cluster", "k8s-cluster.yml"), []byte(GroupVars(spec)), 0o600)
}

// DockerRunArgs is the argv after `docker` (no -it). Image tag is independent of kube_version.
func DockerRunArgs(image, invDir, keyPath, user string, become bool, extraVars []string, playbook string) []string {
	if image == "" {
		image = DefaultKubesprayImage
	}
	if user == "" {
		user = "root"
	}
	args := []string{
		"run", "--rm", "--network", "host",
		"-v", invDir + ":/inventory",
		"-v", keyPath + ":/root/.ssh/id_rsa:ro",
		"-e", "ANSIBLE_HOST_KEY_CHECKING=False",
		image,
		"ansible-playbook",
		"-i", "/inventory/hosts.ini",
		"--private-key", "/root/.ssh/id_rsa",
		"--user", user,
		"--ssh-common-args", "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
	}
	if become {
		args = append(args, "-b")
	}
	for _, ev := range extraVars {
		if strings.TrimSpace(ev) == "" {
			continue
		}
		args = append(args, "-e", ev)
	}
	args = append(args, playbook)
	return args
}

func DockerRunCmd(image, invDir, keyPath, user string, become bool, extraVars []string, playbook string) string {
	parts := []string{"docker"}
	for _, a := range DockerRunArgs(image, invDir, keyPath, user, become, extraVars, playbook) {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ") + " 2>&1"
}

func PlaybookExtras(playbook, kubeVersion string) []string {
	var extra []string
	switch playbook {
	case PlaybookReset:
		extra = append(extra, "reset_confirmation=yes")
	case PlaybookUpgrade:
		if kubeVersion != "" {
			extra = append(extra, "kube_version="+kubeVersion)
		}
	}
	if playbook != PlaybookUpgrade && kubeVersion != "" {
		extra = append(extra, "kube_version="+kubeVersion)
	}
	return extra
}

func dockerOK(quiet func(string) string) bool {
	out := quiet("docker info >/dev/null 2>&1 && echo yes")
	return strings.TrimSpace(out) == "yes"
}

func remoteInvDir(clusterID uint) string {
	return fmt.Sprintf("/tmp/xmanager-kubespray-%d", clusterID)
}
