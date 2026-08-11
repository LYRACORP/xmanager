package shared

import "testing"

func TestWebPanelProgressStateApply(t *testing.T) {
	var p WebPanelProgressState
	p.Start("upgrade")
	if !p.Active || p.Action != "upgrade" {
		t.Fatalf("start: %+v", p)
	}
	p.Apply(WebPanelProgressMsg{Pct: 0.2, Detail: "Building…"})
	p.Apply(WebPanelProgressMsg{Pct: 0.2, Detail: "Building…"}) // duplicate
	p.Apply(WebPanelProgressMsg{Pct: 0.5, Detail: "Uploading…"})
	if p.Pct != 0.5 || p.Detail != "Uploading…" {
		t.Fatalf("apply: %+v", p)
	}
	if len(p.Log) != 2 {
		t.Fatalf("expected 2 unique log lines, got %v", p.Log)
	}
	view := p.View(40)
	if view == "" {
		t.Fatal("expected non-empty view")
	}
	p.Reset()
	if p.Active || p.View(40) != "" {
		t.Fatalf("reset failed: %+v", p)
	}
}
