package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/storage"
)

type oauthPending struct {
	Provider string
	UserID   uint
	Created  time.Time
}

type oauthStateStore struct {
	mu   sync.Mutex
	data map[string]oauthPending
}

func newOAuthStateStore() *oauthStateStore {
	s := &oauthStateStore{data: make(map[string]oauthPending)}
	go s.gc()
	return s
}

func (s *oauthStateStore) put(state string, p oauthPending) {
	s.mu.Lock()
	s.data[state] = p
	s.mu.Unlock()
}

func (s *oauthStateStore) take(state string) (oauthPending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[state]
	if ok {
		delete(s.data, state)
	}
	return p, ok
}

func (s *oauthStateStore) gc() {
	t := time.NewTicker(10 * time.Minute)
	for range t.C {
		s.mu.Lock()
		for k, v := range s.data {
			if time.Since(v.Created) > 15*time.Minute {
				delete(s.data, k)
			}
		}
		s.mu.Unlock()
	}
}

type gitProviderView struct {
	Provider     string
	Label        string
	ClientID     string
	Endpoint     string
	HasSecret    bool
	Connected    bool
	AccountLogin string
	CredID       uint
}

func gitProviderLabels() []struct{ ID, Label string } {
	return []struct{ ID, Label string }{
		{gitforge.ProviderGitHub, "GitHub"},
		{gitforge.ProviderGitLab, "GitLab"},
		{gitforge.ProviderBitbucket, "Bitbucket"},
		{gitforge.ProviderGitea, "Gitea (local)"},
	}
}

func (h *handler) publicPanelURL(r *http.Request) string {
	if h.opts.Config != nil {
		if u := strings.TrimRight(strings.TrimSpace(h.opts.Config.Web.PublicURL), "/"); u != "" {
			return u
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:8080"
	}
	return scheme + "://" + host
}

func (h *handler) oauthRedirectURI(r *http.Request, provider string) string {
	return h.publicPanelURL(r) + "/oauth/git/" + provider + "/callback"
}

func (h *handler) defaultGiteaEndpoint() string {
	if h.opts.DB == nil {
		return "http://127.0.0.1:3000"
	}
	var inst storage.ServiceInstance
	err := h.opts.DB.Where("server_id = ? AND service_type = ?", h.localServerID(), "gitea").First(&inst).Error
	if err != nil {
		return "http://127.0.0.1:3000"
	}
	port := "3000"
	if inst.ConfigJSON != "" {
		var cfg map[string]string
		if json.Unmarshal([]byte(inst.ConfigJSON), &cfg) == nil && cfg["http_port"] != "" {
			port = cfg["http_port"]
		}
	}
	return "http://127.0.0.1:" + port
}

func (h *handler) loadOAuthApp(provider string) (*storage.GitOAuthApp, error) {
	var app storage.GitOAuthApp
	err := h.opts.DB.Where("provider = ?", provider).First(&app).Error
	if err != nil {
		return nil, err
	}
	return &app, nil
}

func (h *handler) appCredentials(provider string) (gitforge.AppCredentials, error) {
	app, err := h.loadOAuthApp(provider)
	if err != nil {
		return gitforge.AppCredentials{}, fmt.Errorf("configure OAuth app for %s in Settings first", provider)
	}
	secret, err := config.Decrypt(app.ClientSecretEncrypted)
	if err != nil {
		return gitforge.AppCredentials{}, fmt.Errorf("decrypting client secret: %w", err)
	}
	endpoint := app.Endpoint
	if provider == gitforge.ProviderGitea && endpoint == "" {
		endpoint = h.defaultGiteaEndpoint()
	}
	return gitforge.AppCredentials{
		ClientID:     app.ClientID,
		ClientSecret: secret,
		Endpoint:     endpoint,
	}, nil
}

func (h *handler) loadCredential(provider string) (*storage.GitCredential, error) {
	var cred storage.GitCredential
	q := h.opts.DB.Where("provider = ?", provider)
	if provider == gitforge.ProviderGitea {
		// prefer matching endpoint when multiple; for now first
	}
	if err := q.Order("updated_at desc").First(&cred).Error; err != nil {
		return nil, err
	}
	return &cred, nil
}

func (h *handler) decryptAccessToken(cred *storage.GitCredential) (string, error) {
	return config.Decrypt(cred.TokenEncrypted)
}

func (h *handler) ensureFreshToken(ctx context.Context, cred *storage.GitCredential) (string, error) {
	token, err := h.decryptAccessToken(cred)
	if err != nil {
		return "", err
	}
	if cred.ExpiresAt == nil || time.Until(*cred.ExpiresAt) > 2*time.Minute {
		return token, nil
	}
	if cred.RefreshEncrypted == "" {
		return token, nil
	}
	refresh, err := config.Decrypt(cred.RefreshEncrypted)
	if err != nil {
		return token, nil
	}
	app, err := h.appCredentials(cred.Provider)
	if err != nil {
		return token, nil
	}
	ts, err := gitforge.RefreshAccessToken(ctx, cred.Provider, app, refresh)
	if err != nil {
		return token, nil
	}
	enc, err := config.Encrypt(ts.AccessToken)
	if err != nil {
		return ts.AccessToken, nil
	}
	cred.TokenEncrypted = enc
	if ts.RefreshToken != "" {
		if renc, err := config.Encrypt(ts.RefreshToken); err == nil {
			cred.RefreshEncrypted = renc
		}
	}
	if ts.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(ts.ExpiresIn) * time.Second)
		cred.ExpiresAt = &exp
	}
	_ = h.opts.DB.Save(cred).Error
	return ts.AccessToken, nil
}

