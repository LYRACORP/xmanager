package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func (h *handler) mailClient() *mailinbox.Client {
	return mailinbox.NewClient(mailinbox.LoadConfig(h.opts.DB, h.localServerID()))
}

func mailConfigForPage(cfg mailinbox.Config) mailinbox.Config {
	cfg.AdminPassword = ""
	return cfg
}

func mailAPIReady(cfg mailinbox.Config, client *mailinbox.Client) bool {
	return client.Ping() == nil
}

func (h *handler) getNodeEmail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	cfg := mailinbox.LoadConfig(h.opts.DB, sid)
	client := mailinbox.NewClient(cfg)

	var connected []storage.ConnectedDomain
	h.scopeServerQuery(r, &storage.ConnectedDomain{}).Order("domain asc").Find(&connected)

	var mailboxes []storage.Mailbox
	h.scopeServerQuery(r, &storage.Mailbox{}).Order("address asc").Find(&mailboxes)

	data := h.basePage(sess, "Email")
	data.ActiveNav = "email"
	data.MailAPIMode = cfg.Mode
	data.MailAPIConfig = mailConfigForPage(cfg)
	data.WebmailURL = cfg.ResolvedWebmailURL()
	data.MailAdminURL = cfg.AdminURL()
	data.ConnectedDomains = connected
	data.Mailboxes = mailboxes
	data.MailAPIReady = mailAPIReady(cfg, client)
	ns := cloudflare.LoadNodeSettings(h.opts.DB, sid)
	data.NodeSettings = &ns
	data.ServerWebmailHost = serverWebmailHost(ns.MainDomain)
	data.ServerWebmailURL = webmailURLForHost(data.ServerWebmailHost)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	for _, cd := range connected {
		data.MailDomainList = append(data.MailDomainList, cd.Domain)
		data.WebmailHosts = append(data.WebmailHosts, webmailHostView{
			Domain: cd.Domain,
			Host:   "webmail." + cd.Domain,
			URL:    webmailURLForDomain(cd.Domain),
		})
	}

	if cfg.Mode == mailinbox.ModeMiaB && data.MailAPIReady {
		users, err := client.ListUsers()
		if err != nil {
			if data.Flash == "" {
				data.Flash = "List users failed: " + err.Error()
			}
		} else {
			data.MailDomains = users
			h.syncMailboxesFromAPI(sid, users)
			h.opts.DB.Where("server_id = ?", sid).Order("address asc").Find(&mailboxes)
			data.Mailboxes = mailboxes
		}
		domains, err := client.ListDomains()
		if err == nil {
			seen := map[string]bool{}
			for _, d := range data.MailDomainList {
				seen[d] = true
			}
			for _, d := range domains {
				if d != "" && !seen[d] {
					seen[d] = true
					data.MailDomainList = append(data.MailDomainList, d)
				}
			}
		}
	}

	h.render(w, "node_email", data)
}

func (h *handler) syncMailboxesFromAPI(serverID uint, domains []mailinbox.MailDomain) {
	for _, d := range domains {
		for _, u := range d.Users {
			addr := strings.ToLower(strings.TrimSpace(u.Email))
			if addr == "" {
				continue
			}
			domain := d.Domain
			if i := strings.Index(addr, "@"); i >= 0 {
				domain = addr[i+1:]
			}
			var mb storage.Mailbox
			err := h.opts.DB.Where("server_id = ? AND address = ?", serverID, addr).First(&mb).Error
			if err == gorm.ErrRecordNotFound {
				_ = h.opts.DB.Create(&storage.Mailbox{
					ServerID: serverID,
					Domain:   domain,
					Address:  addr,
				}).Error
			}
		}
	}
}

