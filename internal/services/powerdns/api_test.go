package powerdns

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureZoneCreatesAndPatches(t *testing.T) {
	var posts, patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			t.Fatalf("missing api key")
		}
		switch {
		case r.Method == "GET" && stringsHasSuffix(r.URL.Path, "/zones/example.com."):
			http.NotFound(w, r)
		case r.Method == "POST" && stringsHasSuffix(r.URL.Path, "/zones"):
			posts++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "example.com."})
		case r.Method == "PATCH":
			patches++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{APIKey: "secret", BaseURL: srv.URL})
	if err := c.EnsureZone("example.com", "1.2.3.4", "mail.example.com"); err != nil {
		t.Fatal(err)
	}
	if posts != 1 || patches != 1 {
		t.Fatalf("posts=%d patches=%d", posts, patches)
	}
}

func stringsHasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
