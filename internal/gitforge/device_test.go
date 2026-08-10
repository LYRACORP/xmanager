package gitforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestAndPollDeviceFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"device_code":      "dev123",
			"user_code":        "WDJB-MJHT",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("device_code") != "dev123" {
			http.Error(w, "bad", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "tok",
			"token_type":   "bearer",
			"scope":        "repo",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	app := AppCredentials{ClientID: "cid", Endpoint: srv.URL}
	dc, err := RequestDeviceCode(context.Background(), ProviderGitHub, app)
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "WDJB-MJHT" {
		t.Fatalf("%+v", dc)
	}
	ts, err := PollDeviceToken(context.Background(), ProviderGitHub, app, dc.DeviceCode)
	if err != nil || ts.AccessToken != "tok" {
		t.Fatalf("ts=%+v err=%v", ts, err)
	}
}

func TestSupportsDeviceFlow(t *testing.T) {
	if !SupportsDeviceFlow("github") || !SupportsDeviceFlow("gitlab") {
		t.Fatal("expected gh/gl")
	}
	if SupportsDeviceFlow("bitbucket") || SupportsDeviceFlow("gitea") {
		t.Fatal("bb/gitea should be false")
	}
}

func TestMergeCredentials(t *testing.T) {
	got := MergeCredentials(
		AppCredentials{ClientID: "a", ClientSecret: "s"},
		AppCredentials{ClientID: "b"},
	)
	if got.ClientID != "b" || got.ClientSecret != "s" {
		t.Fatalf("%+v", got)
	}
}
