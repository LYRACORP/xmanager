package dashboard

import "testing"

func TestParseCPUUsage(t *testing.T) {
	top := `%Cpu(s):  5.0 us,  2.0 sy,  0.0 ni, 90.0 id,  0.0 wa`
	got := parseCPUUsage(top)
	if got < 0.09 || got > 0.11 {
		t.Fatalf("expected ~0.10, got %v", got)
	}
}

func TestParseMemUsage(t *testing.T) {
	free := `              total        used        free
Mem:           7993        2048        5945
Swap:          2047           0        2047`
	got := parseMemUsage(free)
	if got < 0.25 || got > 0.27 {
		t.Fatalf("expected ~0.256, got %v", got)
	}
}

func TestParseSystemdEntries(t *testing.T) {
	out := `nginx.service    loaded active running   A high performance web server
mysql.service    loaded failed failed    MySQL Community Server`
	rows := parseSystemdEntries(out)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].kind != "systemd" || !rows[0].active {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].active {
		t.Fatalf("expected mysql inactive")
	}
}

func TestParentPath(t *testing.T) {
	if parentPath("/var/www") != "/var" {
		t.Fatalf("got %q", parentPath("/var/www"))
	}
	if parentPath("/") != "/" {
		t.Fatalf("root parent should stay /")
	}
}
