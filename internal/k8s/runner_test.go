package k8s

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerRunCmdNoTTYSeparateVersions(t *testing.T) {
	cmd := DockerRunCmd(
		"quay.io/kubespray/kubespray:v2.31.0",
		"/tmp/inv",
		"/home/me/.ssh/id_rsa",
		"ubuntu",
		true,
		[]string{"kube_version=v1.34.0"},
		PlaybookCluster,
	)
	if strings.Contains(cmd, " -it ") || strings.Contains(cmd, "'-it'") {
		t.Fatalf("must not use -it: %s", cmd)
	}
	if !strings.Contains(cmd, "quay.io/kubespray/kubespray:v2.31.0") {
		t.Fatalf("missing kubespray image: %s", cmd)
	}
	if strings.Contains(cmd, "kubespray:v1.34") {
		t.Fatal("kube version must not be the image tag")
	}
	if !strings.Contains(cmd, "kube_version=v1.34.0") || !strings.Contains(cmd, "cluster.yml") {
		t.Fatalf("playbook extras: %s", cmd)
	}
	if !strings.Contains(cmd, "'-b'") {
		t.Fatalf("expected become: %s", cmd)
	}
}

func TestWriteInventoryDir(t *testing.T) {
	dir := t.TempDir()
	spec, err := Normalize(InventorySpec{
		Hosts: []Host{{Name: "n1", AnsibleHost: "10.1.1.1", AnsibleUser: "root", ControlPlane: true, Etcd: true, Worker: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteInventoryDir(dir, spec); err != nil {
		t.Fatal(err)
	}
	ini, err := os.ReadFile(filepath.Join(dir, "hosts.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ini), "n1 ansible_host=10.1.1.1") {
		t.Fatalf("%s", ini)
	}
	gv, err := os.ReadFile(filepath.Join(dir, "group_vars", "k8s_cluster", "k8s-cluster.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gv), "container_manager: containerd") {
		t.Fatalf("%s", gv)
	}
}

func TestPlaybookExtras(t *testing.T) {
	reset := PlaybookExtras(PlaybookReset, "v1.34.0")
	joined := strings.Join(reset, ",")
	if !strings.Contains(joined, "reset_confirmation=yes") {
		t.Fatal(joined)
	}
}
