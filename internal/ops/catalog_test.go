package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
)

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(db, ssh.NewPool())
}

func TestCatalogListsCoreTools(t *testing.T) {
	c := testCatalog(t)
	if _, ok := c.Get("list_servers"); !ok {
		t.Fatal("list_servers missing")
	}
	if _, ok := c.Get("docker_start"); !ok {
		t.Fatal("docker_start missing")
	}
	if _, ok := c.Get("k8s_list_clusters"); !ok {
		t.Fatal("k8s_list_clusters missing")
	}
	if len(c.List()) < 20 {
		t.Fatalf("expected a broad catalog, got %d", len(c.List()))
	}
}

func TestListServersRedactsPassword(t *testing.T) {
	c := testCatalog(t)
	srv := storage.Server{Name: "box", Host: "192.0.2.10", Port: 22, User: "ubuntu", Password: "secret"}
	if err := c.DB.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	out, err := c.Call(context.Background(), "list_servers", nil, CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if contains(out, "secret") {
		t.Fatal("password leaked")
	}
	if !contains(out, "box") {
		t.Fatal("name missing")
	}
}

func TestDestructiveRequiresConfirm(t *testing.T) {
	c := testCatalog(t)
	srv := storage.Server{Name: "box", Host: "192.0.2.10", User: "ubuntu"}
	if err := c.DB.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	_, err := c.Call(context.Background(), "remove_server", map[string]any{"server_id": float64(srv.ID)}, CallOptions{})
	var need *ConfirmNeededError
	if !errors.As(err, &need) {
		t.Fatalf("want ConfirmNeededError, got %v", err)
	}
	if need.Pending.Tool != "remove_server" {
		t.Fatalf("tool %s", need.Pending.Tool)
	}
	out, err := c.Call(context.Background(), "remove_server", map[string]any{"server_id": float64(srv.ID)}, CallOptions{AllowDestructive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(out, "removed") {
		t.Fatalf("got %q", out)
	}
}

func TestUnknownTool(t *testing.T) {
	c := testCatalog(t)
	_, err := c.Call(context.Background(), "nope", nil, CallOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecutorConnectOnDemandMissingServer(t *testing.T) {
	c := testCatalog(t)
	_, err := c.Executor(9999)
	if err == nil {
		t.Fatal("expected missing server")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
