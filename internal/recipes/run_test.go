package recipes

import (
	"strings"
	"testing"
)

func TestUsesApt(t *testing.T) {
	cases := map[string]bool{
		"apt-get update -y": true,
		"DEBIAN_FRONTEND=noninteractive apt-get install -y curl": true,
		"for pkg in x; do apt-get remove -y $pkg; done":          true,
		"docker --version":                      false,
		"curl -fsSL https://example.com | bash": false,
		"apt install -y nodejs":                 true,
	}
	for cmd, want := range cases {
		if got := usesApt(cmd); got != want {
			t.Fatalf("%q: got %v want %v", cmd, got, want)
		}
	}
}

func TestRemoteShellSudo(t *testing.T) {
	r := Runner{User: "ubuntu", Password: "secret"}
	got := r.remoteShell("apt-get update -y")
	if !strings.Contains(got, "sudo -S") || !strings.Contains(got, "secret") {
		t.Fatalf("expected password sudo, got %q", got)
	}
	if !strings.Contains(got, "bash -lc") {
		t.Fatalf("expected bash -lc, got %q", got)
	}
	rRoot := Runner{User: "root"}
	if got := rRoot.remoteShell("true"); strings.Contains(got, "sudo") {
		t.Fatalf("root should not use sudo: %q", got)
	}
}

func TestIsAptLockError(t *testing.T) {
	msg := `E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 13597 (unattended-upgr)
E: Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend), is another process using it?`
	if !isAptLockError(msg) {
		t.Fatal("expected lock detection")
	}
	if isAptLockError("E: Unable to locate package foo") {
		t.Fatal("false positive")
	}
}
