package web

import (
	"net/http"
	"time"

	"github.com/lyracorp/xmanager/internal/services/webpanel"
)

func (h *handler) postNodePanelDisable(w http.ResponseWriter, r *http.Request) {
	h.handleNodePanelAction(w, r, false)
}

func (h *handler) postNodePanelUninstall(w http.ResponseWriter, r *http.Request) {
	h.handleNodePanelAction(w, r, true)
}

func (h *handler) handleNodePanelAction(w http.ResponseWriter, r *http.Request, uninstall bool) {
	_ = r.ParseForm()
	if r.FormValue("confirm") != "yes" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("cancelled"), http.StatusSeeOther)
		return
	}
	svc := webpanel.New(h.opts.DB, h.localServerID())
	title := "Panel stopping"
	flash := "The node web panel is shutting down. You can close this tab. Re-enable it from the TUI with w."
	inner := webpanel.CmdStop() + "; " + webpanel.CmdCleanupDocker()
	if uninstall {
		title = "Panel removed"
		flash = "The node web panel is being removed from this server (binary, unit, and data). You can close this tab."
		inner += "; " + webpanel.CmdUninstallFiles()
		_ = svc.DeleteInstance()
	} else if err := svc.SaveInstance(h.localServerID(), webpanel.ServiceType, "stopped", ""); err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	h.render(w, "panel_goodbye", pageData{
		Title:    title,
		Flash:    flash,
		NodeMode: false,
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	cmd := webpanel.CmdDeferred(inner)
	go func() {
		time.Sleep(400 * time.Millisecond)
		if ex := h.localExec(); ex != nil {
			_, _ = ex.Run(cmd)
		}
	}()
}
