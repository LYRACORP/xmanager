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

func (h *handler) getNodeEmail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	cfg := mailinbox.LoadConfig(h.opts.DB, sid)
	client := mailinbox.NewClient(cfg)

	data := h.basePage(sess, "Email")
	data.ActiveNav = "email"
	data.MailAPIMode = cfg.Mode
	data.MailAPIConfig = mailConfigForPage(cfg)
	data.WebmailURL = cfg.ResolvedWebmailURL()
	data.MailAdminURL = cfg.AdminURL()
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	miabOK := cfg.Mode == mailinbox.ModeMiaB && client.Ping() == nil
	data.MailAPIReady = miabOK

	if miabOK {
		users, err := client.ListUsers()
		if err != nil {
			if data.Flash == "" {
				data.Flash = "List users failed: " + err.Error()
			}
		} else {
			data.MailDomains = users
			h.syncMailboxesFromAPI(sid, users)
		}
		domains, err := client.ListDomains()
		if err == nil {
			data.MailDomainList = domains
		} else if len(data.MailDomains) > 0 {
			for _, d := range data.MailDomains {
				data.MailDomainList = append(data.MailDomainList, d.Domain)
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
	if cfg.Mode != mailinbox.ModeMiaB {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("Email requires Mail-in-a-Box mode — save connection below"), http.StatusSeeOther)
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

	mailReady := mailinbox.NewClient(cfg).Ping() == nil
	var cd storage.ConnectedDomain
	err := h.opts.DB.Where("server_id = ? AND domain = ?", sid, domain).First(&cd).Error
	if err == gorm.ErrRecordNotFound {
		cd = storage.ConnectedDomain{
			ServerID:  sid,
			Domain:    domain,
			PublicIP:  publicIP,
			DNSReady:  dnsReady,
			MailReady: mailReady,
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
	flash += " · add an account to create it on Mail-in-a-Box"
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

	sid := h.localServerID()
	cfg := mailinbox.LoadConfig(h.opts.DB, sid)
	if cfg.Mode != mailinbox.ModeMiaB {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("Email requires Mail-in-a-Box mode"), http.StatusSeeOther)
		return
	}
	client := mailinbox.NewClient(cfg)
	if err := client.CreateMailbox(local, domain, password); err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("create account failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	addr := strings.ToLower(local + "@" + domain)
	var mb storage.Mailbox
	err := h.opts.DB.Where("server_id = ? AND address = ?", sid, addr).First(&mb).Error
	if err == gorm.ErrRecordNotFound {
		_ = h.opts.DB.Create(&storage.Mailbox{ServerID: sid, Domain: domain, Address: addr}).Error
	}
	_ = h.opts.DB.Model(&storage.ConnectedDomain{}).
		Where("server_id = ? AND domain = ?", sid, domain).
		Update("mail_ready", true)
	http.Redirect(w, r, "/email?flash="+urlQueryEscape("Account "+addr+" created"), http.StatusSeeOther)
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

func (h *handler) postNodeEmailConfig(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid := h.localServerID()
	existing := mailinbox.LoadConfig(h.opts.DB, sid)

	apiBase := strings.TrimSpace(r.FormValue("api_base"))
	adminUser := strings.TrimSpace(r.FormValue("admin_user"))
	adminPass := r.FormValue("admin_password")
	hostname := strings.TrimSpace(r.FormValue("hostname"))
	webmail := strings.TrimSpace(r.FormValue("webmail_url"))

	cfgMap := map[string]string{
		"mode": mailinbox.ModeMiaB,
	}
	if apiBase != "" {
		cfgMap["api_base"] = apiBase
	} else if existing.APIBase != "" {
		cfgMap["api_base"] = existing.APIBase
	} else {
		cfgMap["api_base"] = "https://127.0.0.1/admin"
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

	svc := mailinbox.New(h.opts.DB, sid)
	if err := svc.Enable(h.localExec(), cfgMap); err != nil {
		http.Redirect(w, r, "/email?flash="+urlQueryEscape("save failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	flash := "Mail-in-a-Box connection saved"
	if err := mailinbox.NewClient(mailinbox.LoadConfig(h.opts.DB, sid)).Ping(); err != nil {
		flash = fmt.Sprintf("Saved, but API not reachable: %v", err)
	}
	http.Redirect(w, r, "/email?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}
