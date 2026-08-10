package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/gitforge"
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
	if h.connectMode("github", gitforge.AppCredentials{ClientID: "x"}) != "device" {
		t.Fatal("expected device on http public url")
	}
	h.opts.Config.Web.PublicURL = "https://panel.example.com"
	if h.connectMode("github", gitforge.AppCredentials{ClientID: "x"}) != "redirect" {
		t.Fatal("expected redirect on https")
	}
	if h.connectMode("bitbucket", gitforge.AppCredentials{ClientID: "x"}) != "redirect" {
		t.Fatal("bitbucket https redirect")
	}
	h.opts.Config.Web.PublicURL = "http://x"
	if h.connectMode("bitbucket", gitforge.AppCredentials{ClientID: "x"}) != "need_https" {
		t.Fatal("bitbucket needs https")
	}
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
