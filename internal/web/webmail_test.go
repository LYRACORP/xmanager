package web

import (
	"testing"

	"github.com/lyracorp/xmanager/internal/storage"
)

func TestServerWebmailHost(t *testing.T) {
	if got := serverWebmailHost(""); got != "" {
		t.Fatalf("expected empty host, got %q", got)
	}
	if got := serverWebmailHost(" Panel.Example.com. "); got != "webmail.panel.example.com" {
		t.Fatalf("unexpected host: %q", got)
	}
}

func TestDNSZoneForHost(t *testing.T) {
	connected := []storage.ConnectedDomain{
		{Domain: "example.com", DNSProvider: "powerdns"},
		{Domain: "panel.example.com", DNSProvider: "cloudflare", CFZoneID: "zid-panel"},
	}
	ctx := dnsZoneForHost("webmail.panel.example.com", connected)
	if ctx.ZoneDomain != "panel.example.com" {
		t.Fatalf("expected longest matching zone, got %q", ctx.ZoneDomain)
	}
	if ctx.DNSProvider != "cloudflare" {
		t.Fatalf("expected cloudflare provider, got %q", ctx.DNSProvider)
	}
	if ctx.CFZoneID != "zid-panel" {
		t.Fatalf("expected cf zone id, got %q", ctx.CFZoneID)
	}
}

func TestDNSZoneForHostNoMatch(t *testing.T) {
	connected := []storage.ConnectedDomain{{Domain: "example.com"}}
	ctx := dnsZoneForHost("webmail.other.net", connected)
	if ctx.ZoneDomain != "" {
		t.Fatalf("expected empty match, got %q", ctx.ZoneDomain)
	}
}

func TestWebmailHostIdempotentAcrossMainAndMailDomain(t *testing.T) {
	if perDomain, server := webmailHostForDomain("example.com"), serverWebmailHost("example.com"); perDomain != server {
		t.Fatalf("expected same host, got %q and %q", perDomain, server)
	}
}

