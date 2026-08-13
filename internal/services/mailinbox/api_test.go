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

func TestListUsersAndDomainsMiaB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/mail/users" && r.URL.Query().Get("format") == "json":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"domain": "example.com", "users": []map[string]any{
					{"email": "alice@example.com", "status": "active", "privileges": []string{}},
				}},
			})
		case r.URL.Path == "/mail/domains":
			_, _ = w.Write([]byte("example.com\nother.org\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{Mode: ModeMiaB, APIBase: srv.URL, AdminUser: "a", AdminPassword: "b"})
	users, err := c.ListUsers()
	if err != nil || len(users) != 1 || users[0].Users[0].Email != "alice@example.com" {
		t.Fatalf("ListUsers: %+v err=%v", users, err)
	}
	domains, err := c.ListDomains()
	if err != nil || len(domains) != 2 {
		t.Fatalf("ListDomains: %+v err=%v", domains, err)
	}
	if _, err := NewClient(Config{Mode: ModeStalwart, APIBase: srv.URL}).ListUsers(); err == nil {
		t.Fatal("expected stalwart list error")
	}
}

func TestResolvedWebmailURL(t *testing.T) {
	c := Config{APIBase: "https://box.example/admin", Mode: ModeMiaB}
	if got := c.ResolvedWebmailURL(); got != "https://box.example/mail" {
		t.Fatalf("got %q", got)
	}
	c.WebmailURL = "https://mail.example/roundcube"
	if got := c.ResolvedWebmailURL(); got != "https://mail.example/roundcube" {
		t.Fatalf("override got %q", got)
	}
}
