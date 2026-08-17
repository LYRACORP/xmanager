package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/services/cloudflare"
	"github.com/lyracorp/xmanager/internal/services/powerdns"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func (h *handler) pdnsClient() *powerdns.Client {
	return powerdns.NewClient(powerdns.LoadConfig(h.opts.DB, h.localServerID()))
}

func (h *handler) getNodeDNS(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	client := h.pdnsClient()
	pdnsOK := client.Ping() == nil

	data := h.basePage(sess, "DNS")
	data.ActiveNav = "dns"
	data.PowerDNSReady = pdnsOK
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	cfg := powerdns.LoadConfig(h.opts.DB, sid)
	if !pdnsOK && data.Flash == "" {
		data.Flash = fmt.Sprintf("PowerDNS API offline at %s — open Services, Disable then Enable PowerDNS (publishes API on :%s; Adminer keeps :8081)",
			cfg.URL(), powerdns.DefaultAPIPort)
	}


	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", sid).Order("name asc").Find(&projects)
	data.Projects = projects

	var connected []storage.ConnectedDomain
	h.opts.DB.Where("server_id = ?", sid).Order("domain asc").Find(&connected)
	data.ConnectedDomains = connected

	if pdnsOK {
		zones, err := client.ListZones()
		if err != nil {
			if data.Flash == "" {
				data.Flash = "List zones failed: " + err.Error()
			}
		} else {
			data.DNSZones = zones
		}
	}

	h.render(w, "node_dns", data)
}

func (h *handler) postNodeDNSZone(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	zone := strings.TrimSpace(strings.ToLower(r.FormValue("zone")))
	publicIP := strings.TrimSpace(r.FormValue("public_ip"))
	if zone == "" {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("zone name required"), http.StatusSeeOther)
		return
	}
	if publicIP == "" {
		publicIP = detectPublicIP(h.localExec())
	}
	ns := cloudflare.LoadNodeSettings(h.opts.DB, h.localServerID())
	if publicIP == "" && ns.PublicIP != "" {
		publicIP = ns.PublicIP
	}
	client := h.pdnsClient()
	if err := client.Ping(); err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("PowerDNS offline — enable it under Apps first: "+err.Error()), http.StatusSeeOther)
		return
	}
	if err := client.EnsureZone(zone, publicIP, "", ns.NS1, ns.NS2); err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("create zone failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	// Ensure ConnectedDomain hub row exists for linking.
	sid := h.localServerID()
	var cd storage.ConnectedDomain
	err := h.opts.DB.Where("server_id = ? AND domain = ?", sid, zone).First(&cd).Error
	if err == gorm.ErrRecordNotFound {
		cd = storage.ConnectedDomain{ServerID: sid, Domain: zone, PublicIP: publicIP, DNSReady: true, DNSProvider: "powerdns"}
		_ = h.opts.DB.Create(&cd).Error
	} else if err == nil {
		cd.PublicIP = publicIP
		cd.DNSReady = true
		if cd.DNSProvider == "" {
			cd.DNSProvider = "powerdns"
		}
		_ = h.opts.DB.Save(&cd).Error
	}
	http.Redirect(w, r, "/domains/d/"+url.PathEscape(zone)+"?flash="+urlQueryEscape("Zone "+zone+" created"), http.StatusSeeOther)
}

func (h *handler) postNodeDNSZoneDelete(w http.ResponseWriter, r *http.Request) {
	zone := strings.TrimSpace(r.PathValue("zone"))
	if zone == "" {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("zone required"), http.StatusSeeOther)
		return
	}
	if err := h.pdnsClient().DeleteZone(zone); err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape("delete failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/domains?flash="+urlQueryEscape("Zone "+normalizeDomainName(zone)+" deleted"), http.StatusSeeOther)
}