func (h *handler) gitProviderViews() []gitProviderView {
	out := make([]gitProviderView, 0, 4)
	for _, p := range gitProviderLabels() {
		v := gitProviderView{Provider: p.ID, Label: p.Label}
		if app, err := h.loadOAuthApp(p.ID); err == nil {
			v.ClientID = app.ClientID
			v.Endpoint = app.Endpoint
			v.HasSecret = app.ClientSecretEncrypted != ""
		}
		if p.ID == gitforge.ProviderGitea && v.Endpoint == "" {
			v.Endpoint = h.defaultGiteaEndpoint()
		}
		if cred, err := h.loadCredential(p.ID); err == nil && cred.TokenEncrypted != "" {
			v.Connected = true
			v.AccountLogin = cred.AccountLogin
			if v.AccountLogin == "" {
				v.AccountLogin = cred.Username
			}
			v.CredID = cred.ID
		}
		out = append(out, v)
	}
	return out
}

func (h *handler) connectedCredID(provider string) uint {
	cred, err := h.loadCredential(provider)
	if err != nil {
		return 0
	}
	return cred.ID
}

func parseUintForm(s string) uint {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return uint(n)
}

func (h *handler) postSettingsGit(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if h.opts.Config != nil {
		pub := strings.TrimRight(strings.TrimSpace(r.FormValue("public_url")), "/")
		h.opts.Config.Web.PublicURL = pub
		_ = config.Save(h.opts.Config)
	}
	for _, p := range gitProviderLabels() {
		id := p.ID
		clientID := strings.TrimSpace(r.FormValue(id + "_client_id"))
		clientSecret := strings.TrimSpace(r.FormValue(id + "_client_secret"))
		endpoint := strings.TrimSpace(r.FormValue(id + "_endpoint"))
		if clientID == "" && clientSecret == "" && endpoint == "" {
			continue
		}
		var app storage.GitOAuthApp
		err := h.opts.DB.Where("provider = ?", id).First(&app).Error
		if err != nil {
			app = storage.GitOAuthApp{Provider: id, Enabled: true}
		}
		if clientID != "" {
			app.ClientID = clientID
		}
		if endpoint != "" || id == gitforge.ProviderGitea || id == gitforge.ProviderGitLab {
			app.Endpoint = endpoint
		}
		if clientSecret != "" {
			enc, err := config.Encrypt(clientSecret)
			if err != nil {
				http.Redirect(w, r, "/settings?flash="+urlQueryEscape("encrypt failed: "+err.Error()), http.StatusSeeOther)
				return
			}
			app.ClientSecretEncrypted = enc
		}
		if app.ClientID == "" {
			continue
		}
		if app.ID == 0 {
			_ = h.opts.DB.Create(&app).Error
		} else {
			_ = h.opts.DB.Save(&app).Error
		}
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("Git provider settings saved"), http.StatusSeeOther)
}

