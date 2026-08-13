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
