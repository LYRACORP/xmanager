package web

import (
	"strings"

	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/ssl"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) cfToken() string {
	return cloudflare.LoadToken(h.opts.DB, h.localServerID())
}

func sslRequestForDomain(cd storage.ConnectedDomain, token string) ssl.Request {
	upstream := strings.TrimSpace(cd.Upstream)
	if upstream == "" {
		upstream = "http://127.0.0.1:8080"
	}
	return ssl.Request{
		Host:        cd.Domain,
		Upstream:    upstream,
		Enabled:     cd.SSLEnabled,
		Provider:    cd.SSLProvider,
		DNSProvider: cd.DNSProvider,
		CFZoneID:    cd.CFZoneID,
		CFToken:     token,
		CertZone:    cd.Domain,
	}
}

func applySSLResult(cd *storage.ConnectedDomain, res ssl.Result) {
	if cd == nil {
		return
	}
	if res.Provider != "" {
		cd.SSLProvider = res.Provider
	}
	cd.SSLStatus = res.Status
	cd.SSLError = res.Error
	if res.Status == ssl.StatusDisabled {
		cd.SSLEnabled = false
	}
}

func (h *handler) persistSSL(cd *storage.ConnectedDomain) {
	if cd == nil || cd.ID == 0 || h.opts.DB == nil {
		return
	}
	_ = h.opts.DB.Model(cd).Updates(map[string]any{
		"ssl_enabled":  cd.SSLEnabled,
		"ssl_provider": cd.SSLProvider,
		"ssl_status":   cd.SSLStatus,
		"ssl_error":    cd.SSLError,
	}).Error
}

// EnsureDomainSSL provisions or disables TLS for a connected domain (soft-fail).
func (h *handler) EnsureDomainSSL(cd *storage.ConnectedDomain) error {
	if cd == nil || strings.TrimSpace(cd.Domain) == "" {
		return nil
	}
	res := ssl.Issue(h.localExec(), sslRequestForDomain(*cd, h.cfToken()))
	applySSLResult(cd, res)
	h.persistSSL(cd)
	return res.Err()
}

// EnsureHostSSL provisions TLS for an extra hostname (webmail.*) using the parent domain's policy.
func (h *handler) EnsureHostSSL(host, upstream string, parent *storage.ConnectedDomain) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil
	}
	req := ssl.Request{
		Host:     host,
		Upstream: upstream,
		Enabled:  true,
		Provider: ssl.ProviderLetsEncrypt,
		CFToken:  h.cfToken(),
		CertZone: host,
	}
	if parent != nil {
		req.Enabled = parent.SSLEnabled
		req.Provider = parent.SSLProvider
		req.DNSProvider = parent.DNSProvider
		req.CFZoneID = parent.CFZoneID
		if z := strings.TrimSpace(parent.Domain); z != "" {
			req.CertZone = z
		}
	}
	if strings.TrimSpace(req.Upstream) == "" {
		req.Upstream = "http://127.0.0.1:8080"
	}
	res := ssl.Issue(h.localExec(), req)
	return res.Err()
}

func parseFormSSLEnabled(v string, submitted bool) *bool {
	v = strings.ToLower(strings.TrimSpace(v))
	on := true
	if submitted {
		on = v == "1" || v == "on" || v == "true" || v == "yes"
	} else if v != "" {
		on = v == "1" || v == "on" || v == "true" || v == "yes"
	}
	return &on
}
