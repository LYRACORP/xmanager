package datetime

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
)

func TestSelectedZone(t *testing.T) {
	if got := selectedZone(table.Row{"● America/New_York"}); got != "America/New_York" {
		t.Fatalf("got %q", got)
	}
	if got := selectedZone(table.Row{"  UTC"}); got != "UTC" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterZones(t *testing.T) {
	zones := []string{"UTC", "America/New_York", "Europe/London", "Europe/Paris"}
	got := filterZones(zones, "europe")
	if len(got) != 2 {
		t.Fatalf("got %#v", got)
	}
	if len(filterZones(zones, "")) != 4 {
		t.Fatal("empty filter")
	}
}
