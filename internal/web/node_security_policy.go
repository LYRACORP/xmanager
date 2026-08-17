package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/security"
	"github.com/lyracorp/xmanager/internal/traffic"
	"github.com/lyracorp/xmanager/internal/waf"
)

type wafModsecView struct {
	Installed bool
	Enabled   bool
	Detail    string
}

func (h *handler) reloadSecurityAll() {
	h.reloadReqDump()
	if h.secStack != nil {
		h.secStack.reload(h.localExec())
	}
}

func (h *handler) parseSecurityPolicy(r *http.Request) security.Policy {
	_ = r.ParseForm()
	p := security.LoadPolicy(h.opts.DB, h.localServerID())
	p.Enabled = r.FormValue("enabled") == "on"
	p.RateRPM = formInt(r, "rate_rpm", p.RateRPM)
	p.RateBurst = formInt(r, "rate_burst", p.RateBurst)
	p.ConnLimit = formInt(r, "conn_limit", p.ConnLimit)
	p.WAFBuiltin = r.FormValue("waf_builtin") == "on"
	p.WAFSQLi = r.FormValue("waf_sqli") == "on"
	p.WAFXSS = r.FormValue("waf_xss") == "on"
	p.WAFTraversal = r.FormValue("waf_traversal") == "on"
	p.WAFBadBots = r.FormValue("waf_bad_bots") == "on"
	p.WAFModsec = r.FormValue("waf_modsec") == "on"
	p.ModsecDetectOnly = r.FormValue("modsec_detect_only") == "on"
	p.ModsecParanoia = formInt(r, "modsec_paranoia", p.ModsecParanoia)
	p.AutoFail2ban = formInt(r, "auto_fail2ban", p.AutoFail2ban)
	p.AnalysisEnabled = r.FormValue("analysis_enabled") == "on"
	if v := strings.TrimSpace(r.FormValue("block_ips")); v != "" {
		p.BlockIPs = splitLines(v)
	}
	if v := strings.TrimSpace(r.FormValue("allow_ips")); v != "" {
		p.AllowIPs = splitLines(v)
	}
	return p
}

func formInt(r *http.Request, key string, def int) int {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n'
	}) {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (h *handler) postNodeSecurityPolicy(w http.ResponseWriter, r *http.Request) {
	p := h.parseSecurityPolicy(r)
	if err := security.SavePolicy(h.opts.DB, h.localServerID(), p); err != nil {
		http.Redirect(w, r, "/security?flash=policy+save+failed", http.StatusSeeOther)
		return
	}
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=policy+saved", http.StatusSeeOther)
}

func (h *handler) postNodeSecurityIPBlock(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ip := strings.TrimSpace(r.FormValue("ip"))
	if ip == "" {
		http.Redirect(w, r, "/security?flash=ip+required", http.StatusSeeOther)
		return
	}
	p := security.LoadPolicy(h.opts.DB, h.localServerID())
	security.AddBlockIP(&p, ip)
	_ = security.SavePolicy(h.opts.DB, h.localServerID(), p)
	if exec := h.localExec(); exec != nil {
		_ = exec.RunQuiet("sudo ufw deny from " + shellSafeIP(ip) + " 2>/dev/null || true")
	}
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=ip+blocked", http.StatusSeeOther)
}

func (h *handler) postNodeSecurityIPUnblock(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ip := strings.TrimSpace(r.FormValue("ip"))
	p := security.LoadPolicy(h.opts.DB, h.localServerID())
	security.RemoveBlockIP(&p, ip)
	_ = security.SavePolicy(h.opts.DB, h.localServerID(), p)
	if exec := h.localExec(); exec != nil {
		_ = exec.RunQuiet("sudo ufw delete deny from " + shellSafeIP(ip) + " 2>/dev/null || true")
	}
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=ip+unblocked", http.StatusSeeOther)
}

func shellSafeIP(ip string) string {
	return strings.NewReplacer(";", "", "&", "", "|", "", "`", "", " ", "").Replace(ip)
}

func (h *handler) postNodeSecurityModsecInstall(w http.ResponseWriter, r *http.Request) {
	p := security.LoadPolicy(h.opts.DB, h.localServerID())
	p.WAFModsec = true
	if err := waf.InstallModsec(h.localExec(), p); err != nil {
		http.Redirect(w, r, "/security?flash="+urlEscapeShort(err.Error()), http.StatusSeeOther)
		return
	}
	_ = security.SavePolicy(h.opts.DB, h.localServerID(), p)
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=modsecurity+installed", http.StatusSeeOther)
}

func (h *handler) postNodeSecurityModsecRemove(w http.ResponseWriter, r *http.Request) {
	if err := waf.RemoveModsec(h.localExec()); err != nil {
		http.Redirect(w, r, "/security?flash="+urlEscapeShort(err.Error()), http.StatusSeeOther)
		return
	}
	p := security.LoadPolicy(h.opts.DB, h.localServerID())
	p.WAFModsec = false
	_ = security.SavePolicy(h.opts.DB, h.localServerID(), p)
	h.reloadSecurityAll()
	http.Redirect(w, r, "/security?flash=modsecurity+removed", http.StatusSeeOther)
}

func urlEscapeShort(s string) string {
	return strings.ReplaceAll(s, " ", "+")
}

func (h *handler) getAPISecurityTraffic(w http.ResponseWriter, r *http.Request) {
	rng := strings.TrimSpace(r.URL.Query().Get("range"))
	since := time.Now().Add(-time.Hour)
	switch rng {
	case "6h":
		since = time.Now().Add(-6 * time.Hour)
	case "1d":
		since = time.Now().Add(-24 * time.Hour)
	default:
		rng = "1h"
	}
	analysis, err := traffic.Analyze(h.opts.DB, h.localServerID(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"range":    rng,
		"analysis": analysis,
	})
}
