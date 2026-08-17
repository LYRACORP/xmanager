package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) postSettingsPassword(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	current := r.FormValue("current_password")
	newPass := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	if newPass == "" || newPass != confirm {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("passwords do not match"), http.StatusSeeOther)
		return
	}
	var u storage.User
	if err := h.opts.DB.First(&u, sess.UserID).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, current) {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("current password incorrect"), http.StatusSeeOther)
		return
	}
	if err := auth.SetPassword(h.opts.DB, sess.UserID, newPass); err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("Password updated"), http.StatusSeeOther)
}

func (h *handler) postSettingsUsers(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	role := strings.TrimSpace(r.FormValue("role"))
	if username == "" || password == "" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("username and password required"), http.StatusSeeOther)
		return
	}
	if role == "" {
		role = auth.RoleUser
	}
	if err := auth.CreateUser(h.opts.DB, username, password, role); err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("User "+username+" created"), http.StatusSeeOther)
}

func (h *handler) postSettingsUserPassword(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	password := r.FormValue("password")
	if id == 0 || password == "" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("user id and password required"), http.StatusSeeOther)
		return
	}
	if err := auth.SetPassword(h.opts.DB, uint(id), password); err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("Password reset"), http.StatusSeeOther)
}

func (h *handler) postSettingsUserToggle(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if id == 0 {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid user"), http.StatusSeeOther)
		return
	}
	var u storage.User
	if err := h.opts.DB.First(&u, id).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	newEnabled := !u.Enabled
	if !newEnabled && auth.IsAdmin(u.Role) {
		n, err := auth.CountEnabledAdmins(h.opts.DB)
		if err != nil || n <= 1 {
			http.Redirect(w, r, "/settings?flash="+urlQueryEscape("cannot disable the last enabled admin"), http.StatusSeeOther)
			return
		}
	}
	if sess != nil && sess.UserID == u.ID && !newEnabled {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("cannot disable your own account"), http.StatusSeeOther)
		return
	}
	if err := h.opts.DB.Model(&u).Update("enabled", newEnabled).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	msg := "User enabled"
	if !newEnabled {
		msg = "User disabled"
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape(msg), http.StatusSeeOther)
}

func (h *handler) loadPanelUsers() []storage.User {
	var users []storage.User
	h.opts.DB.Order("username asc").Find(&users)
	return users
}
