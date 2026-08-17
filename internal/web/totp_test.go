package web

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lyracorp/xmanager/internal/auth"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func parseWebTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatBytes":  nodemetrics.FormatBytes,
		"formatUptime": nodemetrics.FormatUptime,
		"pathEscape":   url.PathEscape,
		"trimSlash":    func(s string) string { return strings.Trim(s, "/") },
		"trimDot":      func(s string) string { return strings.TrimRight(s, ".") },
		"json": func(v interface{}) (template.JS, error) {
			return "", nil
		},
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl
}

func TestLogin2FATemplate(t *testing.T) {
	tmpl := parseWebTemplates(t)
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "login_2fa", pageData{Title: "Two-factor authentication"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `action="/login/2fa"`) {
		t.Fatal("expected 2fa form")
	}
}

func TestSettings2FACard(t *testing.T) {
	tmpl := parseWebTemplates(t)
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "settings", pageData{Title: "Settings"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `action="/settings/2fa/start"`) {
		t.Fatal("expected enable 2FA")
	}
}

func TestPostLoginTOTPRedirectsTo2FA(t *testing.T) {
	h, db := aclTestHandler(t)
	h.opts.Config = &config.Config{}
	if err := auth.CreateUser(db, "alice", "secret", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.User{}).Where("username = ?", "alice").Update("totp_enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", h.postLogin)
	form := url.Values{"username": {"alice"}, "password": {"secret"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login/2fa" {
		t.Fatalf("location %q", loc)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == pending2FACookie && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected pending 2FA cookie")
	}
}

func TestGetLogin2FAWithoutCookieRedirects(t *testing.T) {
	h, _ := aclTestHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/2fa", h.getLogin2FA)
	req := httptest.NewRequest(http.MethodGet, "/login/2fa", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPostLogin2FAAcceptsValidCode(t *testing.T) {
	h, db := aclTestHandler(t)
	h.opts.Config = &config.Config{}
	if err := auth.CreateUser(db, "bob", "secret", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	en, err := auth.GenerateSecret("bob", "XManager")
	if err != nil {
		t.Fatal(err)
	}
	enc, err := auth.EncryptSecret(en.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.User{}).Where("username = ?", "bob").Updates(map[string]any{
		"totp_secret":  enc,
		"totp_enabled": true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var u storage.User
	db.Where("username = ?", "bob").First(&u)
	h.ensureTOTPStores()
	tok := h.pending2FA.put(pending2FA{UserID: u.ID, Username: u.Username, Role: u.Role})
	now := time.Now()
	code, err := totp.GenerateCodeCustom(en.Secret, now, totp.ValidateOpts{
		Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/2fa", h.postLogin2FA)
	form := url.Values{"code": {code}}
	req := httptest.NewRequest(http.MethodPost, "/login/2fa", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: pending2FACookie, Value: tok})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Fatalf("location %q", loc)
	}
	gotSess := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			gotSess = true
		}
	}
	if !gotSess {
		t.Fatal("expected session cookie")
	}
}
