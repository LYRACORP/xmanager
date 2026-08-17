package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssl"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) loadConnectedDomain(domain string) (*storage.ConnectedDomain, error) {
	domain = normalizeDomainName(domain)
	var cd storage.ConnectedDomain
	if err := h.opts.DB.Where("server_id = ? AND domain = ?", h.localServerID(), domain).First(&cd).Error; err != nil {
		return nil, err
	}
	return &cd, nil
}

func (h *handler) redirectDomainSSL(w http.ResponseWriter, r *http.Request, domain, flash string) {
	http.Redirect(w, r, "/domains/d/"+url.PathEscape(domain)+"?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeDomainSSL(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := normalizeDomainName(r.PathValue("domain"))
	cd, err := h.loadConnectedDomain(domain)
	if err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("domain not found"), http.StatusSeeOther)
		return
	}
	enabled := parseFormSSLEnabled(r.FormValue("ssl_enabled"), r.FormValue("ssl_submitted") == "1")
	cd.SSLEnabled = *enabled
	if p := strings.ToLower(strings.TrimSpace(r.FormValue("ssl_provider"))); p != "" {
		if p == "auto" {
			p = ""
		}
		cd.SSLProvider = p
	}
	if !cd.SSLEnabled {
		cd.SSLStatus = ssl.StatusDisabled
	} else {
		cd.SSLStatus = ssl.StatusPending
	}
	h.persistSSL(cd)
	flash := "SSL settings saved"
	if err := h.EnsureDomainSSL(cd); err != nil {
		flash = "SSL: " + err.Error()
	} else if cd.SSLEnabled {
		flash = "SSL " + cd.SSLStatus + " (" + ssl.ResolveProvider(cd.DNSProvider, cd.SSLProvider) + ")"
	} else {
		flash = "SSL disabled for " + domain
	}
	h.redirectDomainSSL(w, r, domain, flash)
}

func (h *handler) postNodeDomainSSLIssue(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := normalizeDomainName(r.PathValue("domain"))
	cd, err := h.loadConnectedDomain(domain)
	if err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("domain not found"), http.StatusSeeOther)
		return
	}
	cd.SSLEnabled = true
	if p := strings.ToLower(strings.TrimSpace(r.FormValue("ssl_provider"))); p != "" && p != "auto" {
		cd.SSLProvider = p
	}
	h.persistSSL(cd)
	flash := "SSL issued"
	if err := h.EnsureDomainSSL(cd); err != nil {
		flash = "SSL issue: " + err.Error()
	} else {
		flash = "SSL " + cd.SSLStatus + " via " + ssl.ResolveProvider(cd.DNSProvider, cd.SSLProvider)
	}
	h.redirectDomainSSL(w, r, domain, flash)
}

func (h *handler) postNodeDomainSSLCustom(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := normalizeDomainName(r.PathValue("domain"))
	cd, err := h.loadConnectedDomain(domain)
	if err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("domain not found"), http.StatusSeeOther)
		return
	}
	cert := strings.TrimSpace(r.FormValue("cert_pem"))
	key := strings.TrimSpace(r.FormValue("key_pem"))
	if !ssl.ValidPEM(cert, key) {
		h.redirectDomainSSL(w, r, domain, "custom SSL: paste a PEM certificate and private key")
		return
	}
	cd.SSLEnabled = true
	cd.SSLProvider = ssl.ProviderCustom
	h.persistSSL(cd)
	req := sslRequestForDomain(*cd, h.cfToken())
	req.CustomCert = cert
	req.CustomKey = key
	res := ssl.Issue(h.localExec(), req)
	applySSLResult(cd, res)
	h.persistSSL(cd)
	flash := "Custom certificate installed"
	if res.Error != "" {
		flash = "custom SSL: " + res.Error
	}
	h.redirectDomainSSL(w, r, domain, flash)
}

func (h *handler) postNodeDomainSSLDisable(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := normalizeDomainName(r.PathValue("domain"))
	cd, err := h.loadConnectedDomain(domain)
	if err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("domain not found"), http.StatusSeeOther)
		return
	}
	cd.SSLEnabled = false
	h.persistSSL(cd)
	flash := "SSL disabled for " + domain
	if err := h.EnsureDomainSSL(cd); err != nil {
		flash = "SSL disable: " + err.Error()
	}
	h.redirectDomainSSL(w, r, domain, flash)
}
