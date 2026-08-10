package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
	Next     string
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
	CanConnect   bool
	ConnectMode  string // device | redirect | need_https | need_client
}

type devicePending struct {
	Provider   string
	DeviceCode string
	Interval   int
	UserID     uint
	App        gitforge.AppCredentials
	Created    time.Time
	Expires    time.Time
	Next       string
}

type deviceStateStore struct {
	mu   sync.Mutex
	data map[string]devicePending
}

func newDeviceStateStore() *deviceStateStore {
	return &deviceStateStore{data: make(map[string]devicePending)}
}

func (s *deviceStateStore) put(id string, p devicePending) {
	s.mu.Lock()
	s.data[id] = p
	s.mu.Unlock()
}

func (s *deviceStateStore) get(id string) (devicePending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[id]
	return p, ok
}

func (s *deviceStateStore) delete(id string) {
	s.mu.Lock()
	delete(s.data, id)
	s.mu.Unlock()
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
	provider = gitforge.NormalizeProvider(provider)
	base := gitforge.BuiltinCredentials(provider)
	if provider == gitforge.ProviderGitea && base.Endpoint == "" {
		base.Endpoint = h.defaultGiteaEndpoint()
	}
	if app, err := h.loadOAuthApp(provider); err == nil {
		secret := ""
		if app.ClientSecretEncrypted != "" {
			if s, err := config.Decrypt(app.ClientSecretEncrypted); err == nil {
				secret = s
			}
		}
		base = gitforge.MergeCredentials(base, gitforge.AppCredentials{
			ClientID:     app.ClientID,
			ClientSecret: secret,
			Endpoint:     app.Endpoint,
		})
	}
	if provider == gitforge.ProviderGitea && base.Endpoint == "" {
		base.Endpoint = h.defaultGiteaEndpoint()
	}
	return base, nil
}

func (h *handler) requireAppCredentials(provider string) (gitforge.AppCredentials, error) {
	app, err := h.appCredentials(provider)
	if err != nil {
		return app, err
	}
	if strings.TrimSpace(app.ClientID) == "" {
		return app, fmt.Errorf("no OAuth client for %s — set XMANAGER_%s_OAUTH_CLIENT_ID or Advanced custom app", provider, strings.ToUpper(provider))
	}
	return app, nil
}

func (h *handler) publicURLIsHTTPS() bool {
	if h.opts.Config == nil {
		return false
	}
	u := strings.ToLower(strings.TrimSpace(h.opts.Config.Web.PublicURL))
	return strings.HasPrefix(u, "https://")
}

