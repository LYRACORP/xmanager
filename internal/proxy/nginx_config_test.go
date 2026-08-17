package proxy

import (
	"strings"
	"testing"
)

func TestHTTPVHostConfig(t *testing.T) {
	got := HTTPVHostConfig("example.com", "http://127.0.0.1:8080", nil)
	if !strings.Contains(got, "listen 80;") || strings.Contains(got, "listen 443") {
		t.Fatalf("expected HTTP-only vhost, got:\n%s", got)
	}
	if !strings.Contains(got, "server_name example.com;") {
		t.Fatal("missing server_name")
	}
	if !strings.Contains(got, "proxy_pass http://127.0.0.1:8080;") {
		t.Fatal("missing proxy_pass")
	}
}

func TestHTTPSVHostConfig(t *testing.T) {
	got := HTTPSVHostConfig("example.com", "http://127.0.0.1:8080",
		"/etc/nginx/ssl/example.com/fullchain.pem",
		"/etc/nginx/ssl/example.com/privkey.pem",
		[]string{"/etc/nginx/snippets/xmanager-ratelimit.conf"})
	if !strings.Contains(got, "listen 443 ssl;") {
		t.Fatalf("missing 443 ssl:\n%s", got)
	}
	if !strings.Contains(got, "return 301 https://$host$request_uri;") {
		t.Fatal("missing HTTP redirect")
	}
	if !strings.Contains(got, "include /etc/nginx/snippets/xmanager-ratelimit.conf;") {
		t.Fatal("missing snippet include")
	}
}

func TestParseVHostConfig(t *testing.T) {
	cfg := HTTPSVHostConfig("a.example", "http://127.0.0.1:9", "/c.pem", "/k.pem",
		[]string{"/etc/nginx/snippets/xmanager-modsec.conf"})
	meta := ParseVHostConfig(cfg)
	if meta.Domain != "a.example" {
		t.Fatalf("domain %q", meta.Domain)
	}
	if meta.Upstream != "http://127.0.0.1:9" {
		t.Fatalf("upstream %q", meta.Upstream)
	}
	if !meta.SSL {
		t.Fatal("expected ssl")
	}
	if len(meta.Includes) != 1 || meta.Includes[0] != "/etc/nginx/snippets/xmanager-modsec.conf" {
		t.Fatalf("includes %#v", meta.Includes)
	}
}
