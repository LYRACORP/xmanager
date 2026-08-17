package webpanel

import (
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/ssh"
)

func TestShellQuote(t *testing.T) {
	if got := shellQuote("abc"); got != "'abc'" {
		t.Fatalf("got %q", got)
	}
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("got %q", got)
	}
}

func TestPrivRootNoSudo(t *testing.T) {
	w := &WebPanel{sshCfg: ssh.ClientConfig{User: "root"}}
	cmd := "mkdir -p /opt/xmanager"
	if got := w.priv(cmd); got != cmd {
		t.Fatalf("root should not wrap: %q", got)
	}
}

func TestPrivUbuntuWithPassword(t *testing.T) {
	w := &WebPanel{sshCfg: ssh.ClientConfig{User: "ubuntu", Password: "s3cret"}}
	got := w.priv("mkdir -p /opt/xmanager")
	if !strings.Contains(got, "sudo -S") {
		t.Fatalf("expected sudo -S, got %q", got)
	}
	if !strings.Contains(got, "s3cret") {
		t.Fatalf("expected password in pipe, got %q", got)
	}
	if !strings.Contains(got, "bash -c") {
		t.Fatalf("expected bash -c wrap, got %q", got)
	}
}

func TestPrivUbuntuNoPasswordUsesSudoN(t *testing.T) {
	w := &WebPanel{sshCfg: ssh.ClientConfig{User: "ubuntu"}}
	got := w.priv("true")
	if !strings.HasPrefix(got, "sudo -n bash -c ") {
		t.Fatalf("expected sudo -n, got %q", got)
	}
}
