package apps

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteCatalogListAndLoad(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v4/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"oneClickApps":[{"name":"privatebin","displayName":"PrivateBin","description":"paste","isOfficial":true,"logoUrl":"privatebin.png"}]}`))
	})
	mux.HandleFunc("/v4/apps/privatebin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"captainVersion":4,"services":{"$$cap_appname":{"image":"privatebin/nginx-fpm-alpine:1.5.1"}},"caproverOneClickApp":{"displayName":"PrivateBin","description":"paste","isOfficial":true,"variables":[{"id":"$$cap_version","label":"Version","defaultValue":"1.5.1"}],"instructions":{"start":"hi","end":"done"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	l := &Loader{
		dirs:   []string{"/nonexistent"},
		remote: &CatalogClient{Base: srv.URL + "/v4", Client: srv.Client()},
	}
	sums, err := l.ListSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].ID != "privatebin" {
		t.Fatalf("summaries: %+v", sums)
	}
	app, err := l.Load("privatebin")
	if err != nil {
		t.Fatal(err)
	}
	if app.DisplayName != "PrivateBin" || app.ComposeYAML == "" {
		t.Fatalf("app: %+v compose=%q", app, app.ComposeYAML)
	}
	compose, err := l.Render("privatebin", map[string]string{"$$cap_version": "9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	if compose == "" {
		t.Fatal("empty render")
	}
}
