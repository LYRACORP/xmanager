package webpanel

import "testing"

func TestFormatPanelLoginURL(t *testing.T) {
	got := formatPanelLoginURL("192.0.2.8", "8080", "qekyfk7518312j1")
	want := "http://192.0.2.8:8080/qekyfk7518312j1/login"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if formatPanelLoginURL("", "", "") != "http://host:8080" {
		t.Fatal("empty fallback")
	}
}

func TestParseYAMLAccessKey(t *testing.T) {
	raw := "web:\n  enabled: true\n  access_key: \"qekyfk7518312j1\"\n  port: 8080\n"
	if got := parseYAMLAccessKey(raw); got != "qekyfk7518312j1" {
		t.Fatalf("got %q", got)
	}
	if parseYAMLAccessKey("access_key: login") != "" {
		t.Fatal("reserved key should be rejected")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("a\nb"); got != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestLoginURLUsesCachedKey(t *testing.T) {
	w := New(nil, 1)
	w.SetHost("192.0.2.8")
	w.accessKey = "qekyfk7518312j1"
	got := w.LoginURL("8080")
	want := "http://192.0.2.8:8080/qekyfk7518312j1/login"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
