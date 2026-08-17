package ssl

import "strings"

const (
	ProviderAuto        = ""
	ProviderLetsEncrypt = "letsencrypt"
	ProviderCloudflare  = "cloudflare"
	ProviderCustom      = "custom"
	ProviderCaddy       = "caddy"

	StatusPending  = "pending"
	StatusOK       = "ok"
	StatusError    = "error"
	StatusDisabled = "disabled"
)

// ResolveProvider picks an issuer. Explicit non-auto values win; Cloudflare DNS defaults to origin certs.
func ResolveProvider(dnsProvider, explicit string) string {
	p := strings.ToLower(strings.TrimSpace(explicit))
	if p == "auto" {
		p = ""
	}
	switch p {
	case ProviderLetsEncrypt, ProviderCloudflare, ProviderCustom, ProviderCaddy:
		return p
	}
	if strings.EqualFold(strings.TrimSpace(dnsProvider), "cloudflare") {
		return ProviderCloudflare
	}
	return ProviderLetsEncrypt
}

// OriginHostnames returns apex + wildcard names for a Cloudflare Origin CA request.
func OriginHostnames(zone string) []string {
	zone = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(zone), "."))
	if zone == "" {
		return nil
	}
	return []string{zone, "*." + zone}
}

// LetsEncryptPaths is the live certbot directory for a hostname.
func LetsEncryptPaths(host string) (cert, key string) {
	host = strings.ToLower(strings.TrimSpace(host))
	base := "/etc/letsencrypt/live/" + host
	return base + "/fullchain.pem", base + "/privkey.pem"
}

// OriginCertDir is where Cloudflare/custom PEMs are stored for a zone or host.
func OriginCertDir(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return "/etc/nginx/ssl/" + name
}

func OriginCertPaths(name string) (cert, key string) {
	dir := OriginCertDir(name)
	return dir + "/fullchain.pem", dir + "/privkey.pem"
}

// ValidPEM reports whether cert and key look like PEM material.
func ValidPEM(cert, key string) bool {
	cert = strings.TrimSpace(cert)
	key = strings.TrimSpace(key)
	if !strings.Contains(cert, "BEGIN CERTIFICATE") {
		return false
	}
	if !strings.Contains(key, "BEGIN") || !strings.Contains(key, "PRIVATE KEY") {
		return false
	}
	return true
}
