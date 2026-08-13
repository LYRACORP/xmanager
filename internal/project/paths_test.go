package project

import (
	"testing"

	"github.com/lyracorp/xmanager/internal/storage"
)

func TestProjectDir(t *testing.T) {
	p := storage.Project{Name: "My App", Type: "git"}
	if got := ProjectDir(p); got != "/opt/xmanager/projects/my-app" {
		t.Fatalf("got %q", got)
	}
	p.Type = "function"
	if got := ProjectDir(p); got != "/opt/xmanager/functions/my-app" {
		t.Fatalf("function got %q", got)
	}
}
