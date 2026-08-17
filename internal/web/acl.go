package web

import (
	"net/http"
	"strings"

	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func (s *session) IsAdmin() bool {
	if s == nil {
		return false
	}
	return auth.IsAdmin(s.Role)
}

func fillPageACL(data *pageData, sess *session) {
	if sess == nil {
		return
	}
	data.IsAdmin = sess.IsAdmin()
	data.UserRole = auth.NormalizeRole(sess.Role)
}

func (h *handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := sessionFromCtx(r.Context())
		if sess == nil || !sess.IsAdmin() {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			http.Redirect(w, r, "/?flash="+urlQueryEscape("Admin access required"), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (h *handler) requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return h.requireAuth(h.requireAdmin(next))
}

func (h *handler) scopeUserQuery(r *http.Request) *gorm.DB {
	q := h.opts.DB
	sess := sessionFromCtx(r.Context())
	if sess != nil && !sess.IsAdmin() {
		q = q.Where("user_id = ?", sess.UserID)
	}
	return q
}

func (h *handler) canOwn(r *http.Request, ownerID uint) bool {
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		return false
	}
	if sess.IsAdmin() {
		return true
	}
	if ownerID == 0 {
		return false
	}
	return ownerID == sess.UserID
}

func (h *handler) requireOwner(w http.ResponseWriter, r *http.Request, ownerID uint) bool {
	if !h.canOwn(r, ownerID) {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (h *handler) actingUserID(r *http.Request) uint {
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		return 0
	}
	return sess.UserID
}

func (h *handler) loadOwnedNodeProject(r *http.Request, id uint) (*storage.Project, error) {
	var p storage.Project
	err := h.opts.DB.Where("id = ? AND server_id = ?", id, h.localServerID()).First(&p).Error
	if err != nil {
		return nil, err
	}
	if !h.canOwn(r, p.UserID) {
		return nil, gorm.ErrRecordNotFound
	}
	return &p, nil
}

func (h *handler) loadOwnedConnectedDomain(r *http.Request, domain string) (*storage.ConnectedDomain, error) {
	domain = normalizeDomainName(domain)
	var cd storage.ConnectedDomain
	if err := h.opts.DB.Where("server_id = ? AND domain = ?", h.localServerID(), domain).First(&cd).Error; err != nil {
		return nil, err
	}
	if !h.canOwn(r, cd.UserID) {
		return nil, gorm.ErrRecordNotFound
	}
	return &cd, nil
}

func (h *handler) loadOwnedFTPUser(r *http.Request, id uint) (*storage.FTPUser, error) {
	var u storage.FTPUser
	err := h.opts.DB.Where("id = ? AND server_id = ?", id, h.localServerID()).First(&u).Error
	if err != nil {
		return nil, err
	}
	if !h.canOwn(r, u.UserID) {
		return nil, gorm.ErrRecordNotFound
	}
	return &u, nil
}

func (h *handler) ownedDatabaseNames(r *http.Request) map[string]map[string]bool {
	sess := sessionFromCtx(r.Context())
	if sess != nil && sess.IsAdmin() {
		return nil
	}
	var rows []storage.ProjectDatabase
	q := h.opts.DB.Where("server_id = ?", h.localServerID())
	if sess != nil {
		q = q.Where("user_id = ?", sess.UserID)
	}
	q.Find(&rows)
	out := make(map[string]map[string]bool)
	for _, row := range rows {
		if out[row.DBType] == nil {
			out[row.DBType] = make(map[string]bool)
		}
		out[row.DBType][row.DBName] = true
	}
	return out
}

func (h *handler) canAccessDatabase(r *http.Request, dbType, name string) bool {
	owned := h.ownedDatabaseNames(r)
	if owned == nil {
		return true
	}
	if m, ok := owned[dbType]; ok {
		return m[name]
	}
	return false
}

func (h *handler) ownedBucketNames(r *http.Request) map[string]bool {
	sess := sessionFromCtx(r.Context())
	if sess != nil && sess.IsAdmin() {
		return nil
	}
	var rows []storage.StorageBucket
	q := h.opts.DB.Where("server_id = ?", h.localServerID())
	if sess != nil {
		q = q.Where("user_id = ?", sess.UserID)
	}
	q.Find(&rows)
	out := make(map[string]bool, len(rows))
	for _, b := range rows {
		out[b.Name] = true
	}
	return out
}

func (h *handler) requireOwnedBucket(w http.ResponseWriter, r *http.Request, name string) bool {
	owned := h.ownedBucketNames(r)
	if owned == nil {
		return true
	}
	if !owned[name] {
		http.NotFound(w, r)
		return false
	}
	return true
}

func (h *handler) connectDomainFromRequest(r *http.Request, opts domainConnectOpts) (*storage.ConnectedDomain, error) {
	opts.OwnerUserID, opts.IsAdmin = h.domainConnectACL(r)
	return h.connectDomain(opts)
}

func (h *handler) domainConnectACL(r *http.Request) (ownerUserID uint, isAdmin bool) {
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		return 0, false
	}
	return sess.UserID, sess.IsAdmin()
}

func (h *handler) scopeServerQuery(r *http.Request, model any) *gorm.DB {
	q := h.opts.DB.Where("server_id = ?", h.localServerID())
	sess := sessionFromCtx(r.Context())
	if sess != nil && !sess.IsAdmin() {
		q = q.Where("user_id = ?", sess.UserID)
	}
	return q.Model(model)
}

func displayRole(role string) string {
	return auth.NormalizeRole(role)
}
