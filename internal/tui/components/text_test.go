package components

import "testing"

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	got := Truncate("abcdefghij", 6)
	if runewidthStringWidth(got) > 6 {
		t.Fatalf("width %d > 6 for %q", runewidthStringWidth(got), got)
	}
	if got[len(got)-len("…"):] != "…" {
		t.Fatalf("expected ellipsis, got %q", got)
	}
}

func TestWrap(t *testing.T) {
	out := Wrap("one two three four five", 10)
	for _, line := range splitNL(out) {
		if runewidthStringWidth(line) > 10 {
			t.Fatalf("line too wide: %q", line)
		}
	}
}

func TestTruncateMiddle(t *testing.T) {
	got := TruncateMiddle("/var/lib/docker/containers/abcdef", 16)
	if runewidthStringWidth(got) > 16 {
		t.Fatalf("too wide: %q", got)
	}
}

func runewidthStringWidth(s string) int {
	return len([]rune(s)) // tests use ASCII only
}

func splitNL(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
