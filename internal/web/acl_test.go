package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func aclTestHandler(t *testing.T) (*handler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&storage.User{}, &storage.Project{}, &storage.Server{}); err != nil {
		t.Fatal(err)
	}
	srv := storage.Server{Name: "local", Host: "127.0.0.1", User: "root", IsActive: true}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	h := &handler{
		opts: Options{DB: db},
		sess: newSessionStore(),
	}
	h.localSrvID = srv.ID
	return h, db
}

func withSessionReq(h *handler, userID uint, role string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	token := h.sess.create(userID, "tester", role)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	sess, _ := h.sess.get(token)
	return r.WithContext(withSession(r.Context(), sess))
}

func TestScopeServerQueryFiltersUser(t *testing.T) {
	h, db := aclTestHandler(t)
	admin := storage.User{Username: "admin", PasswordHash: "x", Role: "admin", Enabled: true}
	user := storage.User{Username: "user", PasswordHash: "x", Role: "user", Enabled: true}
	db.Create(&admin)
	db.Create(&user)
	db.Create(&storage.Project{Name: "mine", ServerID: h.localServerID(), UserID: user.ID})
	db.Create(&storage.Project{Name: "legacy", ServerID: h.localServerID(), UserID: 0})
	db.Create(&storage.Project{Name: "other", ServerID: h.localServerID(), UserID: 999})

	r := withSessionReq(h, user.ID, "user")
	var n int64
	h.scopeServerQuery(r, &storage.Project{}).Count(&n)
	if n != 1 {
		t.Fatalf("user scope count = %d, want 1", n)
	}

	rAdmin := withSessionReq(h, admin.ID, "admin")
	h.scopeServerQuery(rAdmin, &storage.Project{}).Count(&n)
	if n != 3 {
		t.Fatalf("admin scope count = %d, want 3", n)
	}
}

func TestRequireAdminRedirectsNonAdmin(t *testing.T) {
	h, db := aclTestHandler(t)
	user := storage.User{Username: "u", PasswordHash: "x", Role: "user", Enabled: true}
	db.Create(&user)

	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /secret", h.requireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	r := withSessionReq(h, user.ID, "user")
	r.URL.Path = "/secret"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; loc=%s", rr.Code, rr.Header().Get("Location"))
	}
	if called {
		t.Fatal("handler should not run for non-admin")
	}
}

func TestLoadOwnedNodeProject404OtherUser(t *testing.T) {
	h, db := aclTestHandler(t)
	owner := storage.User{Username: "owner", PasswordHash: "x", Role: "user", Enabled: true}
	other := storage.User{Username: "other", PasswordHash: "x", Role: "user", Enabled: true}
	db.Create(&owner)
	db.Create(&other)
	p := storage.Project{Name: "p1", ServerID: h.localServerID(), UserID: owner.ID}
	db.Create(&p)

	r := withSessionReq(h, other.ID, "user")
	if _, err := h.loadOwnedNodeProject(r, p.ID); err != gorm.ErrRecordNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestCanOwnLegacyAdminOnly(t *testing.T) {
	h, _ := aclTestHandler(t)
	userReq := withSessionReq(h, 5, "user")
	if h.canOwn(userReq, 0) {
		t.Fatal("legacy resource should be admin-only")
	}
	adminReq := withSessionReq(h, 1, "admin")
	if !h.canOwn(adminReq, 0) {
		t.Fatal("admin should access legacy resources")
	}
}
