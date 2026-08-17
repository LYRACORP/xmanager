package web

import (
	"net/http"
	"strconv"
	"strings"

	xmftp "github.com/lyracorp/xmanager/internal/ftp"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) ftpMgr() *xmftp.Manager {
	return xmftp.New(h.localExec())
}

func (h *handler) ftpEnabledUsernames(sid uint) []string {
	var users []storage.FTPUser
	h.opts.DB.Where("server_id = ? AND enabled = ?", sid, true).Order("username asc").Find(&users)
	out := make([]string, 0, len(users))
	for _, u := range users {
		out = append(out, u.Username)
	}
	return out
}

func (h *handler) loadFTPUser(id uint) (*storage.FTPUser, error) {
	var u storage.FTPUser
	err := h.opts.DB.Where("id = ? AND server_id = ?", id, h.localServerID()).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (h *handler) getNodeFTP(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	var users []storage.FTPUser
	h.opts.DB.Where("server_id = ?", sid).Order("username asc").Find(&users)
	data := h.basePage(sess, "FTP")
	data.ActiveNav = "ftp"
	data.FTPUsers = users
	data.FTPEnabled = h.ftpMgr().IsEnabled()
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "node_ftp", data)
}

func (h *handler) postNodeFTPToggle(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	action := strings.ToLower(strings.TrimSpace(r.FormValue("action")))
	mgr := h.ftpMgr()
	var err error
	flash := "FTP updated"
	switch action {
	case "enable":
		err = mgr.Enable(h.ftpEnabledUsernames(h.localServerID()))
		if err == nil {
			flash = "FTP enabled (vsftpd :21, passive 40000–40100)"
		}
	case "disable":
		err = mgr.Disable()
		if err == nil {
			flash = "FTP disabled"
		}
	default:
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("unknown action"), http.StatusSeeOther)
		return
	}
	if err != nil {
		flash = err.Error()
	}
	http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeFTPUser(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.ToLower(strings.TrimSpace(r.FormValue("username")))
	password := r.FormValue("password")
	home := strings.TrimSpace(r.FormValue("home"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	sid := h.localServerID()

	home, err := h.ftpMgr().CreateUser(username, password, home)
	if err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	row := storage.FTPUser{
		ServerID: sid,
		Username: username,
		Home:     home,
		Enabled:  true,
		Notes:    notes,
	}
	if err := h.opts.DB.Create(&row).Error; err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("host user created, DB save failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("User "+username+" created · jail "+home), http.StatusSeeOther)
}

func (h *handler) postNodeFTPUserPassword(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadFTPUser(uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	if err := h.ftpMgr().SetPassword(u.Username, r.FormValue("password")); err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("Password updated for "+u.Username), http.StatusSeeOther)
}

func (h *handler) postNodeFTPUserEnable(w http.ResponseWriter, r *http.Request) {
	h.setFTPUserEnabled(w, r, true)
}

func (h *handler) postNodeFTPUserDisable(w http.ResponseWriter, r *http.Request) {
	h.setFTPUserEnabled(w, r, false)
}

func (h *handler) setFTPUserEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadFTPUser(uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	if err := h.ftpMgr().SetEnabled(u.Username, enabled); err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	h.opts.DB.Model(u).Update("enabled", enabled)
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(u.Username+" "+state), http.StatusSeeOther)
}

func (h *handler) postNodeFTPUserDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadFTPUser(uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	removeHome := r.FormValue("remove_home") == "1"
	if err := h.ftpMgr().DeleteUser(u.Username, removeHome); err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	h.opts.DB.Delete(u)
	http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("Deleted "+u.Username), http.StatusSeeOther)
}
