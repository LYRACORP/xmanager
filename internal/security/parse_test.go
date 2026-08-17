package security

import (
	"strings"
	"testing"
)

func TestParseUFWStatus(t *testing.T) {
	raw := `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
80/tcp                     ALLOW IN    Anywhere
443/tcp                    ALLOW IN    Anywhere
`
	st := ParseUFWStatus(raw)
	if !st.Active {
		t.Fatal("expected active")
	}
	if len(st.Rules) < 3 {
		t.Fatalf("rules=%d", len(st.Rules))
	}
	if st.Rules[0].Port != "22" || st.Rules[0].Proto != "tcp" {
		t.Fatalf("first rule: %+v", st.Rules[0])
	}
}

func TestParseAuthFailures(t *testing.T) {
	raw := `Aug 17 10:01:01 host sshd[123]: Failed password for invalid user admin from 203.0.113.5 port 54321 ssh2
Aug 17 10:02:01 host sshd[124]: Failed password for root from 198.51.100.2 port 22 ssh2
`
	fails := ParseAuthFailures(raw)
	if len(fails) != 2 {
		t.Fatalf("got %d fails", len(fails))
	}
	if fails[0].IP != "203.0.113.5" {
		t.Fatalf("ip0=%q", fails[0].IP)
	}
	if fails[1].User != "root" && !strings.Contains(fails[1].Detail, "root") {
		t.Fatalf("user1=%q detail=%q", fails[1].User, fails[1].Detail)
	}
}

func TestScanProbe(t *testing.T) {
	if !ScanProbe("/wp-admin/setup.php", "") {
		t.Fatal("expected wp-admin probe")
	}
	if ScanProbe("/api/health", "") {
		t.Fatal("unexpected probe")
	}
}
