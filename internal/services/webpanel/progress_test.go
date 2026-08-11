package webpanel

import (
	"testing"
)

func TestProgressReportsClampAndForward(t *testing.T) {
	var gotPct float64
	var gotDetail string
	calls := 0
	w := New(nil, 1)
	w.SetProgress(func(pct float64, detail string) {
		calls++
		gotPct = pct
		gotDetail = detail
	})

	w.report(-1, "too low")
	if calls != 1 || gotPct != 0 || gotDetail != "too low" {
		t.Fatalf("clamp low: calls=%d pct=%v detail=%q", calls, gotPct, gotDetail)
	}

	w.report(1.5, "too high")
	if calls != 2 || gotPct != 1 || gotDetail != "too high" {
		t.Fatalf("clamp high: calls=%d pct=%v detail=%q", calls, gotPct, gotDetail)
	}

	w.report(0.42, "mid")
	if calls != 3 || gotPct != 0.42 || gotDetail != "mid" {
		t.Fatalf("mid: calls=%d pct=%v detail=%q", calls, gotPct, gotDetail)
	}

	w.SetProgress(nil)
	w.report(0.9, "noop")
	if calls != 3 {
		t.Fatalf("nil progress should not call: %d", calls)
	}
}
