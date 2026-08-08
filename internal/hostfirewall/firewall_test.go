package hostfirewall

import (
	"strings"
	"testing"
)

func TestParsePortsList(t *testing.T) {
	ports, proto, err := ParsePortsList("80, 443,8080")
	if err != nil {
		t.Fatal(err)
	}
	if proto != "tcp" || len(ports) != 3 || ports[0] != 80 || ports[2] != 8080 {
		t.Fatalf("got ports=%v proto=%s", ports, proto)
	}

	ports, proto, err = ParsePortsList("53/udp")
	if err != nil || proto != "udp" || len(ports) != 1 || ports[0] != 53 {
		t.Fatalf("got ports=%v proto=%s err=%v", ports, proto, err)
	}

	if _, _, err := ParsePortsList(""); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := ParsePortsList("99999"); err == nil {
		t.Fatal("expected range error")
	}
}

func TestDenyProtectsSSH(t *testing.T) {
	err := Deny(22, "tcp", nil)
	if err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("expected SSH protect, got %v", err)
	}
}
