package ssl

import (
	"strings"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	if got := ResolveProvider("powerdns", ""); got != ProviderLetsEncrypt {
		t.Fatalf("default %q", got)
	}
	if got := ResolveProvider("cloudflare", ""); got != ProviderCloudflare {
		t.Fatalf("cf auto %q", got)
	}
	if got := ResolveProvider("cloudflare", "letsencrypt"); got != ProviderLetsEncrypt {
		t.Fatalf("explicit le %q", got)
	}
	if got := ResolveProvider("powerdns", "auto"); got != ProviderLetsEncrypt {
		t.Fatalf("auto %q", got)
	}
	if got := ResolveProvider("", "caddy"); got != ProviderCaddy {
		t.Fatalf("caddy %q", got)
	}
}

func TestOriginHostnames(t *testing.T) {
	got := OriginHostnames("Example.com.")
	if len(got) != 2 || got[0] != "example.com" || got[1] != "*.example.com" {
		t.Fatalf("%v", got)
	}
	if OriginHostnames("  ") != nil {
		t.Fatal("expected nil")
	}
}

func TestValidPEM(t *testing.T) {
	if ValidPEM("nope", "nope") {
		t.Fatal("expected false")
	}
	cert := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"
	key := "-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----"
	if !ValidPEM(cert, key) {
		t.Fatal("expected true")
	}
}

func TestGenerateOriginCSR(t *testing.T) {
	hosts := OriginHostnames("example.com")
	csr, err := GenerateOriginCSR(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csr.CSRPEM, "BEGIN CERTIFICATE REQUEST") {
		t.Fatalf("csr: %s", csr.CSRPEM[:40])
	}
	if !strings.Contains(csr.KeyPEM, "PRIVATE KEY") {
		t.Fatal("missing key")
	}
}

func TestOriginCAPayload(t *testing.T) {
	hosts := OriginHostnames("example.com")
	p := OriginCAPayload("-----BEGIN CERTIFICATE REQUEST-----\nX\n-----END CERTIFICATE REQUEST-----", hosts)
	if p["request_type"] != "origin-rsa" {
		t.Fatalf("%v", p["request_type"])
	}
	if p["requested_validity"] != 5475 {
		t.Fatalf("%v", p["requested_validity"])
	}
	got, _ := p["hostnames"].([]string)
	if strings.Join(got, ",") != "example.com,*.example.com" {
		t.Fatalf("%v", got)
	}
}

func TestIssueDisabled(t *testing.T) {
	res := Issue(nil, Request{Host: "example.com", Enabled: false})
	if res.Status != StatusDisabled {
		t.Fatalf("status %q", res.Status)
	}
}

func TestIssueMissingHost(t *testing.T) {
	res := Issue(nil, Request{Enabled: true})
	if res.Status != StatusError {
		t.Fatalf("status %q", res.Status)
	}
}
