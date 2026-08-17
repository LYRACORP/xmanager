package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUnderRoot(t *testing.T) {
	root := t.TempDir()
	abs, err := resolveUnderRoot(root, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(abs, filepath.Join("a", "b")) {
		t.Fatalf("got %q", abs)
	}
	if _, err := resolveUnderRoot(root, "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := resolveUnderRoot(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute escape error")
	}
	_ = os.MkdirAll(filepath.Join(root, "ok"), 0o755)
	got, err := resolveUnderRoot(root, "ok")
	if err != nil || !strings.HasSuffix(got, "ok") {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveUnderRootFTPHome(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveUnderRoot(root, "../etc/passwd"); err == nil {
		t.Fatal("expected escape from FTP home")
	}
	if _, err := resolveUnderRoot(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute escape from FTP home")
	}
	nested := filepath.Join(root, "pub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveUnderRoot(root, "pub")
	if err != nil || got != nested && !strings.HasSuffix(got, "pub") {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestResolveContainerPath(t *testing.T) {
	got, err := resolveContainerPath("/app", "")
	if err != nil || got != "/app" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = resolveContainerPath("/app", "src/index.js")
	if err != nil || got != "/app/src/index.js" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := resolveContainerPath("/app", "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := resolveContainerPath("/app", "/etc/passwd"); err == nil {
		t.Fatal("expected absolute escape error")
	}
}
