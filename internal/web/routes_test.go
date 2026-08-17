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
	"github.com/lyracorp/xmanager/internal/nodemetrics"
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
	funcMap := template.FuncMap{
		"formatBytes":  nodemetrics.FormatBytes,
		"formatUptime": nodemetrics.FormatUptime,
		"pathEscape":   url.PathEscape,
		"trimSlash":    func(s string) string { return strings.Trim(s, "/") },
		"trimDot":      func(s string) string { return strings.TrimRight(s, ".") },
		"json": func(v interface{}) (template.JS, error) {
			return "", nil
		},
	}
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