func (h *handler) postNodeEmailDomain(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	publicIP := strings.TrimSpace(r.FormValue("public_ip"))
	if domain == "" {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("domain required"), http.StatusSeeOther)
		return
	}

	sid := h.localServerID()
	cfg := mailinbox.LoadConfig(h.opts.DB, sid)
	client := mailinbox.NewClient(cfg)
	if err := client.Ping(); err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("mail API offline — enable mailinbox under Apps"), http.StatusSeeOther)
		return
	}

	mailMX := strings.TrimSpace(cfg.Hostname)
	if mailMX == "" || mailMX == "mail.example.com" {
		mailMX = "mail." + domain
	}
	if publicIP == "" {
		publicIP = detectPublicIP(h.localExec())
	}

	pdns := powerdns.NewClient(powerdns.LoadConfig(h.opts.DB, sid))
	ns := cloudflare.LoadNodeSettings(h.opts.DB, sid)
	dnsReady := false
	dnsErr := ""
	if err := pdns.Ping(); err == nil {
		if err := pdns.EnsureZone(domain, publicIP, mailMX, ns.NS1, ns.NS2); err != nil {
			dnsErr = err.Error()
		} else {
			dnsReady = true
		}
	}

	if err := client.EnsureDomain(domain); err != nil && cfg.Mode != mailinbox.ModeMiaB {
		// soft — MiaB creates on first mailbox
		_ = err
	}

	mailReady := client.Ping() == nil
	var cd storage.ConnectedDomain
	err := h.opts.DB.Where("server_id = ? AND domain = ?", sid, domain).First(&cd).Error
	if err == gorm.ErrRecordNotFound {
		cd = storage.ConnectedDomain{
			ServerID:    sid,
			Domain:      domain,
			PublicIP:    publicIP,
			DNSReady:    dnsReady,
			MailReady:   mailReady,
			DNSProvider: "powerdns",
		}
		_ = h.opts.DB.Create(&cd).Error
	} else if err == nil {
		if publicIP != "" {
			cd.PublicIP = publicIP
		}
		cd.DNSReady = cd.DNSReady || dnsReady
		cd.MailReady = mailReady
		_ = h.opts.DB.Save(&cd).Error
	}

	flash := "Domain " + domain + " registered"
	if dnsReady {
		flash += " · DNS MX ready"
	} else if dnsErr != "" {
		flash += " · DNS failed: " + dnsErr
	}
	if wmErr := h.EnsureWebmail(domain); wmErr != nil {
		flash += " · webmail: " + wmErr.Error()
	} else {
		flash += " · webmail." + domain + " ready"
	}
	if swErr := h.EnsureServerWebmail(); swErr != nil {
		flash += " · server webmail: " + swErr.Error()
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeEmailAccount(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	local := strings.TrimSpace(r.FormValue("local_part"))
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	password := r.FormValue("password")
	if local == "" || domain == "" || password == "" {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("local part, domain, and password required"), http.StatusSeeOther)
		return
	}

	mb, err := h.createMailboxAPI(r, local, domain, password, 0)
	if err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("create account failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	flash := "Account " + mb.Address + " created · webmail at " + webmailURLForDomain(domain)
	if host := serverWebmailHost(cloudflare.LoadNodeSettings(h.opts.DB, h.localServerID()).MainDomain); host != "" {
		flash += " · shared webmail at " + webmailURLForHost(host)
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeEmailAccountDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if email == "" {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("email required"), http.StatusSeeOther)
		return
	}
	sid := h.localServerID()
	client := h.mailClient()
	if err := client.DeleteMailbox(email); err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("delete failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	h.opts.DB.Where("server_id = ? AND address = ?", sid, email).Delete(&storage.Mailbox{})
	http.Redirect(w, r, "/email?flash="+urlQueryEscape("Deleted "+email), http.StatusSeeOther)
}

func (h *handler) postNodeEmailEnsureWebmail(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	domain := strings.ToLower(strings.TrimSpace(r.FormValue("domain")))
	if domain == "" {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("domain required"), http.StatusSeeOther)
		return
	}
	flash := "Webmail ensured for " + domain + " → " + webmailURLForDomain(domain)
	if err := h.EnsureWebmail(domain); err != nil {
		flash = "Webmail for " + domain + ": " + err.Error()
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeEmailEnsureServerWebmail(w http.ResponseWriter, r *http.Request) {
	host := serverWebmailHost(cloudflare.LoadNodeSettings(h.opts.DB, h.localServerID()).MainDomain)
	if host == "" {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("set Main domain in Settings first"), http.StatusSeeOther)
		return
	}
	flash := "Server webmail ensured → " + webmailURLForHost(host)
	if err := h.EnsureServerWebmail(); err != nil {
		flash = "Server webmail: " + err.Error()
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeEmailConfig(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid := h.localServerID()
	existing := mailinbox.LoadConfig(h.opts.DB, sid)

	apiBase := strings.TrimSpace(r.FormValue("api_base"))
	adminUser := strings.TrimSpace(r.FormValue("admin_user"))
	adminPass := r.FormValue("admin_password")
	hostname := strings.TrimSpace(r.FormValue("hostname"))
	webmail := strings.TrimSpace(r.FormValue("webmail_url"))
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode != mailinbox.ModeMiaB && mode != mailinbox.ModeStalwart {
		mode = existing.Mode
		if mode == "" {
			mode = mailinbox.ModeStalwart
		}
	}

	cfgMap := map[string]string{
		"mode": mode,
	}
	if apiBase != "" {
		cfgMap["api_base"] = apiBase
	} else if existing.APIBase != "" {
		cfgMap["api_base"] = existing.APIBase
	}
	if adminUser != "" {
		cfgMap["admin_user"] = adminUser
	} else if existing.AdminUser != "" {
		cfgMap["admin_user"] = existing.AdminUser
	}
	if adminPass != "" {
		cfgMap["admin_password"] = adminPass
	} else if existing.AdminPassword != "" {
		cfgMap["admin_password"] = existing.AdminPassword
	}
	if hostname != "" {
		cfgMap["hostname"] = hostname
	} else if existing.Hostname != "" {
		cfgMap["hostname"] = existing.Hostname
	}
	if webmail != "" {
		cfgMap["webmail_url"] = webmail
	} else if existing.WebmailURL != "" {
		cfgMap["webmail_url"] = existing.WebmailURL
	}
	if existing.HTTPSPort != "" {
		cfgMap["https_port"] = existing.HTTPSPort
	}

	svc := mailinbox.New(h.opts.DB, sid)
	if err := svc.Enable(h.localExec(), cfgMap); err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("save failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	flash := "Mail connection saved (" + mode + ")"
	if err := mailinbox.NewClient(mailinbox.LoadConfig(h.opts.DB, sid)).Ping(); err != nil {
		flash = fmt.Sprintf("Saved, but API not reachable: %v", err)
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}
