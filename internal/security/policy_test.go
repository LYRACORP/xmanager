package security

import (
	"net"
	"testing"
)

func TestIsAllowed(t *testing.T) {
	p := Policy{
		BlockIPs: []string{"203.0.113.5", "10.0.0.0/8"},
		AllowIPs: []string{"10.0.0.1"},
	}
	if !IsAllowed("10.0.0.1", p) {
		t.Fatal("allow should win")
	}
	if IsAllowed("10.0.0.2", p) {
		t.Fatal("blocked cidr")
	}
	if IsAllowed("203.0.113.5", p) {
		t.Fatal("blocked ip")
	}
	if !IsAllowed("198.51.100.1", p) {
		t.Fatal("should allow unknown")
	}
}

func TestIPMatch(t *testing.T) {
	if !ipMatch("192.168.1.5", "192.168.1.5") {
		t.Fatal("exact")
	}
	_, n, _ := net.ParseCIDR("192.168.0.0/16")
	_ = n
	if !ipMatch("192.168.99.1", "192.168.0.0/16") {
		t.Fatal("cidr")
	}
}
