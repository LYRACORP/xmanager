package nodemetrics

import (
	"strings"
	"testing"
	"time"
)

func TestNetRates(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := t0.Add(5 * time.Second)
	rx, tx := NetRates(1000, 2000, t0, 6000, 7000, t1)
	if rx != 1000 || tx != 1000 { // 5000 bytes / 5s
		t.Fatalf("got rx=%v tx=%v", rx, tx)
	}
	rx, tx = NetRates(100, 100, t0, 50, 50, t1) // counter reset
	if rx != 0 || tx != 0 {
		t.Fatalf("reset: rx=%v tx=%v", rx, tx)
	}
}

func TestParseHumanSize(t *testing.T) {
	cases := map[string]uint64{
		"128 MB":     128 * 1024 * 1024,
		"1.5GB":      uint64(1.5 * 1024 * 1024 * 1024),
		"8192 bytes": 8192,
		"1024":       1024,
		"-":          0,
	}
	for in, want := range cases {
		if got := ParseHumanSize(in); got != want {
			t.Fatalf("%q: got %d want %d", in, got, want)
		}
	}
}

func TestFormatRate(t *testing.T) {
	s := FormatRate(1024)
	if !strings.Contains(s, "/s") {
		t.Fatalf("%s", s)
	}
}
