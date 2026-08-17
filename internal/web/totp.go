package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/securityevents"
	"github.com/lyracorp/xmanager/internal/storage"
)

const (
	pending2FACookie = "xm_2fa"
	pending2FAMaxAge = 300
	pending2FAFails  = 5
)

type pending2FA struct {
	UserID   uint
	Username string
	Role     string
	expires  time.Time
	fails    int
}

type pending2FAStore struct {
	mu   sync.Mutex
	data map[string]*pending2FA
}

func newPending2FAStore() *pending2FAStore {
	return &pending2FAStore{data: make(map[string]*pending2FA)}
}

func (s *pending2FAStore) put(p pending2FA) string {
	tok := randomToken()
	p.expires = time.Now().Add(pending2FAMaxAge * time.Second)
	s.mu.Lock()
	s.data[tok] = &p
	s.mu.Unlock()
	return tok
}

func (s *pending2FAStore) get(tok string) (*pending2FA, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[tok]
	if !ok || time.Now().After(p.expires) {
		delete(s.data, tok)
		return nil, false
	}
	return p, true
}

func (s *pending2FAStore) fail(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[tok]
	if !ok {
		return false
	}
	p.fails++
	if p.fails >= pending2FAFails {
		delete(s.data, tok)
		return false
	}
	return true
}

func (s *pending2FAStore) delete(tok string) {
	s.mu.Lock()
	delete(s.data, tok)
	s.mu.Unlock()
}

type totpEnroll struct {
	Secret  string
	QR      string
	expires time.Time
}

type totpEnrollStore struct {
	mu     sync.Mutex
	byUser map[uint]*totpEnroll
}

func newTOTPEnrollStore() *totpEnrollStore {
	return &totpEnrollStore{byUser: make(map[uint]*totpEnroll)}
}

func (s *totpEnrollStore) put(userID uint, en totpEnroll) {
	en.expires = time.Now().Add(10 * time.Minute)
	s.mu.Lock()
	s.byUser[userID] = &en
	s.mu.Unlock()
}

func (s *totpEnrollStore) get(userID uint) (*totpEnroll, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	en, ok := s.byUser[userID]
	if !ok || time.Now().After(en.expires) {
		delete(s.byUser, userID)
		return nil, false
	}
	return en, true
}

func (s *totpEnrollStore) delete(userID uint) {
	s.mu.Lock()
	delete(s.byUser, userID)
	s.mu.Unlock()
}

type totpRevealStore struct {
	mu     sync.Mutex
	byUser map[uint][]string
}

func newTOTPRevealStore() *totpRevealStore {
	return &totpRevealStore{byUser: make(map[uint][]string)}
}

func (s *totpRevealStore) put(userID uint, codes []string) {
	s.mu.Lock()
	s.byUser[userID] = codes
	s.mu.Unlock()
}

func (s *totpRevealStore) take(userID uint) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes := s.byUser[userID]
	delete(s.byUser, userID)
	return codes
}

func (h *handler) ensureTOTPStores() {
	if h.pending2FA == nil {
		h.pending2FA = newPending2FAStore()
	}
	if h.totpEnroll == nil {
		h.totpEnroll = newTOTPEnrollStore()
	}
	if h.totpReveal == nil {
		h.totpReveal = newTOTPRevealStore()
	}
}

func setPending2FACookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     pending2FACookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   pending2FAMaxAge,
	})
}

func clearPending2FACookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pending2FACookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (h *handler) pendingFromRequest(r *http.Request) (string, *pending2FA, bool) {
	h.ensureTOTPStores()
	c, err := r.Cookie(pending2FACookie)
	if err != nil {
		return "", nil, false
	}
	p, ok := h.pending2FA.get(c.Value)
	return c.Value, p, ok
}

