package web

import (
	"net/http"
	"net/url"

	"github.com/lyracorp/xmanager/internal/hosttime"
)

func (h *handler) fillHostTime(data *pageData, withZones bool) {
	ex := h.localExec()
	st, err := hosttime.ReadStatus(ex)
	if err != nil {
		if data.Flash == "" {
			data.Flash = err.Error()
		}
		return
	}
	data.HostTime = st
	if !withZones {
		return
	}
	zones, err := hosttime.ListTimezones(ex)
	if err != nil && data.Flash == "" {
		data.Flash = err.Error()
	}
	data.Timezones = zones
}

func (h *handler) getNodeTime(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "Date & time")
	data.ActiveNav = "time"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.fillHostTime(&data, true)
	h.render(w, "node_datetime", data)
}

func (h *handler) postNodeTimeTimezone(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if err := hosttime.SetTimezone(h.localExec(), r.FormValue("timezone")); err != nil {
		http.Redirect(w, r, "/time?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/time?flash=timezone+updated", http.StatusSeeOther)
}

func (h *handler) postNodeTimeClock(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if err := hosttime.SetTime(h.localExec(), r.FormValue("clock")); err != nil {
		http.Redirect(w, r, "/time?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/time?flash=clock+updated", http.StatusSeeOther)
}

func (h *handler) postNodeTimeNTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	switch r.FormValue("action") {
	case "on":
		if err := hosttime.SetNTP(h.localExec(), true); err != nil {
			http.Redirect(w, r, "/time?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/time?flash=NTP+enabled", http.StatusSeeOther)
	case "off":
		if err := hosttime.SetNTP(h.localExec(), false); err != nil {
			http.Redirect(w, r, "/time?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/time?flash=NTP+disabled", http.StatusSeeOther)
	case "sync":
		if _, err := hosttime.SyncNTP(h.localExec()); err != nil {
			http.Redirect(w, r, "/time?flash="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/time?flash=NTP+sync+requested", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/time?flash=unknown+action", http.StatusSeeOther)
	}
}
