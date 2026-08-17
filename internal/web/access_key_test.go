package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testAccessMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<a href="/projects">p</a><link href="//cdn.example/x">`))
	})
	mux.HandleFunc("GET /go", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
	})
	return mux
}

func TestAccessGateRequiresKey(t *testing.T) {
	g := newAccessGate("qekyfk7518312j1", testAccessMux())

	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("bare /login status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/qekyfk7518312j1/login", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("keyed /login status = %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Result().Body)
	s := string(body)
	if !strings.Contains(s, `href="/qekyfk7518312j1/projects"`) {
		t.Fatalf("expected prefixed href, got %s", s)
	}
	if !strings.Contains(s, `href="//cdn.example/x"`) {
		t.Fatalf("protocol-relative URL rewritten: %s", s)
	}
}

func TestAccessGatePrefixesRedirect(t *testing.T) {
	g := newAccessGate("qekyfk7518312j1", testAccessMux())
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/qekyfk7518312j1/go", nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc != "/qekyfk7518312j1/projects" {
		t.Fatalf("Location = %q", loc)
	}
}

func TestAccessGateKeyChange(t *testing.T) {
	g := newAccessGate("oldkey12", testAccessMux())
	g.setKey("newkey34")

	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/oldkey12/login", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("old key status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/newkey34/login", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("new key status = %d", rr.Code)
	}
}

func TestRewriteHTMLDoesNotDoublePrefix(t *testing.T) {
	in := []byte(`<a href="/abc12345/projects">x</a>`)
	out := string(rewriteHTMLPaths(in, "/abc12345"))
	if strings.Contains(out, "/abc12345/abc12345/") {
		t.Fatalf("double prefix: %s", out)
	}
	if !strings.Contains(out, `href="/abc12345/projects"`) {
		t.Fatalf("got %s", out)
	}
}

func TestPrefixLocation(t *testing.T) {
	if got := prefixLocation("/k", "/projects"); got != "/k/projects" {
		t.Fatal(got)
	}
	if got := prefixLocation("/k", "/k/projects"); got != "/k/projects" {
		t.Fatal(got)
	}
	if got := prefixLocation("/k", "/"); got != "/k/" {
		t.Fatal(got)
	}
	if got := prefixLocation("/k", "https://ex"); got != "https://ex" {
		t.Fatal(got)
	}
}
