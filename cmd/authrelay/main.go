// Package main is the Lyracorp OAuth relay service (auth.lyracorp.dev).
//
// It acts as a trusted middleman for GitHub / GitLab OAuth, holding the
// registered client secret server-side so that self-hosted XManager panels
// can use the standard browser redirect flow without registering their own
// OAuth app or exposing a client secret.
//
// Flow:
//
//  1. XManager panel → GET /connect/github?panel_url=...&nonce=...&next=...
//  2. Relay → redirects browser to GitHub authorize page
//  3. GitHub → redirects to GET /callback/github?code=...&state=...
//  4. Relay exchanges code for token (server-side, using relay's client secret)
//  5. Relay stores token under a one-time ID (5 min TTL)
//  6. Relay → redirects browser back to panel /oauth/git/github/relay-finish?nonce=...&token_id=...
//  7. Panel GETs /token/{id} from relay, receives token, stores credential, loads repos
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Provider config ─────────────────────────────────────────────────────────

type providerCfg struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       string
}

var providerMap = map[string]*providerCfg{}

// ─── HMAC-signed state ────────────────────────────────────────────────────────

type statePayload struct {
	Provider string `json:"p"`
	PanelURL string `json:"u"`
	Nonce    string `json:"n"`
	Next     string `json:"nx,omitempty"`
	Exp      int64  `json:"e"`
}

func encodeState(hmacKey []byte, d statePayload) (string, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func decodeState(hmacKey []byte, token string) (statePayload, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return statePayload{}, false
	}
	payload, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return statePayload{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return statePayload{}, false
	}
	var d statePayload
	if err := json.Unmarshal(b, &d); err != nil {
		return statePayload{}, false
	}
	if time.Now().Unix() > d.Exp {
		return statePayload{}, false
	}
	return d, true
}

// ─── One-time token store ─────────────────────────────────────────────────────

type tokenEntry struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"` // unix
}

type tokenStore struct {
	mu   sync.Mutex
	data map[string]tokenEntry
}

func (s *tokenStore) put(id string, e tokenEntry, ttl time.Duration) {
	s.mu.Lock()
	if s.data == nil {
		s.data = make(map[string]tokenEntry)
	}
	s.data[id] = e
	s.mu.Unlock()
	time.AfterFunc(ttl, func() {
		s.mu.Lock()
		delete(s.data, id)
		s.mu.Unlock()
	})
}

func (s *tokenStore) take(id string) (tokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.data[id]
	if ok {
		delete(s.data, id)
	}
	return e, ok
}

// ─── Relay server ─────────────────────────────────────────────────────────────

type relay struct {
	baseURL  string
	hmacKey  []byte
	tokens   tokenStore
	hc       *http.Client
	log      *slog.Logger
}

func newRelay(baseURL, hmacSecret string) *relay {
	return &relay{
		baseURL: strings.TrimRight(baseURL, "/"),
		hmacKey: []byte(hmacSecret),
		hc:      &http.Client{Timeout: 15 * time.Second},
		log:     slog.Default(),
	}
}

func randID() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (rl *relay) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connect/{provider}", rl.handleConnect)
	mux.HandleFunc("GET /callback/{provider}", rl.handleCallback)
	mux.HandleFunc("GET /token/{id}", rl.handleTokenPickup)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	return mux
}

