package hosttime

import (
	"strings"
	"testing"
)

func TestSanitizeTimezone(t *testing.T) {
	ok, err := SanitizeTimezone(" America/New_York ")
	if err != nil || ok != "America/New_York" {
		t.Fatalf("got %q %v", ok, err)
	}
	if _, err := SanitizeTimezone("UTC"); err != nil {
		t.Fatal(err)
	}
	if _, err := SanitizeTimezone("Etc/GMT+5"); err != nil {
		t.Fatal(err)
	}
	if _, err := SanitizeTimezone("foo; rm -rf /"); err == nil {
		t.Fatal("expected reject injection")
	}
	if _, err := SanitizeTimezone("../etc"); err == nil {
		t.Fatal("expected reject ..")
	}
	if _, err := SanitizeTimezone(""); err == nil {
		t.Fatal("expected empty error")
	}
}

func TestParseShow(t *testing.T) {
	out := `Timezone=Europe/London
LocalRTC=no
CanNTP=yes
NTP=yes
NTPSynchronized=yes
TimeUSec=Mon 2026-08-17 11:00:00 BST
`
	st := ParseShow(out)
	if st.Timezone != "Europe/London" {
		t.Fatalf("tz %q", st.Timezone)
	}
	if !st.NTP || !st.NTPSynchronized || st.LocalRTC || !st.CanNTP {
		t.Fatalf("%+v", st)
	}
}

func TestParseTimezones(t *testing.T) {
	zones := ParseTimezones("UTC\nAmerica/New_York\nbad zone\nEurope/Paris\n")
	if len(zones) != 3 {
		t.Fatalf("got %#v", zones)
	}
}

func TestClockLocalValue(t *testing.T) {
	st := Status{LocalTime: "2026-08-17 15:04:05 -0400"}
	if got := st.ClockLocalValue(); got != "2026-08-17T15:04" {
		t.Fatalf("got %q", got)
	}
}

func TestParseClock(t *testing.T) {
	got, err := ParseClock("2026-08-17 15:04:05")
	if err != nil || got != "2026-08-17 15:04:05" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = ParseClock("2026-08-17T15:04")
	if err != nil || !strings.HasPrefix(got, "2026-08-17 15:04") {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ParseClock("not-a-time"); err == nil {
		t.Fatal("expected error")
	}
}
