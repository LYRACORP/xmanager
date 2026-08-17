package web

import "testing"

func TestParseCPUPctAndMemMB(t *testing.T) {
	if parseCPUPct("12.5%") != 12.5 {
		t.Fatal(parseCPUPct("12.5%"))
	}
	if got := parseMemMB("1.5GiB / 2GiB"); got < 1500 || got > 1600 {
		t.Fatalf("gib %v", got)
	}
	if got := parseMemMB("256MiB / 1GiB"); got != 256 {
		t.Fatalf("mib %v", got)
	}
}
