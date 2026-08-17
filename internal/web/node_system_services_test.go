package web

import "testing"

func TestParseSystemctlListUnits(t *testing.T) {
	out := `UNIT LOAD ACTIVE SUB DESCRIPTION
nginx.service loaded active running A high performance web server
cron.service loaded active running Regular background program processing daemon
broken.service loaded failed failed Example failed unit
`
	rows := parseSystemctlListUnits(out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	if !rows[0].Running || rows[0].Unit != "nginx.service" {
		t.Fatalf("nginx: %+v", rows[0])
	}
	if !rows[2].Failed {
		t.Fatalf("broken should be failed: %+v", rows[2])
	}
	running := filterSystemServices(rows, "running")
	if len(running) != 2 {
		t.Fatalf("running filter: %d", len(running))
	}
	failed := filterSystemServices(rows, "failed")
	if len(failed) != 1 || failed[0].Unit != "broken.service" {
		t.Fatalf("failed filter: %+v", failed)
	}
}

func TestSanitizeSystemdUnit(t *testing.T) {
	u, err := sanitizeSystemdUnit("nginx")
	if err != nil || u != "nginx.service" {
		t.Fatalf("got %q %v", u, err)
	}
	if _, err := sanitizeSystemdUnit("nginx;rm -rf /"); err == nil {
		t.Fatal("expected reject")
	}
}
