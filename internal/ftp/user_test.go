package ftp

import "testing"

func TestValidateUsername(t *testing.T) {
	if err := ValidateUsername("alice"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateUsername("a1_b"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "ab", "Root", "root", "ubuntu", "www-data", "Alice", "a b", "ftp", "../x"} {
		if err := ValidateUsername(bad); err == nil {
			t.Fatalf("expected reject %q", bad)
		}
	}
}

func TestValidateHome(t *testing.T) {
	got, err := ValidateHome("alice", "")
	if err != nil || got != "/srv/ftp/alice" {
		t.Fatalf("got %q %v", got, err)
	}
	got, err = ValidateHome("alice", "/srv/ftp/alice")
	if err != nil || got != "/srv/ftp/alice" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := ValidateHome("alice", "/"); err == nil {
		t.Fatal("expected reject /")
	}
	if _, err := ValidateHome("alice", "/srv/ftp"); err == nil {
		t.Fatal("expected reject home root")
	}
	if _, err := ValidateHome("alice", "/var/www"); err == nil {
		t.Fatal("expected reject outside jail")
	}
	if _, err := ValidateHome("alice", "/srv/ftp/../root"); err == nil {
		t.Fatal("expected reject ..")
	}
}

func TestUserlistRewrite(t *testing.T) {
	raw := "alice\n# comment\nbob\nalice\n\n"
	names := parseUserlist(raw)
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Fatalf("parse: %v", names)
	}
	off := setUserlistEnabled(names, "alice", false)
	if len(off) != 1 || off[0] != "bob" {
		t.Fatalf("disable: %v", off)
	}
	on := setUserlistEnabled(off, "carol", true)
	if got := formatUserlist(on); got != "bob\ncarol\n" {
		t.Fatalf("format %q", got)
	}
}
