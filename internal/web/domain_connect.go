package web

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/services/mailinbox"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// domainConnectOpts controls what to wire when adding/updating a domain.
type domainConnectOpts struct {
	Domain      string
	Upstream    string
	ProjectID   uint
	DBType      string
	DBName      string
	PublicIP    string
	DNSProvider string // "powerdns" | "cloudflare" | "" | "none"
	CFZoneID    string
	SkipDNS     bool
	SkipMail    bool
	SkipNginx   bool
}

func detectPublicIP(exec *ssh.Executor) string {
	if exec == nil {
		return ""
	}
	for _, cmd := range []string{
		`curl -4 -fsS --max-time 3 https://api.ipify.org 2>/dev/null`,
		`curl -4 -fsS --max-time 3 https://ifconfig.me 2>/dev/null`,
		`hostname -I 2>/dev/null | awk '{print $1}'`,
	} {
		out := strings.TrimSpace(exec.RunQuiet(cmd))
		if out != "" && !strings.Contains(out, " ") && strings.Contains(out, ".") {
			return out
		}
	}
	return ""
}

func (h *handler) connectDomain(opts domainConnectOpts) (*storage.ConnectedDomain, error) {
	domain := strings.ToLower(strings.TrimSpace(opts.Domain))
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}
	sid := h.localServerID()
	exec := h.localExec()

	if opts.Upstream == "" {
		opts.Upstream = "http://127.0.0.1:8080"
	}
	if opts.PublicIP == "" {
		opts.PublicIP = detectPublicIP(exec)
	}

	var cd storage.ConnectedDomain
	err := h.opts.DB.Where("server_id = ? AND domain = ?", sid, domain).First(&cd).Error
	if err == gorm.ErrRecordNotFound {
		cd = storage.ConnectedDomain{
			ServerID: sid,
			Domain:   domain,
		}
	} else if err != nil {
		return nil, err
	}

	cd.Upstream = opts.Upstream
	cd.PublicIP = opts.PublicIP
	if opts.ProjectID > 0 {
		pid := opts.ProjectID
		cd.ProjectID = &pid
	}
	if opts.DBType != "" {
		cd.DBType = opts.DBType
	}
	if opts.DBName != "" {
		cd.DBName = opts.DBName
	}
	provider := strings.ToLower(strings.TrimSpace(opts.DNSProvider))
	if opts.DNSProvider != "" {
		cd.DNSProvider = provider
	}
	if opts.CFZoneID != "" {
		cd.CFZoneID = strings.TrimSpace(opts.CFZoneID)
	}
	if provider == "none" {
		opts.SkipDNS = true
		cd.DNSProvider = "none"
	}

	var errs []string
	var soft []string

	ns := cloudflare.LoadNodeSettings(h.opts.DB, sid)
	if opts.PublicIP == "" && ns.PublicIP != "" {
		opts.PublicIP = ns.PublicIP
		cd.PublicIP = opts.PublicIP
	}

	// Project link + ProjectDomain row
	if cd.ProjectID != nil && *cd.ProjectID > 0 {
		_ = h.opts.DB.Model(&storage.Project{}).Where("id = ?", *cd.ProjectID).Update("domain", domain)
		var pd storage.ProjectDomain
		if h.opts.DB.Where("project_id = ? AND domain = ?", *cd.ProjectID, domain).First(&pd).Error != nil {
			_ = h.opts.DB.Create(&storage.ProjectDomain{ProjectID: *cd.ProjectID, Domain: domain, SSL: true}).Error
		}
	}

	// Database link (metadata + ProjectDatabase when project set)
	if cd.DBType != "" && cd.DBName != "" && cd.ProjectID != nil && *cd.ProjectID > 0 {
		var pdb storage.ProjectDatabase
		q := h.opts.DB.Where("project_id = ? AND db_type = ? AND db_name = ?", *cd.ProjectID, cd.DBType, cd.DBName)
		if q.First(&pdb).Error != nil {
			_ = h.opts.DB.Create(&storage.ProjectDatabase{
				ProjectID: *cd.ProjectID,
				ServerID:  sid,
				DBType:    cd.DBType,
				DBName:    cd.DBName,
			}).Error
		}
	}

	// Nginx — install if missing, then add vhost (needed for Cloudflare origin / :80)
	if !opts.SkipNginx {
		if m := proxy.NewManager(proxy.Nginx, exec); m != nil {
			if nm, ok := m.(*proxy.NginxManager); ok {
				if err := nm.EnsureInstalled(); err != nil {
					soft = append(soft, "nginx: "+err.Error())
				} else if err := nm.AddVHost(domain, opts.Upstream); err != nil {
					soft = append(soft, "nginx: "+err.Error())
				} else {
					// Best-effort open HTTP for Cloudflare / public access.
					_ = hostfirewall.Allow([]int{80, 443}, "tcp")
				}
			}
		}
	}

	// DNS — PowerDNS, Cloudflare, or skip
	cd.DNSReady = false
	if !opts.SkipDNS {
		switch cd.DNSProvider {
		case "cloudflare":
			token := ns.CFAPIToken
			if token == "" {
				errs = append(errs, "cloudflare: API token not set — add it under Settings → Node Identity")
			} else {
				cfg := cloudflare.NewConfig(token, cd.CFZoneID)
				client := cloudflare.NewClient(cfg)
				if cd.CFZoneID == "" {
					zid, err := client.LookupZoneID(domain)
					if err != nil {
						errs = append(errs, "cloudflare: "+truncateErr(err.Error(), 160))
					} else {
						cd.CFZoneID = zid
						cfg.ZoneID = zid
						client = cloudflare.NewClient(cfg)
					}
				}
				if cd.CFZoneID != "" {
					if err := client.Ping(); err != nil {
						errs = append(errs, "cloudflare: "+truncateErr(err.Error(), 160))
					} else {
						cd.DNSReady = true
					}
				}
			}
		case "powerdns":
			pdnsCfg := powerdns.LoadConfig(h.opts.DB, sid)
			client := powerdns.NewClient(pdnsCfg)
			mailHost := mailinbox.LoadConfig(h.opts.DB, sid).Hostname
			if mailHost == "" || mailHost == "mail.example.com" {
				mailHost = "mail." + domain
			}
			if err := client.Ping(); err != nil {
				errs = append(errs, fmt.Sprintf("powerdns: API offline at %s — disable/enable PowerDNS under Services (API defaults to :%s; Adminer uses :8081): %v",
					pdnsCfg.URL(), powerdns.DefaultAPIPort, truncateErr(err.Error(), 120)))
			} else if err := client.EnsureZone(domain, opts.PublicIP, mailHost, ns.NS1, ns.NS2); err != nil {
				errs = append(errs, "powerdns: "+truncateErr(err.Error(), 160))
			} else {
				cd.DNSReady = true
			}
		case "":
			// Legacy rows with no provider: try PowerDNS once and stamp provider.
			cd.DNSProvider = "powerdns"
			pdnsCfg := powerdns.LoadConfig(h.opts.DB, sid)
			client := powerdns.NewClient(pdnsCfg)
			mailHost := mailinbox.LoadConfig(h.opts.DB, sid).Hostname
			if mailHost == "" || mailHost == "mail.example.com" {
				mailHost = "mail." + domain
			}
			if err := client.Ping(); err != nil {
				errs = append(errs, fmt.Sprintf("powerdns: API offline at %s — disable/enable PowerDNS under Services (API defaults to :%s; Adminer uses :8081): %v",
					pdnsCfg.URL(), powerdns.DefaultAPIPort, truncateErr(err.Error(), 120)))
			} else if err := client.EnsureZone(domain, opts.PublicIP, mailHost, ns.NS1, ns.NS2); err != nil {
				errs = append(errs, "powerdns: "+truncateErr(err.Error(), 160))
			} else {
				cd.DNSReady = true
			}
		}
	}

	// Mail domain — soft-fail when API is down
	cd.MailReady = false
	if !opts.SkipMail {
		mailCfg := mailinbox.LoadConfig(h.opts.DB, sid)
		mailClient := mailinbox.NewClient(mailCfg)
		if err := mailClient.Ping(); err != nil {
			soft = append(soft, fmt.Sprintf("mail: API unreachable at %s — skipped (%v)", mailCfg.BaseURL(), err))
		} else if err := mailClient.EnsureDomain(domain); err != nil {
			soft = append(soft, "mail: "+truncateErr(err.Error(), 160))
		} else {
			cd.MailReady = true
		}
	}

	all := append(append([]string{}, soft...), errs...)
	cd.LastError = strings.Join(all, "; ")
	if cd.ID == 0 {
		if err := h.opts.DB.Create(&cd).Error; err != nil {
			return nil, err
		}
	} else if err := h.opts.DB.Save(&cd).Error; err != nil {
		return nil, err
	}
	// Soft warnings still surface as "saved with warnings"; hard errors too.
	if len(all) > 0 {
		return &cd, fmt.Errorf("%s", cd.LastError)
	}
	return &cd, nil
}

