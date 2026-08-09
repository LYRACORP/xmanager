package mailinbox

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateMailboxStalwart(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			t.Fatalf("auth missing")
		}
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/api/principal") {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			if m["type"] == "individual" {
				created = true
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{
		Mode:          ModeStalwart,
		APIBase:       srv.URL,
		AdminUser:     "admin",
		AdminPassword: "pass",
	})
	if err := c.CreateMailbox("alice", "example.com", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected principal create")
	}
}

func TestCreateMailboxMiaB(t *testing.T) {
	var gotEmail string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mail/users/add" {
			_ = r.ParseForm()
			gotEmail = r.FormValue("email")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient(Config{
		Mode:          ModeMiaB,
		APIBase:       srv.URL,
		AdminUser:     "admin@box.example",
		AdminPassword: "pass",
	})
	if err := c.CreateMailbox("bob", "example.com", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if gotEmail != "bob@example.com" {
		t.Fatalf("got %q", gotEmail)
	}
}
