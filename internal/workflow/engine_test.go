package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/storage"
)

func TestEngineIfDelay(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat := ops.New(db, nil)
	eng := New(db, cat)
	g := Graph{
		Nodes: []Node{
			{ID: "a", Type: "if", Config: map[string]any{"expr": "true"}},
			{ID: "b", Type: "delay", Config: map[string]any{"seconds": 0}},
		},
		Edges: []Edge{{From: "a", To: "b", Port: "true"}},
	}
	logs, err := eng.execute(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) < 2 {
		t.Fatalf("logs %d", len(logs))
	}
}

func TestChatMatches(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wf := storage.Workflow{Name: "nightly", Trigger: "chat", ChatPhrase: "deploy all", Enabled: true}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	got := ChatMatches(db, "please Deploy All servers")
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestCronMatches(t *testing.T) {
	now := time.Date(2026, 8, 17, 10, 15, 0, 0, time.UTC)
	if !cronMatches("*/15 * * * *", now) {
		t.Fatal("expected match")
	}
	if cronMatches("0 * * * *", now) {
		t.Fatal("did not expect match")
	}
}

func TestHasDestructive(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat := ops.New(db, nil)
	eng := New(db, cat)
	g := Graph{Nodes: []Node{{ID: "x", Type: "remove_server"}}}
	if !eng.HasDestructive(g) {
		t.Fatal("expected destructive")
	}
}