// GET /connect/{provider}?panel_url=...&nonce=...&next=...
func (rl *relay) handleConnect(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	cfg, ok := providerMap[provider]
	if !ok || cfg.ClientID == "" {
		http.Error(w, "unsupported or unconfigured provider: "+provider, http.StatusBadRequest)
		return
	}

	panelURL := strings.TrimSpace(r.URL.Query().Get("panel_url"))
	nonce := strings.TrimSpace(r.URL.Query().Get("nonce"))
	next := r.URL.Query().Get("next")
	if panelURL == "" || nonce == "" {
		http.Error(w, "panel_url and nonce are required", http.StatusBadRequest)
		return
	}
	pu, err := url.Parse(panelURL)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		http.Error(w, "invalid panel_url scheme", http.StatusBadRequest)
		return
	}

	state, err := encodeState(rl.hmacKey, statePayload{
		Provider: provider,
		PanelURL: panelURL,
		Nonce:    nonce,
		Next:     next,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	q := url.Values{}
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", rl.baseURL+"/callback/"+provider)
	q.Set("scope", cfg.Scopes)
	q.Set("state", state)
	q.Set("response_type", "code")

	rl.log.Info("connect", "provider", provider, "panel", panelURL)
	http.Redirect(w, r, cfg.AuthURL+"?"+q.Encode(), http.StatusFound)
}

// GET /callback/{provider}?code=...&state=...
func (rl *relay) handleCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	cfg, ok := providerMap[provider]
	if !ok {
		http.Error(w, "unsupported provider", http.StatusBadRequest)
		return
	}

	errParam := r.URL.Query().Get("error")
	if errParam != "" {
		desc := r.URL.Query().Get("error_description")
		http.Error(w, "OAuth denied: "+errParam+" — "+desc, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	stateToken := r.URL.Query().Get("state")
	if code == "" || stateToken == "" {
		http.Error(w, "code and state required", http.StatusBadRequest)
		return
	}

	sd, ok := decodeState(rl.hmacKey, stateToken)
	if !ok || sd.Provider != provider {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	// Exchange code for token server-side.
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", rl.baseURL+"/callback/"+provider)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		http.Error(w, "request error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := rl.hc.Do(req)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "parse error: "+string(body), http.StatusBadGateway)
		return
	}
	if raw.Error != "" {
		http.Error(w, "OAuth error: "+raw.Error+" — "+raw.ErrorDesc, http.StatusBadGateway)
		return
	}
	if raw.AccessToken == "" {
		http.Error(w, "empty access token from provider", http.StatusBadGateway)
		return
	}

	var expiresAt int64
	if raw.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second).Unix()
	}

	id := randID()
	rl.tokens.put(id, tokenEntry{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		Scope:        raw.Scope,
		ExpiresAt:    expiresAt,
	}, 5*time.Minute)

	// Redirect browser back to the originating XManager panel.
	panelCB := sd.PanelURL + "/oauth/git/" + provider + "/relay-finish"
	q := url.Values{}
	q.Set("nonce", sd.Nonce)
	q.Set("token_id", id)
	if sd.Next != "" {
		q.Set("next", sd.Next)
	}
	rl.log.Info("callback success", "provider", provider, "panel", sd.PanelURL)
	http.Redirect(w, r, panelCB+"?"+q.Encode(), http.StatusFound)
}

// GET /token/{id} — one-time token pickup (called server-side by the panel).
func (rl *relay) handleTokenPickup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	e, ok := rl.tokens.take(id)
	if !ok {
		http.Error(w, "token not found or already collected", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(e)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return v
}

func main() {
	baseURL := mustEnv("RELAY_BASE_URL")   // https://auth.lyracorp.dev
	hmacSec := mustEnv("RELAY_HMAC_SECRET") // random 32+ char string

	// Register configured providers.
	if id := os.Getenv("GITHUB_CLIENT_ID"); id != "" {
		providerMap["github"] = &providerCfg{
			ClientID:     id,
			ClientSecret: mustEnv("GITHUB_CLIENT_SECRET"),
			AuthURL:      "https://github.com/login/oauth/authorize",
			TokenURL:     "https://github.com/login/oauth/access_token",
			Scopes:       "repo,read:user,user:email",
		}
		slog.Info("provider enabled", "name", "github")
	}
	if id := os.Getenv("GITLAB_CLIENT_ID"); id != "" {
		providerMap["gitlab"] = &providerCfg{
			ClientID:     id,
			ClientSecret: mustEnv("GITLAB_CLIENT_SECRET"),
			AuthURL:      "https://gitlab.com/oauth/authorize",
			TokenURL:     "https://gitlab.com/oauth/token",
			Scopes:       "read_user api",
		}
		slog.Info("provider enabled", "name", "gitlab")
	}
	if len(providerMap) == 0 {
		log.Fatal("no providers configured — set GITHUB_CLIENT_ID / GITLAB_CLIENT_ID")
	}

	rl := newRelay(baseURL, hmacSec)

	addr := os.Getenv("RELAY_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	slog.Info("auth relay starting", "addr", addr, "base_url", baseURL)
	if err := http.ListenAndServe(addr, rl.routes()); err != nil {
		log.Fatal(err)
	}
}