func (h *handler) connectMode(provider string, app gitforge.AppCredentials) string {
	provider = gitforge.NormalizeProvider(provider)
	if app.ClientID == "" {
		return "need_client"
	}
	// Redirect flow works over plain HTTP too (GitHub/GitLab allow it).
	// Prefer it whenever a client secret is available — this is the
	// standard Vercel-style flow: browser → GitHub → callback → done.
	if strings.TrimSpace(app.ClientSecret) != "" {
		return "redirect"
	}
	// No client secret: fall back to device flow (no redirect URI needed).
	if gitforge.SupportsDeviceFlow(provider) {
		return "device"
	}
	return "need_https"
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
		app, _ := h.appCredentials(p.ID)
		v.ClientID = app.ClientID
		v.Endpoint = app.Endpoint
		v.HasSecret = app.ClientSecret != ""
		if p.ID == gitforge.ProviderGitea && v.Endpoint == "" {
			v.Endpoint = h.defaultGiteaEndpoint()
		}
		v.ConnectMode = h.connectMode(p.ID, app)
		v.CanConnect = true // always show Connect; handler explains missing client / HTTPS
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

func safeNextPath(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	return raw
}

func appendFlash(path, msg string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "flash=" + urlQueryEscape(msg)
}

func (h *handler) oauthFailRedirect(w http.ResponseWriter, r *http.Request, msg string) {
	next := safeNextPath(r.URL.Query().Get("next"), "/projects/new?type=git")
	http.Redirect(w, r, appendFlash(next, msg), http.StatusSeeOther)
}

func (h *handler) getOAuthGitConnect(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	next := safeNextPath(r.URL.Query().Get("next"), "/projects/new?type=git&provider="+provider)
	if provider == "" {
		h.oauthFailRedirect(w, r, "unknown provider")
		return
	}
	if provider == gitforge.ProviderGitea {
		// Best-effort local OAuth app; if it fails we still offer token bootstrap.
		_ = h.ensureGiteaOAuthApp(r)
	}
	app, _ := h.appCredentials(provider)
	mode := h.connectMode(provider, app)
	switch mode {
	case "need_client", "need_https":
		// No OAuth app (or Bitbucket on HTTP): open forge in browser + paste token.
		h.renderTokenBootstrap(w, r, sess, provider, app.Endpoint, next)
		return
	case "device":
		h.startDeviceConnect(w, r, sess, provider, app, next)
		return
	}

	state := randomToken()
	if h.oauthStates == nil {
		h.oauthStates = newOAuthStateStore()
	}
	h.oauthStates.put(state, oauthPending{Provider: provider, UserID: sess.UserID, Created: time.Now(), Next: next})
	authURL, err := gitforge.AuthorizeURL(provider, app, h.oauthRedirectURI(r, provider), state)
	if err != nil {
		h.oauthFailRedirect(w, r, err.Error())
		return
	}
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func (h *handler) renderTokenBootstrap(w http.ResponseWriter, r *http.Request, sess *session, provider, endpoint, next string) {
	data := h.basePage(sess, "Connect "+provider)
	data.ActiveNav = "projects"
	data.BootstrapProvider = provider
	data.BootstrapTokenURL = gitforge.TokenCreateURL(provider, endpoint)
	data.BootstrapHint = gitforge.TokenHint(provider)
	data.BootstrapNext = next
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	h.render(w, "settings_git_token", data)
}

func (h *handler) postOAuthGitToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	next := safeNextPath(r.FormValue("next"), "/projects/new?type=git&provider="+provider)
	token := strings.TrimSpace(r.FormValue("token"))
	username := strings.TrimSpace(r.FormValue("username"))
	if provider == "" || token == "" {
		http.Redirect(w, r, appendFlash("/oauth/git/"+provider+"/connect?next="+url.QueryEscape(next), "token required"), http.StatusSeeOther)
		return
	}
	// Bitbucket app passwords often need username:token basic auth; store as token, username separate.
	access := token
	if provider == gitforge.ProviderBitbucket && username != "" && !strings.Contains(token, ":") {
		access = username + ":" + token
	}
	app, _ := h.appCredentials(provider)
	endpoint := app.Endpoint
	if provider == gitforge.ProviderGitea && endpoint == "" {
		endpoint = h.defaultGiteaEndpoint()
	}
	acct, err := gitforge.FetchAccount(r.Context(), provider, endpoint, access)
	if err != nil {
		// Bitbucket bearer may fail for app passwords — try as-is message
		http.Redirect(w, r, "/oauth/git/"+provider+"/connect?next="+url.QueryEscape(next)+"&flash="+urlQueryEscape("invalid token: "+err.Error()), http.StatusSeeOther)
		return
	}
	enc, err := config.Encrypt(access)
	if err != nil {
		http.Redirect(w, r, appendFlash(next, err.Error()), http.StatusSeeOther)
		return
	}
	cred := storage.GitCredential{
		Provider:       provider,
		Username:       acct.Login,
		AccountLogin:   acct.Login,
		TokenEncrypted: enc,
		Endpoint:       endpoint,
		Scopes:         "token",
	}
	if username != "" && cred.Username == "" {
		cred.Username = username
		cred.AccountLogin = username
	}
	var existing storage.GitCredential
	if err := h.opts.DB.Where("provider = ?", provider).Order("id desc").First(&existing).Error; err == nil {
		cred.ID = existing.ID
		cred.CreatedAt = existing.CreatedAt
		_ = h.opts.DB.Save(&cred).Error
	} else {
		_ = h.opts.DB.Create(&cred).Error
	}
	http.Redirect(w, r, appendFlash(next, "Connected "+provider+" as "+cred.AccountLogin), http.StatusSeeOther)
}

func (h *handler) startDeviceConnect(w http.ResponseWriter, r *http.Request, sess *session, provider string, app gitforge.AppCredentials, next string) {
	dc, err := gitforge.RequestDeviceCode(r.Context(), provider, app)
	if err != nil {
		http.Redirect(w, r, appendFlash(next, "device start: "+err.Error()), http.StatusSeeOther)
		return
	}
	id := randomToken()
	if h.deviceStates == nil {
		h.deviceStates = newDeviceStateStore()
	}
	exp := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	if dc.ExpiresIn <= 0 {
		exp = time.Now().Add(15 * time.Minute)
	}
	h.deviceStates.put(id, devicePending{
		Provider:   provider,
		DeviceCode: dc.DeviceCode,
		Interval:   dc.Interval,
		UserID:     sess.UserID,
		App:        app,
		Created:    time.Now(),
		Expires:    exp,
		Next:       next,
	})
	verify := dc.VerificationURIComplete
	if verify == "" {
		verify = dc.VerificationURI
	}
	data := h.basePage(sess, "Connect "+provider)
	data.ActiveNav = "projects"
	data.DeviceID = id
	data.DeviceUserCode = dc.UserCode
	data.DeviceVerifyURL = verify
	data.DeviceProvider = provider
	data.DeviceInterval = dc.Interval
	data.PublicURL = next // reuse for cancel link
	if data.DeviceInterval <= 0 {
		data.DeviceInterval = 5
	}
	h.render(w, "settings_git_device", data)
}

func (h *handler) getDevicePoll(w http.ResponseWriter, r *http.Request) {
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	id := r.URL.Query().Get("id")
	if provider == "" || id == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if h.deviceStates == nil {
		http.Error(w, "expired", http.StatusGone)
		return
	}
	pending, ok := h.deviceStates.get(id)
	if !ok || pending.Provider != provider {
		http.Error(w, "expired", http.StatusGone)
		return
	}
	if time.Now().After(pending.Expires) {
		h.deviceStates.delete(id)
		retry := "/oauth/git/" + provider + "/connect?next=" + url.QueryEscape(safeNextPath(pending.Next, "/projects/new?type=git&provider="+provider))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<p class="text-amber-300 text-sm">Code expired. <a class="underline" href="%s">Try again</a></p>`, htmlEscape(retry))
		return
	}
	ts, err := gitforge.PollDeviceToken(r.Context(), provider, pending.App, pending.DeviceCode)
	if err != nil {
		if errors.Is(err, gitforge.ErrDevicePending) || errors.Is(err, gitforge.ErrDeviceSlowDown) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<p class="text-panel-muted text-sm">Waiting for approval…</p>`))
			return
		}
		if errors.Is(err, gitforge.ErrDeviceExpired) || errors.Is(err, gitforge.ErrDeviceDenied) {
			h.deviceStates.delete(id)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<p class="text-red-300 text-sm">%s</p>`, htmlEscape(err.Error()))
		return
	}
	h.deviceStates.delete(id)
	if err := h.persistOAuthTokens(provider, pending.App, ts); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<p class="text-red-300 text-sm">%s</p>`, htmlEscape(err.Error()))
		return
	}
	dest := safeNextPath(pending.Next, "/projects/new?type=git&provider="+provider)
	w.Header().Set("HX-Redirect", appendFlash(dest, "Connected "+provider))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<p class="text-emerald-400 text-sm">Connected! Redirecting…</p>`))
}

func (h *handler) persistOAuthTokens(provider string, app gitforge.AppCredentials, ts *gitforge.TokenSet) error {
	ctx := context.Background()
	acct, err := gitforge.FetchAccount(ctx, provider, app.Endpoint, ts.AccessToken)
	if err != nil {
		return fmt.Errorf("fetch user: %w", err)
	}
	enc, err := config.Encrypt(ts.AccessToken)
	if err != nil {
		return err
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
		return h.opts.DB.Save(&cred).Error
	}
	return h.opts.DB.Create(&cred).Error
}

func (h *handler) getOAuthGitCallback(w http.ResponseWriter, r *http.Request) {
	provider := gitforge.NormalizeProvider(r.PathValue("provider"))
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	fallback := "/projects/new?type=git&provider=" + provider
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		http.Redirect(w, r, appendFlash(fallback, "oauth: "+errMsg), http.StatusSeeOther)
		return
	}
	if provider == "" || code == "" || state == "" {
		http.Redirect(w, r, appendFlash(fallback, "invalid oauth callback"), http.StatusSeeOther)
		return
	}
	if h.oauthStates == nil {
		http.Redirect(w, r, appendFlash(fallback, "oauth state expired"), http.StatusSeeOther)
		return
	}
	pending, ok := h.oauthStates.take(state)
	if !ok || pending.Provider != provider {
		http.Redirect(w, r, appendFlash(fallback, "oauth state mismatch"), http.StatusSeeOther)
		return
	}
	next := safeNextPath(pending.Next, fallback)
	app, err := h.requireAppCredentials(provider)
	if err != nil {
		http.Redirect(w, r, appendFlash(next, err.Error()), http.StatusSeeOther)
		return
	}
	ts, err := gitforge.ExchangeCode(r.Context(), provider, app, h.oauthRedirectURI(r, provider), code)
	if err != nil {
		http.Redirect(w, r, appendFlash(next, "token exchange: "+err.Error()), http.StatusSeeOther)
		return
	}
	if err := h.persistOAuthTokens(provider, app, ts); err != nil {
		http.Redirect(w, r, appendFlash(next, err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, appendFlash(next, "Connected "+provider), http.StatusSeeOther)
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

// getAPIGitDeviceStart starts an OAuth device flow and returns JSON so the
// browser can open the verification URL in a new tab without a popup blocker.
// GET /api/git/device/start?provider=github
func (h *handler) getAPIGitDeviceStart(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	provider := gitforge.NormalizeProvider(r.URL.Query().Get("provider"))
	next := safeNextPath(r.URL.Query().Get("next"), "/projects/new?type=git&provider="+provider)
	if provider == "" {
		http.Error(w, `{"error":"provider required"}`, http.StatusBadRequest)
		return
	}
	if !gitforge.SupportsDeviceFlow(provider) {
		http.Error(w, `{"error":"device flow not supported"}`, http.StatusBadRequest)
		return
	}
	app, err := h.appCredentials(provider)
	if err != nil || app.ClientID == "" {
		http.Error(w, `{"error":"no OAuth client configured"}`, http.StatusBadRequest)
		return
	}
	dc, err := gitforge.RequestDeviceCode(r.Context(), provider, app)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, `{"error":%q}`, err.Error())
		return
	}
	id := randomToken()
	if h.deviceStates == nil {
		h.deviceStates = newDeviceStateStore()
	}
	exp := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	if dc.ExpiresIn <= 0 {
		exp = time.Now().Add(15 * time.Minute)
	}
	interval := dc.Interval
	if interval <= 0 {
		interval = 5
	}
	h.deviceStates.put(id, devicePending{
		Provider:   provider,
		DeviceCode: dc.DeviceCode,
		Interval:   interval,
		UserID:     sess.UserID,
		App:        app,
		Created:    time.Now(),
		Expires:    exp,
		Next:       next,
	})
	verifyURL := dc.VerificationURIComplete
	if verifyURL == "" {
		verifyURL = dc.VerificationURI
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"id":%q,"user_code":%q,"verify_url":%q,"interval":%d}`,
		id, dc.UserCode, verifyURL, interval)
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
