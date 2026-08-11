package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestRemoveKnownHostsLines(t *testing.T) {
	dir := t.TempDir()
	kh := filepath.Join(dir, "known_hosts")
	content := "host-a ssh-ed25519 AAAAoldA\n" +
		"host-b ssh-ed25519 AAAAoldB\n" +
		"host-c ssh-ed25519 AAAAoldC\n"
	if err := os.WriteFile(kh, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeKnownHostsLines(kh, map[int]struct{}{2: {}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(kh)
	if err != nil {
		t.Fatal(err)
	}
	want := "host-a ssh-ed25519 AAAAoldA\nhost-c ssh-ed25519 AAAAoldC\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReplaceKnownHostEntries(t *testing.T) {
	dir := t.TempDir()
	kh := filepath.Join(dir, "known_hosts")

	oldKey := mustTestPublicKey(t, 1024)
	newKey := mustTestPublicKey(t, 1024)
	host := "10.0.0.9"
	addr := knownhosts.Normalize(net.JoinHostPort(host, "22"))
	oldLine := knownhosts.Line([]string{addr}, oldKey)

	if err := os.WriteFile(kh, []byte(oldLine+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	base, err := knownhosts.New(kh)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP(host), Port: 22}
	err = base(net.JoinHostPort(host, "22"), remote, newKey)
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) || len(keyErr.Want) == 0 {
		t.Fatalf("expected key mismatch, got %v", err)
	}

	if err := replaceKnownHostEntries(kh, keyErr.Want, net.JoinHostPort(host, "22"), remote, newKey); err != nil {
		t.Fatal(err)
	}

	base2, err := knownhosts.New(kh)
	if err != nil {
		t.Fatal(err)
	}
	if err := base2(net.JoinHostPort(host, "22"), remote, newKey); err != nil {
		t.Fatalf("expected new key accepted after replace: %v", err)
	}

	data, _ := os.ReadFile(kh)
	if strings.Contains(string(data), oldLine) {
		t.Fatalf("old line still present: %s", data)
	}
}

func TestHostKeyMismatchErrorIs(t *testing.T) {
	err := fmt.Errorf("connecting: %w", &HostKeyMismatchError{
		Host:           "1.2.3.4:22",
		KnownHostsPath: "/tmp/known_hosts",
	})
	if !IsHostKeyMismatch(err) {
		t.Fatal("expected IsHostKeyMismatch")
	}
	got, ok := AsHostKeyMismatch(err)
	if !ok || got.Host != "1.2.3.4:22" {
		t.Fatalf("AsHostKeyMismatch got %#v ok=%v", got, ok)
	}
}

func mustTestPublicKey(t *testing.T, bits int) gossh.PublicKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