func (h *handler) getNodeDNSZoneDetail(w http.ResponseWriter, r *http.Request) {
	// HTMX fragment still used if bookmarked; prefer full Domains detail page.
	zone := normalizeDomainName(r.PathValue("zone"))
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/domains/d/"+url.PathEscape(zone), http.StatusSeeOther)
		return
	}
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "DNS zone")
	data.ActiveNav = "domains"
	data.DNSZoneName = zone
	data.PowerDNSReady = true

	sid := h.localServerID()
	var projects []storage.Project
	h.opts.DB.Where("server_id = ?", sid).Order("name asc").Find(&projects)
	data.Projects = projects

	var connected []storage.ConnectedDomain
	h.opts.DB.Where("server_id = ? AND domain = ?", sid, zone).Find(&connected)
	data.ConnectedDomains = connected

	z, err := h.pdnsClient().GetZone(zone)
	if err != nil {
		data.Flash = err.Error()
		h.render(w, "node_dns_zone", data)
		return
	}
	data.DNSZone = z
	data.DNSZoneName = z.Name
	h.render(w, "node_dns_zone", data)
}

func (h *handler) postNodeDNSRecord(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	zone := strings.TrimSpace(r.PathValue("zone"))
	name := strings.TrimSpace(r.FormValue("name"))
	rtype := strings.TrimSpace(r.FormValue("type"))
	content := strings.TrimSpace(r.FormValue("content"))
	ttl, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("ttl")))
	if ttl <= 0 {
		ttl = 300
	}
	var contents []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			contents = append(contents, line)
		}
	}
	redirect := "/domains/d/" + url.PathEscape(normalizeDomainName(zone)) + "?flash="
	if err := h.pdnsClient().UpsertRecord(zone, name, rtype, ttl, contents); err != nil {
		http.Redirect(w, r, redirect+urlQueryEscape("record failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redirect+urlQueryEscape(fmt.Sprintf("Record %s %s saved", name, rtype)), http.StatusSeeOther)
}

func (h *handler) postNodeDNSRecordDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	zone := strings.TrimSpace(r.PathValue("zone"))
	name := strings.TrimSpace(r.FormValue("name"))
	rtype := strings.TrimSpace(r.FormValue("type"))
	redir := "/domains/d/" + url.PathEscape(normalizeDomainName(zone))
	if err := h.pdnsClient().DeleteRecord(zone, name, rtype); err != nil {
		http.Redirect(w, r, redir+"?flash="+urlQueryEscape("delete record failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, redir+"?flash="+urlQueryEscape("Record deleted"), http.StatusSeeOther)
}

func (h *handler) postNodeDNSZoneLink(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	zone := strings.TrimSpace(strings.ToLower(strings.TrimSuffix(r.PathValue("zone"), ".")))
	if zone == "" {
		zone = strings.TrimSpace(strings.ToLower(r.FormValue("zone")))
	}
	pid, _ := strconv.ParseUint(r.FormValue("project_id"), 10, 64)
	dbType := strings.TrimSpace(r.FormValue("db_type"))
	dbName := strings.TrimSpace(r.FormValue("db_name"))
	upstream := strings.TrimSpace(r.FormValue("upstream"))

	sid := h.localServerID()
	var cd storage.ConnectedDomain
	err := h.opts.DB.Where("server_id = ? AND domain = ?", sid, zone).First(&cd).Error
	if err == gorm.ErrRecordNotFound {
		cd = storage.ConnectedDomain{ServerID: sid, Domain: zone, DNSReady: true}
	} else if err != nil {
		http.Redirect(w, r, "/domains?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if pid > 0 {
		p := uint(pid)
		cd.ProjectID = &p
	} else {
		cd.ProjectID = nil
	}
	cd.DBType = dbType
	cd.DBName = dbName
	if upstream != "" {
		cd.Upstream = upstream
	}
	if cd.ID == 0 {
		err = h.opts.DB.Create(&cd).Error
	} else {
		err = h.opts.DB.Save(&cd).Error
	}
	flash := "Linked " + zone
	if err != nil {
		flash = err.Error()
	}
	http.Redirect(w, r, "/domains/d/"+url.PathEscape(zone)+"?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}
