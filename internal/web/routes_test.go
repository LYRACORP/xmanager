package web

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/hosttime"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.AutoMigrate(&storage.GitCredential{}, &storage.GitOAuthApp{}, &storage.User{})
	return db
}

func TestRegisterNodeRoutesNoConflict(t *testing.T) {
	h := &handler{
		opts: Options{
			Config: &config.Config{},
		},
		nodeMode: true,
	}
	mux := http.NewServeMux()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerNode panicked: %v", r)
		}
	}()
	h.registerNode(mux)
}

func TestNodeDockerSwarmTemplate(t *testing.T) {
	funcMap := webTemplateFuncs()
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{
		Title:           "Docker",
		Swarm:           docker.SwarmInfo{State: "active", ControlAvailable: true, NodeAddr: "203.0.113.50"},
		SwarmWorkerJoin: "docker swarm join --token SWMTKN-1-example 203.0.113.50:2377",
		SwarmNodes: []docker.SwarmNode{
			{ID: "abc123def456", Hostname: "manager-1", Status: "Ready", Availability: "Active", ManagerStatus: "Leader"},
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "node_docker", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "docker swarm join --token") {
		t.Fatal("expected worker join command")
	}
	if !strings.Contains(out, "Init swarm") && !strings.Contains(out, "Leave swarm") {
		t.Fatal("expected swarm action")
	}
}

func TestAPIGitReposUnauthorized(t *testing.T) {
	db := openTestDB(t)
	h := &handler{
		opts:     Options{Config: &config.Config{}, DB: db},
		nodeMode: true,
		sess:     newSessionStore(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/git/repos", h.requireAuth(h.getAPIGitRepos))

	req := httptest.NewRequest(http.MethodGet, "/api/git/repos?provider=github", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d want redirect", rec.Code)
	}

	token := h.sess.create(1, "admin", "admin")
	req2 := httptest.NewRequest(http.MethodGet, "/api/git/repos?provider=github", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", rec2.Code, rec2.Body.String())
	}
}

func TestConnectMode(t *testing.T) {
	h := &handler{opts: Options{Config: &config.Config{Web: config.WebConfig{PublicURL: "http://1.2.3.4:8080"}}}}

	// GitHub with no client secret → relay (auth.lyracorp.dev handles the secret)
	if h.connectMode("github", gitforge.AppCredentials{ClientID: "x"}) != "relay" {
		t.Fatal("expected relay for GitHub when no local client secret")
	}
	// Client secret present → redirect flow takes priority over relay
	if h.connectMode("github", gitforge.AppCredentials{ClientID: "x", ClientSecret: "s"}) != "redirect" {
		t.Fatal("expected redirect when client secret set locally")
	}
	// Bitbucket with secret → redirect
	if h.connectMode("bitbucket", gitforge.AppCredentials{ClientID: "x", ClientSecret: "s"}) != "redirect" {
		t.Fatal("bitbucket with secret should redirect")
	}
	// Bitbucket without secret → no relay support → need_https
	if h.connectMode("bitbucket", gitforge.AppCredentials{ClientID: "x"}) != "need_https" {
		t.Fatal("bitbucket without secret needs https")
	}
	// No client id → need_client
	if h.connectMode("github", gitforge.AppCredentials{}) != "need_client" {
		t.Fatal("need client")
	}
}

func TestDevicePollGone(t *testing.T) {
	h := &handler{
		opts:         Options{Config: &config.Config{}, DB: openTestDB(t)},
		sess:         newSessionStore(),
		deviceStates: newDeviceStateStore(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /settings/git/{provider}/device/poll", h.requireAuth(h.getDevicePoll))
	token := h.sess.create(1, "admin", "admin")
	req := httptest.NewRequest(http.MethodGet, "/settings/git/github/device/poll?id=missing", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestNodeDateTimeTemplate(t *testing.T) {
	funcMap := webTemplateFuncs()
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{
		Title:     "Date & time",
		NodeMode:  true,
		IsAdmin:   true,
		ActiveNav: "time",
		HostTime: hosttime.Status{
			Timezone:        "UTC",
			LocalTime:       "2026-08-17 15:04:05 +0000",
			NTP:             true,
			NTPSynchronized: true,
		},
		Timezones: []string{"UTC", "America/New_York"},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "node_datetime", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "America/New_York") || !strings.Contains(out, `action="/time/timezone"`) {
		t.Fatal("expected timezone form")
	}
	if !strings.Contains(out, "2026-08-17T15:04") {
		t.Fatal("expected datetime-local value")
	}
}

func TestPostTimeTimezoneRejectsInjection(t *testing.T) {
	h, db := aclTestHandler(t)
	admin := storage.User{Username: "admin", PasswordHash: "x", Role: "admin", Enabled: true}
	db.Create(&admin)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /time/timezone", h.requireAdminAuth(h.postNodeTimeTimezone))

	token := h.sess.create(admin.ID, "admin", "admin")
	form := url.Values{"timezone": {"foo; rm -rf /"}}
	req := httptest.NewRequest(http.MethodPost, "/time/timezone", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "invalid") && !strings.Contains(loc, "timezone") {
		t.Fatalf("location %q", loc)
	}
}

func testWebFuncMap() template.FuncMap {
	return webTemplateFuncs()
}

func TestSettingsThisPanelCard(t *testing.T) {
	tmpl, err := template.New("").Funcs(testWebFuncMap()).ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{Title: "Settings", NodeMode: true, IsAdmin: true}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "settings", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `action="/settings/panel/disable"`) || !strings.Contains(out, `action="/settings/panel/uninstall"`) {
		t.Fatal("expected panel disable/remove forms")
	}
}

func TestPanelGoodbyeTemplate(t *testing.T) {
	tmpl, err := template.New("").Funcs(testWebFuncMap()).ParseFS(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "panel_goodbye", pageData{Title: "Panel stopping", Flash: "shutting down"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Panel stopping") {
		t.Fatal(buf.String())
	}
}

func TestPostPanelDisableRequiresConfirm(t *testing.T) {
	h, db := aclTestHandler(t)
	admin := storage.User{Username: "admin", PasswordHash: "x", Role: "admin", Enabled: true}
	db.Create(&admin)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /settings/panel/disable", h.requireAdminAuth(h.postNodePanelDisable))
	token := h.sess.create(admin.ID, "admin", "admin")
	req := httptest.NewRequest(http.MethodPost, "/settings/panel/disable", strings.NewReader("confirm=no"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPostPanelUninstallRejectsNonAdmin(t *testing.T) {
	h, db := aclTestHandler(t)
	user := storage.User{Username: "u", PasswordHash: "x", Role: "user", Enabled: true}
	db.Create(&user)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /settings/panel/uninstall", h.requireAdminAuth(h.postNodePanelUninstall))
	token := h.sess.create(user.ID, "u", "user")
	form := url.Values{"confirm": {"yes"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/panel/uninstall", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d want 303", rec.Code)
	}
}