func (h *handler) getOAuthGitConnect(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	if provider == "" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("unknown provider"), http.StatusSeeOther)
		return
	}
	app, err := h.appCredentials(provider)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	state := randomToken()
	if h.oauthStates == nil {
		h.oauthStates = newOAuthStateStore()
	}
	h.oauthStates.put(state, oauthPending{Provider: provider, UserID: sess.UserID, Created: time.Now()})
	authURL, err := gitforge.AuthorizeURL(provider, app, h.oauthRedirectURI(r, provider), state)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (h *handler) getOAuthGitCallback(w http.ResponseWriter, r *http.Request) {
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("oauth: "+errMsg), http.StatusSeeOther)
		return
	}
	if provider == "" || code == "" || state == "" {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("invalid oauth callback"), http.StatusSeeOther)
		return
	}
	if h.oauthStates == nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("oauth state expired"), http.StatusSeeOther)
		return
	}
	pending, ok := h.oauthStates.take(state)
	if !ok || pending.Provider != provider {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("oauth state mismatch"), http.StatusSeeOther)
		return
	}
	app, err := h.appCredentials(provider)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	ts, err := gitforge.ExchangeCode(ctx, provider, app, h.oauthRedirectURI(r, provider), code)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("token exchange: "+err.Error()), http.StatusSeeOther)
		return
	}
	acct, err := gitforge.FetchAccount(ctx, provider, app.Endpoint, ts.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape("fetch user: "+err.Error()), http.StatusSeeOther)
		return
	}
	enc, err := config.Encrypt(ts.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/settings?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	cred := storage.GitCredential{
		Provider:       provider,
		Username:       acct.Login,
		AccountLogin:   acct.Login,
		TokenEncrypted: enc,
		Endpoint:       app.Endpoint,
		Scopes:         ts.Scope,
	}
	if ts.RefreshToken != "" {
		if renc, err := config.Encrypt(ts.RefreshToken); err == nil {
			cred.RefreshEncrypted = renc
		}
	}
	if ts.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(ts.ExpiresIn) * time.Second)
		cred.ExpiresAt = &exp
	}
	var existing storage.GitCredential
	if err := h.opts.DB.Where("provider = ?", provider).Order("id desc").First(&existing).Error; err == nil {
		cred.ID = existing.ID
		cred.CreatedAt = existing.CreatedAt
		_ = h.opts.DB.Save(&cred).Error
	} else {
		_ = h.opts.DB.Create(&cred).Error
	}
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("Connected "+provider+" as "+acct.Login), http.StatusSeeOther)
}

func (h *handler) postOAuthGitDisconnect(w http.ResponseWriter, r *http.Request) {
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	if provider == "" {
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	_ = h.opts.DB.Where("provider = ?", provider).Delete(&storage.GitCredential{}).Error
	http.Redirect(w, r, "/settings?flash="+urlQueryEscape("Disconnected "+provider), http.StatusSeeOther)
}

func (h *handler) getAPIGitRepos(w http.ResponseWriter, r *http.Request) {
	provider := gitforge.NormalizeProvider(r.URL.Query().Get("provider"))
	q := r.URL.Query().Get("q")
	if provider == "" {
		http.Error(w, "provider required", http.StatusBadRequest)
		return
	}
	cred, err := h.loadCredential(provider)
	if err != nil || cred.TokenEncrypted == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<div class="text-sm text-amber-300 p-3">Not connected. <a class="underline" href="/settings">Connect in Settings</a>.</div>`))
		return
	}
	token, err := h.ensureFreshToken(r.Context(), cred)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	endpoint := cred.Endpoint
	if endpoint == "" {
		if app, err := h.appCredentials(provider); err == nil {
			endpoint = app.Endpoint
		}
	}
	repos, err := gitforge.ListRepos(r.Context(), provider, endpoint, token, q)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<div class="text-sm text-red-300 p-3">Failed to list repos: %s</div>`, htmlEscape(err.Error()))
		return
	}
	data := pageData{
		GitRepos:     repos,
		GitProvider:   provider,
		TemplateQuery: q,
		NodeMode:      true,
		GitCredID:     cred.ID,
	}
	h.render(w, "node_git_repos", data)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
