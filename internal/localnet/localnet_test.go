package localnet

import (
	"runtime"
	"testing"
	"time"
)

func TestSample(t *testing.T) {
	c, err := Sample()
	if err != nil {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if c.Iface == "" {
		t.Fatal("empty iface")
	}
	if c.At.IsZero() {
		t.Fatal("zero time")
	}
}

func TestRates(t *testing.T) {
	prev := Counters{Rx: 1000, Tx: 2000, At: time.Now().Add(-time.Second)}
	cur := Counters{Rx: 1000 + 1024, Tx: 2000 + 2048, At: time.Now()}
	rx, tx := Rates(prev, cur)
	if rx < 900 || rx > 1200 {
		t.Fatalf("rx=%v", rx)
	}
	if tx < 1900 || tx > 2200 {
		t.Fatalf("tx=%v", tx)
	}
}

func TestFormatRate(t *testing.T) {
	if got := FormatRate(512); got != "512 B/s" {
		t.Fatal(got)
	}
	if got := FormatRate(2048); got != "2.0 KB/s" {
		t.Fatal(got)
	}
}
