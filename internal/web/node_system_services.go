package web

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type systemServiceView struct {
	Unit        string
	Load        string
	Active      string
	Sub         string
	Description string
	Running     bool
	Failed      bool
}

var systemdUnitNameRe = regexp.MustCompile(`^[a-zA-Z0-9@_.\-]+(\.service)?$`)

func sanitizeSystemdUnit(unit string) (string, error) {
	unit = strings.TrimSpace(unit)
	unit = strings.TrimSuffix(unit, "/")
	if unit == "" {
		return "", fmt.Errorf("unit required")
	}
	if !systemdUnitNameRe.MatchString(unit) {
		return "", fmt.Errorf("invalid unit name")
	}
	if !strings.HasSuffix(unit, ".service") {
		unit += ".service"
	}
	return unit, nil
}

func parseSystemctlListUnits(out string) []systemServiceView {
	var rows []systemServiceView
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "UNIT ") || strings.HasPrefix(line, "Legend:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		unit := fields[0]
		if !strings.HasSuffix(unit, ".service") {
			continue
		}
		load, active, sub := fields[1], fields[2], fields[3]
		desc := ""
		if len(fields) > 4 {
			desc = strings.Join(fields[4:], " ")
		}
		rows = append(rows, systemServiceView{
			Unit:        unit,
			Load:        load,
			Active:      active,
			Sub:         sub,
			Description: desc,
			Running:     active == "active",
			Failed:      active == "failed" || sub == "failed",
		})
	}
	return rows
}

func filterSystemServices(all []systemServiceView, filter string) []systemServiceView {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "running":
		var out []systemServiceView
		for _, s := range all {
			if s.Running {
				out = append(out, s)
			}
		}
		return out
	case "failed":
		var out []systemServiceView
		for _, s := range all {
			if s.Failed {
				out = append(out, s)
			}
		}
		return out
	default:
		return all
	}
}

func listSystemdServices(exec *ssh.Executor) ([]systemServiceView, error) {
	if exec == nil {
		return nil, fmt.Errorf("no executor")
	}
	out := exec.RunQuiet("systemctl list-units --type=service --all --no-pager --plain 2>/dev/null")
	if strings.TrimSpace(out) == "" {
		// Fallback without --plain (older systemd)
		out = exec.RunQuiet("systemctl list-units --type=service --all --no-pager --no-legend 2>/dev/null")
	}
	return parseSystemctlListUnits(out), nil
}

func (h *handler) getNodeSystemServices(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter")))
	if filter == "" {
		filter = "all"
	}

	all, err := listSystemdServices(h.localExec())
	data := h.basePage(sess, "Services")
	data.ActiveNav = "services"
	data.SystemSvcFilter = filter
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	if err != nil {
		if data.Flash == "" {
			data.Flash = "Could not list systemd units: " + err.Error()
		}
	}
	data.SystemServices = filterSystemServices(all, filter)
	h.render(w, "node_system_services", data)
}

func (h *handler) postNodeSystemServiceAction(w http.ResponseWriter, r *http.Request) {
	unit, err := sanitizeSystemdUnit(r.PathValue("unit"))
	if err != nil {
		http.Redirect(w, r, "/services?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	action := "restart"
	switch {
	case strings.HasSuffix(r.URL.Path, "/start"):
		action = "start"
	case strings.HasSuffix(r.URL.Path, "/stop"):
		action = "stop"
	case strings.HasSuffix(r.URL.Path, "/restart"):
		action = "restart"
	}

	exec := h.localExec()
	cmd := fmt.Sprintf("systemctl %s %s 2>&1", action, unit)
	res, runErr := exec.Run(cmd)
	flash := unit + " " + action + " ok"
	if runErr != nil {
		flash = unit + " " + action + " failed: " + runErr.Error()
	} else if res != nil && res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stdout + res.Stderr)
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.ExitCode)
		}
		flash = unit + " " + action + " failed: " + msg
	}
	http.Redirect(w, r, "/services?flash="+url.QueryEscape(flash), http.StatusSeeOther)
}