func (h *handler) getLogin2FA(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.pendingFromRequest(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	flash := r.URL.Query().Get("flash")
	h.render(w, "login_2fa", pageData{Title: "Two-factor authentication", NodeMode: h.nodeMode, Flash: flash})
}

func (h *handler) postLogin2FA(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	tok, p, ok := h.pendingFromRequest(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	var u storage.User
	if err := h.opts.DB.First(&u, p.UserID).Error; err != nil || !u.Enabled || !u.TOTPEnabled {
		h.pending2FA.delete(tok)
		clearPending2FACookie(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	code := r.FormValue("code")
	recovery := r.FormValue("recovery")
	ok2FA := false
	if secret, err := auth.DecryptSecret(u.TOTPSecret); err == nil && auth.ValidateCode(secret, code) {
		ok2FA = true
	}
	if !ok2FA && recovery != "" {
		if next, used := auth.ConsumeRecoveryCode(u.TOTPRecovery, recovery); used {
			u.TOTPRecovery = next
			_ = h.opts.DB.Model(&u).Update("totp_recovery", next).Error
			ok2FA = true
		}
	}
	ip := clientIPFromRequest(r)
	if !ok2FA {
		securityevents.Log(h.opts.DB, securityevents.Entry{
			ServerID: h.localServerID(),
			Kind:     securityevents.KindLoginFail,
			IP:       ip,
			Actor:    p.Username,
			Detail:   "invalid 2fa",
		})
		if !h.pending2FA.fail(tok) {
			clearPending2FACookie(w)
			h.render(w, "login", pageData{Title: "Login", NodeMode: h.nodeMode, Flash: "Too many attempts. Sign in again."})
			return
		}
		h.render(w, "login_2fa", pageData{Title: "Two-factor authentication", NodeMode: h.nodeMode, Flash: "Invalid authenticator or recovery code."})
		return
	}
	h.pending2FA.delete(tok)
	clearPending2FACookie(w)
	securityevents.Log(h.opts.DB, securityevents.Entry{
		ServerID: h.localServerID(),
		Kind:     securityevents.KindLoginOK,
		IP:       ip,
		Actor:    p.Username,
	})
	sessionTok := h.sess.create(p.UserID, p.Username, p.Role)
	setSessionCookie(w, sessionTok)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *handler) loadUser(id uint) (*storage.User, error) {
	var u storage.User
	if err := h.opts.DB.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (h *handler) postSettings2FAStart(w http.ResponseWriter, r *http.Request) {
	h.ensureTOTPStores()
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	u, err := h.loadUser(sess.UserID)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	if u.TOTPEnabled {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA is already enabled"), http.StatusSeeOther)
		return
	}
	en, err := auth.GenerateSecret(sess.Username, "XManager")
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	h.totpEnroll.put(sess.UserID, totpEnroll{Secret: en.Secret, QR: en.QRDataURI})
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (h *handler) postSettings2FAConfirm(w http.ResponseWriter, r *http.Request) {
	h.ensureTOTPStores()
	_ = r.ParseForm()
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	en, ok := h.totpEnroll.get(sess.UserID)
	if !ok {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA setup expired — start again"), http.StatusSeeOther)
		return
	}
	if !auth.ValidateCode(en.Secret, r.FormValue("code")) {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid authenticator code"), http.StatusSeeOther)
		return
	}
	enc, err := auth.EncryptSecret(en.Secret)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	plain, hashed, err := auth.GenerateRecoveryCodes()
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := h.opts.DB.Model(&storage.User{}).Where("id = ?", sess.UserID).Updates(map[string]any{
		"totp_secret":   enc,
		"totp_enabled":  true,
		"totp_recovery": hashed,
	}).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	h.totpEnroll.delete(sess.UserID)
	h.totpReveal.put(sess.UserID, plain)
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA enabled — save your recovery codes now"), http.StatusSeeOther)
}

func (h *handler) verifyUserTOTP(u *storage.User, code, recovery string) bool {
	if secret, err := auth.DecryptSecret(u.TOTPSecret); err == nil && auth.ValidateCode(secret, code) {
		return true
	}
	if recovery == "" {
		return false
	}
	next, used := auth.ConsumeRecoveryCode(u.TOTPRecovery, recovery)
	if !used {
		return false
	}
	u.TOTPRecovery = next
	_ = h.opts.DB.Model(u).Update("totp_recovery", next).Error
	return true
}

func (h *handler) postSettings2FADisable(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	u, err := h.loadUser(sess.UserID)
	if err != nil || !u.TOTPEnabled {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA is not enabled"), http.StatusSeeOther)
		return
	}
	if !h.verifyUserTOTP(u, r.FormValue("code"), r.FormValue("recovery")) {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid authenticator or recovery code"), http.StatusSeeOther)
		return
	}
	if err := h.opts.DB.Model(u).Updates(map[string]any{
		"totp_secret":   "",
		"totp_enabled":  false,
		"totp_recovery": "",
	}).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA disabled"), http.StatusSeeOther)
}

func (h *handler) postSettings2FARecovery(w http.ResponseWriter, r *http.Request) {
	h.ensureTOTPStores()
	_ = r.ParseForm()
	sess := sessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	u, err := h.loadUser(sess.UserID)
	if err != nil || !u.TOTPEnabled {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA is not enabled"), http.StatusSeeOther)
		return
	}
	if !h.verifyUserTOTP(u, r.FormValue("code"), r.FormValue("recovery")) {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid authenticator or recovery code"), http.StatusSeeOther)
		return
	}
	plain, hashed, err := auth.GenerateRecoveryCodes()
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := h.opts.DB.Model(u).Update("totp_recovery", hashed).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	h.totpReveal.put(sess.UserID, plain)
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("New recovery codes generated — save them now"), http.StatusSeeOther)
}

func (h *handler) postSettingsUserReset2FA(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if id == 0 {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid user"), http.StatusSeeOther)
		return
	}
	if sess != nil && sess.UserID == uint(id) {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("disable 2FA on your own account from Two-factor below"), http.StatusSeeOther)
		return
	}
	var u storage.User
	if err := h.opts.DB.First(&u, id).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	if err := h.opts.DB.Model(&u).Updates(map[string]any{
		"totp_secret":   "",
		"totp_enabled":  false,
		"totp_recovery": "",
	}).Error; err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("2FA reset for "+u.Username), http.StatusSeeOther)
}
