package web

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/reqdump"
	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/securityevents"
	svcReqdump "github.com/lyracorp/xmanager/internal/services/reqdump"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/waf"
)

type securityPageView struct {
	Firewall      security.FirewallStatus
	SSH           security.SSHConfig
	SSLCerts      []security.SSLCert
	Fail2ban      security.Fail2banStatus
	AuthFails     []security.AuthFail
	Events        []storage.SecurityEvent
	Dumps         []storage.RequestDump
	DumpConfig    reqdump.Config
	HoneypotPorts string
	Policy        security.Policy
	Modsec        wafModsecView
	BlockIPsText  string
	AllowIPsText  string
}

func (h *handler) reloadReqDump() {
	if h.dumpMgr == nil {
		return
	}
	h.dumpMgr.Reload()
	cfg := h.dumpMgr.Config()
	exec := h.localExec()
	if cfg.Enabled && cfg.DumpNginx && exec != nil {
		_ = reqdump.ConfigureNginxMirror(exec, h.dumpMgr.SinkURL())
	} else if exec != nil {
		_ = reqdump.RemoveNginxMirror(exec)
	}
}

func (h *handler) getNodeSecurity(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	exec := h.localExec()
	sid := h.localServerID()

	fw := security.ReadFirewall(exec)
	sshCfg := security.SSHConfigRead(exec)
	certs, _, _ := security.SSLCerts(exec)
	f2b := security.ReadFail2ban(exec)
	authFails := security.AuthFailures(exec, 80)
	events, _ := securityevents.List(h.opts.DB, securityevents.Filter{ServerID: sid, Limit: 100})
	dumps, _ := reqdump.List(h.opts.DB, reqdump.ListFilter{ServerID: sid, Limit: 50})
	dumpCfg := reqdump.LoadConfig(h.opts.DB, sid)
	policy := security.LoadPolicy(h.opts.DB, sid)
	modsec := waf.DetectModsec(exec)

	data := h.basePage(sess, "Security")
	data.ActiveNav = "security"
	data.Security = securityPageView{
		Firewall:      fw,
		SSH:           sshCfg,
		SSLCerts:      certs,
		Fail2ban:      f2b,
		AuthFails:     authFails,
		Events:        events,
		Dumps:         dumps,
		DumpConfig:    dumpCfg,
		HoneypotPorts: dumpCfg.HoneypotPorts,
		Policy:        policy,
		Modsec:        wafModsecView{Installed: modsec.Installed, Enabled: modsec.Enabled, Detail: modsec.Detail},
		BlockIPsText:  strings.Join(policy.BlockIPs, "\n"),
		AllowIPsText:  strings.Join(policy.AllowIPs, "\n"),
	}
	data.SecurityPolicy = policy
	data.ModsecStatus = data.Security.Modsec
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_security", data)
}

func (h *handler) postNodeSecurityFirewall(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := r.FormValue("action")
	exec := h.localExec()
	var err error
	switch action {
	case "open":
		err = security.AllowPorts(exec, r.FormValue("ports"))
	case "close":
		port, _ := strconv.Atoi(r.FormValue("port"))
		proto := r.FormValue("proto")
		if proto == "" {
			proto = "tcp"
		}
		err = security.DenyPort(exec, port, proto, panelProtectedPorts())
	default:
		http.Redirect(w, r, "/security?flash=unknown+action", http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/security?flash="+url.QueryEscape("firewall: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/security?flash=firewall+updated", http.StatusSeeOther)
}

func (h *handler) postNodeSecuritySSHHarden(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("confirm") != "yes" {
		http.Redirect(w, r, "/security?flash=hardening+cancelled", http.StatusSeeOther)
		return
	}
	if err := security.ApplySSHHardening(h.localExec()); err != nil {
		http.Redirect(w, r, "/security?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/security?flash=ssh+hardening+applied", http.StatusSeeOther)
}

func (h *handler) postNodeSecuritySSLRenew(w http.ResponseWriter, r *http.Request) {
	out, err := security.RenewSSL(h.localExec())
	if err != nil {
		http.Redirect(w, r, "/security?flash="+url.QueryEscape("renew: "+err.Error()), http.StatusSeeOther)
		return
	}
	msg := strings.TrimSpace(out)
	if len(msg) > 120 {
		msg = msg[:120] + "…"
	}
	http.Redirect(w, r, "/security?flash="+url.QueryEscape("certbot: "+msg), http.StatusSeeOther)
}

func (h *handler) postNodeSecurityFail2banUnban(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	jail := r.FormValue("jail")
	ip := r.FormValue("ip")
	if err := security.UnbanIP(h.localExec(), jail, ip); err != nil {
		http.Redirect(w, r, "/security?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/security?flash=ip+unbanned", http.StatusSeeOther)
}

func (h *handler) postNodeSecurityDump(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid := h.localServerID()
	cfg := reqdump.LoadConfig(h.opts.DB, sid)
	cfg.Enabled = r.FormValue("enabled") == "on"
	cfg.DumpPanel = r.FormValue("dump_panel") == "on"
	cfg.DumpNginx = r.FormValue("dump_nginx") == "on"
	cfg.DumpHoneypot = r.FormValue("dump_honeypot") == "on"
	if ports := strings.TrimSpace(r.FormValue("honeypot_ports")); ports != "" {
		cfg.HoneypotPorts = ports
	}
	if err := reqdump.SaveConfig(h.opts.DB, sid, cfg); err != nil {
		http.Redirect(w, r, "/security?flash=save+failed", http.StatusSeeOther)
		return
	}
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=dump+settings+saved", http.StatusSeeOther)
}

func (h *handler) getNodeSecurityDumpDetail(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	row, err := reqdump.Get(h.opts.DB, h.localServerID(), uint(id))
	if err != nil || row == nil {
		http.Redirect(w, r, "/security?flash=dump+not+found", http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("raw") == "1" {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dump-%d.bin", row.ID))
		_, _ = w.Write(row.Body)
		return
	}
	data := h.basePage(sess, "Request dump")
	data.ActiveNav = "security"
	data.RequestDump = row
	data.RequestDumpHex = hex.EncodeToString(row.Body)
	h.render(w, "node_security_dump", data)
}

func (h *handler) postInternalReqdump(w http.ResponseWriter, r *http.Request) {
	if h.dumpMgr == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	// handled by manager sink server on loopback; this route is fallback on main mux
	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1") && !strings.HasPrefix(r.RemoteAddr, "[::1]") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.dumpMgr.HandleNginxSink(w, r)
}

func panelProtectedPorts() map[int]string {
	extra := map[int]string{}
	for p, label := range hostfirewall.ProtectedPorts {
		extra[p] = label
	}
	return extra
}

func (h *handler) lookupReqdumpService() *svcReqdump.Service {
	return svcReqdump.New(h.opts.DB, h.localServerID(), h.reloadSecurityAll)
}
