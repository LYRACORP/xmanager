package powerdns

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestListZonesAndCRUD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/servers/localhost/zones":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "example.com.", "kind": "Native", "serial": 1},
			})
		case r.Method == "GET" && stringsHasSuffix(r.URL.Path, "/zones/example.com."):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "example.com.", "kind": "Native", "serial": 2,
				"rrsets": []map[string]any{
					{"name": "example.com.", "type": "A", "ttl": 300, "records": []map[string]any{{"content": "1.2.3.4", "disabled": false}}},
				},
			})
		case r.Method == "DELETE" && stringsHasSuffix(r.URL.Path, "/zones/example.com."):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "PATCH":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{APIKey: "k", BaseURL: srv.URL})
	zones, err := c.ListZones()
	if err != nil || len(zones) != 1 || zones[0].Name != "example.com" {
		t.Fatalf("ListZones: %+v err=%v", zones, err)
	}
	z, err := c.GetZone("example.com")
	if err != nil || z.RecordCount != 1 {
		t.Fatalf("GetZone: %+v err=%v", z, err)
	}
	if err := c.UpsertRecord("example.com", "www", "A", 60, []string{"9.9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteRecord("example.com", "www.example.com.", "A"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteZone("example.com"); err != nil {
		t.Fatal(err)
	}
}

func stringsHasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func TestDefaultAPIPort(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.APIPort != DefaultAPIPort || DefaultAPIPort != "8082" {
		t.Fatalf("default api port=%q want 8082", cfg.APIPort)
	}
	parsed := ParseConfig(`{"api_key":"x"}`)
	if parsed.APIPort != "8082" {
		t.Fatalf("empty api_port fallback=%q", parsed.APIPort)
	}
}

func TestComposeYAMLPublishesHostPort(t *testing.T) {
	yml := ComposeYAML(Config{APIKey: "k", DNSPort: "53", APIPort: "8082"})
	if !strings.Contains(yml, `"8082:8081"`) && !strings.Contains(yml, "8082:8081") {
		t.Fatalf("expected host 8082 -> container 8081:\n%s", yml)
	}
	if !strings.Contains(yml, "PDNS_AUTH_WEBSERVER_PORT=8081") {
		t.Fatal("container should listen on 8081")
	}
	if strings.Contains(yml, `"8081:8081"`) {
		t.Fatal("default compose should not publish host 8081 (Adminer)")
	}
}

