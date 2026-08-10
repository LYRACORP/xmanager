package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndRenderPrivatebin(t *testing.T) {
	if _, err := os.Stat(filepath.Join(dir, "privatebin.yml")); err != nil {
		dir = "apps/caprover"
		if _, err := os.Stat(filepath.Join(dir, "privatebin.yml")); err != nil {
			t.Skip("one-click apps catalog not present")
		}
	}
	l := NewLoader(dir)
	app, err := l.Load("privatebin")
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "privatebin" {
		t.Fatalf("id %q", app.ID)
	}
	compose, err := l.Render("privatebin", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if compose == "" {
		t.Fatal("empty compose")
	}
}

func TestListSummaries(t *testing.T) {
	dir := "apps/caprover"
	if _, err := os.Stat(dir); err != nil {
		if _, err := os.Stat(dir); err != nil {
			t.Skip("catalog missing")
		}
	}
	l := NewLoader(dir)
	sums, err := l.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) < 1 {
		t.Fatal("expected templates")
	}
}
