package gitforge

import "testing"

func TestFormatRepoTime(t *testing.T) {
	if got := formatRepoTime("2024-07-19T12:00:00Z"); got != "Jul 19, 2024" {
		t.Fatalf("got %q", got)
	}
	if got := formatRepoTime(""); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}
