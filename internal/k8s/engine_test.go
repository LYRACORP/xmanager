package k8s

import (
	"context"
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func TestSaveNewAndInventoryJSON(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := storage.Server{Name: "cp", Host: "10.0.0.8", Port: 22, User: "ubuntu", SSHKeyPath: "/tmp/id_rsa"}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	eng := NewEngine(db, ssh.NewPool())
	c, inv, err := eng.SaveNew(CreateSpec{
		Name:    "lab",
		Members: []MemberSpec{{ServerID: srv.ID, Role: "all"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == 0 || c.Status != "pending" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(HostsINI(inv), "ansible_host=10.0.0.8") {
		t.Fatal(HostsINI(inv))
	}
	members, err := eng.Members(c.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("%v %v", members, err)
	}
	list, err := eng.List(0)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
}

func TestRunPlaybookFakeDocker(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := storage.Server{Name: "cp", Host: "10.0.0.8", Port: 22, User: "root", SSHKeyPath: "/tmp/id_rsa"}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	eng := NewEngine(db, ssh.NewPool())
	var sawCmd string
	eng.dockerAvailable = func(*ssh.Executor) bool { return true }
	eng.runDocker = func(ctx context.Context, exec *ssh.Executor, cmd string, onLine func(string)) error {
		sawCmd = cmd
		onLine("PLAY [kubespray]")
		return nil
	}
	eng.fetchAdminConf = func(*ssh.Executor) (string, error) {
		return "apiVersion: v1\nkind: Config\n", nil
	}
	c, _, err := eng.SaveNew(CreateSpec{Name: "lab", Members: []MemberSpec{{ServerID: srv.ID, Role: "all"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.RunPlaybook(context.Background(), c.ID, PlaybookCluster, "", nil); err != nil {
		t.Fatal(err)
	}
	got, _ := eng.Get(c.ID)
	if got.Status != "ready" {
		t.Fatalf("status %s log %s", got.Status, got.LastLog)
	}
	if !strings.Contains(sawCmd, "cluster.yml") || strings.Contains(sawCmd, "-it") {
		t.Fatalf("cmd %s", sawCmd)
	}
	if got.KubeconfigEnc == "" {
		t.Fatal("expected encrypted kubeconfig")
	}
	if strings.Contains(got.LastLog, "apiVersion: v1") {
		t.Fatal("kubeconfig leaked into log")
	}
}

func TestMaskSecretJSON(t *testing.T) {
	raw := `{"kind":"Secret","data":{"token":"abc"},"stringData":{"x":"y"}}`
	out := MaskSecretJSON(raw)
	if strings.Contains(out, "abc") || strings.Contains(out, `"y"`) {
		t.Fatal(out)
	}
	if !strings.Contains(out, "***") {
		t.Fatal(out)
	}
}

func TestParseResourceList(t *testing.T) {
	raw := `{
	  "items": [
	    {"kind":"Node","metadata":{"name":"n1"},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.34.0"}}}
	  ]
	}`
	rows, err := parseResourceList(raw, "Node")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if rows[0].Name != "n1" || rows[0].Status != "Ready" {
		t.Fatalf("%+v", rows[0])
	}
}