func truncateErr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func normalizeDomainName(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// disconnectDomain removes panel links and optionally nginx vhost / PowerDNS zone.
func (h *handler) disconnectDomain(domain string, deleteDNS, deleteNginx bool) error {
	domain = normalizeDomainName(domain)
	if domain == "" {
		return fmt.Errorf("domain required")
	}
	sid := h.localServerID()
	exec := h.localExec()
	var notes []string

	if deleteNginx {
		if m := proxy.NewManager(proxy.Nginx, exec); m != nil {
			if nm, ok := m.(*proxy.NginxManager); ok && nm.IsAvailable() {
				if err := nm.RemoveVHost(domain); err != nil {
					notes = append(notes, "nginx: "+err.Error())
				}
			}
		}
	}

	if deleteDNS {
		if err := powerdns.NewClient(powerdns.LoadConfig(h.opts.DB, sid)).DeleteZone(domain); err != nil {
			notes = append(notes, "dns: "+err.Error())
		}
	}

	// Local mailbox rows (remote delete best-effort)
	var mboxes []storage.Mailbox
	h.opts.DB.Where("server_id = ? AND domain = ?", sid, domain).Find(&mboxes)
	mailClient := mailinbox.NewClient(mailinbox.LoadConfig(h.opts.DB, sid))
	for _, mb := range mboxes {
		_ = mailClient.DeleteMailbox(mb.Address)
		_ = h.opts.DB.Delete(&mb).Error
	}

	_ = h.opts.DB.Where("domain = ?", domain).Delete(&storage.ProjectDomain{}).Error

	res := h.opts.DB.Where("server_id = ? AND domain = ?", sid, domain).Delete(&storage.ConnectedDomain{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 && len(notes) == 0 {
		return fmt.Errorf("domain %s not found", domain)
	}
	if len(notes) > 0 {
		return fmt.Errorf("removed with warnings: %s", strings.Join(notes, "; "))
	}
	return nil
}

func (h *handler) createMailboxAPI(local, domain, password string, projectID uint) (*storage.Mailbox, error) {
	local = strings.ToLower(strings.TrimSpace(local))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if local == "" || domain == "" {
		return nil, fmt.Errorf("local part and domain required")
	}
	if password == "" {
		return nil, fmt.Errorf("password required")
	}
	sid := h.localServerID()

	// Ensure domain hub exists (mail + dns best-effort).
	_, _ = h.connectDomain(domainConnectOpts{Domain: domain, ProjectID: projectID, SkipNginx: true})

	mailCfg := mailinbox.LoadConfig(h.opts.DB, sid)
	client := mailinbox.NewClient(mailCfg)
	if err := client.CreateMailbox(local, domain, password); err != nil {
		return nil, err
	}
	addr := local + "@" + domain
	mb := storage.Mailbox{
		ServerID: sid,
		Domain:   domain,
		Address:  addr,
	}
	if projectID > 0 {
		mb.ProjectID = &projectID
	}
	if err := h.opts.DB.Create(&mb).Error; err != nil {
		return nil, err
	}
	_ = h.opts.DB.Model(&storage.ConnectedDomain{}).
		Where("server_id = ? AND domain = ?", sid, domain).
		Updates(map[string]any{"mail_ready": true, "last_error": ""})
	return &mb, nil
}

func (h *handler) deleteMailboxAPI(id uint) error {
	var mb storage.Mailbox
	if err := h.opts.DB.First(&mb, id).Error; err != nil {
		return err
	}
	client := mailinbox.NewClient(mailinbox.LoadConfig(h.opts.DB, h.localServerID()))
	_ = client.DeleteMailbox(mb.Address) // best-effort remote delete
	return h.opts.DB.Delete(&mb).Error
}
