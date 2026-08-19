package k8s

import (
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/storage"
)

func TestHostsINIGroups(t *testing.T) {
	spec, err := Normalize(InventorySpec{
		KubeVersion:   "v1.34.0",
		NetworkPlugin: "calico",
		Hosts: []Host{
			{Name: "cp1", ServerID: 1, AnsibleHost: "10.0.0.1", AnsibleUser: "ubuntu", ControlPlane: true, Etcd: true, Worker: true, KeyPath: "/keys/id"},
			{Name: "w1", ServerID: 2, AnsibleHost: "10.0.0.2", AnsibleUser: "ubuntu", Worker: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ini := HostsINI(spec)
	for _, want := range []string{
		"[all]",
		"cp1 ansible_host=10.0.0.1 ansible_user=ubuntu ansible_port=22",
		"[kube_control_plane]",
		"cp1",
		"[etcd]",
		"[kube_node]",
		"w1",
		"[k8s_cluster:children]",
		"kube_control_plane",
		"kube_node",
	} {
		if !strings.Contains(ini, want) {
			t.Fatalf("inventory missing %q\n%s", want, ini)
		}
	}
	if strings.Contains(ini, "\nw1\n") && strings.Count(ini[strings.Index(ini, "[kube_control_plane]"):strings.Index(ini, "[etcd]")], "w1") > 0 {
		t.Fatal("worker listed in control plane")
	}
	gv := GroupVars(spec)
	if !strings.Contains(gv, "kube_version: v1.34.0") || !strings.Contains(gv, "kube_network_plugin: calico") {
		t.Fatalf("group_vars: %s", gv)
	}
	if !NeedsBecome(spec) {
		t.Fatal("ubuntu user should need become")
	}
	cp, ok := FirstControlPlane(spec)
	if !ok || cp.Name != "cp1" {
		t.Fatalf("control plane: %+v", cp)
	}
	if SSHKeyPath(spec) != "/keys/id" {
		t.Fatalf("key %q", SSHKeyPath(spec))
	}
}

func TestNormalizePromotesFirstHost(t *testing.T) {
	spec, err := Normalize(InventorySpec{
		Hosts: []Host{{Name: "only", ServerID: 9, AnsibleHost: "192.0.2.10"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.KubeVersion != DefaultKubeVersion || spec.KubesprayImage != DefaultKubesprayImage {
		t.Fatalf("defaults: %+v", spec)
	}
	if !spec.Hosts[0].ControlPlane || !spec.Hosts[0].Etcd || !spec.Hosts[0].Worker {
		t.Fatalf("expected all-in-one roles: %+v", spec.Hosts[0])
	}
}

func TestHostFromServer(t *testing.T) {
	srv := storage.Server{Name: "web-01", Host: "203.0.113.5", User: "root", Port: 2222}
	srv.ID = 3
	h := HostFromServer(srv, "control-plane,etcd")
	if h.Name != "web_01" || h.AnsiblePort != 2222 || !h.ControlPlane || h.Worker {
		t.Fatalf("%+v", h)
	}
	if RoleString(true, true, false) != "control-plane,etcd" {
		t.Fatal(RoleString(true, true, false))
	}
}

func TestInventoryNameSanitizes(t *testing.T) {
	if InventoryName("1bad name!", 4) != "n_1bad_name" {
		t.Fatalf("got %q", InventoryName("1bad name!", 4))
	}
}
