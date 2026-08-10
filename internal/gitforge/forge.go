package gitforge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider identifiers.
const (
	ProviderGitHub    = "github"
	ProviderGitLab    = "gitlab"
	ProviderBitbucket = "bitbucket"
	ProviderGitea     = "gitea"
)

// Repo is a forge repository suitable for project creation.
type Repo struct {
	Name          string
	FullName      string
	CloneURL      string
	Private       bool
	DefaultBranch string
}

// TokenSet is an OAuth token response.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds; 0 = unknown / non-expiring
	TokenType    string
	Scope        string
}

// Account is the authenticated user.
type Account struct {
	Login string
	Name  string
}

// AppCredentials is the OAuth application registered by the admin.
type AppCredentials struct {
	ClientID     string
	ClientSecret string
	Endpoint     string // base URL for gitea / self-hosted gitlab
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// NormalizeProvider returns a known provider id or empty.
func NormalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case ProviderGitHub, "gh":
		return ProviderGitHub
	case ProviderGitLab, "gl":
		return ProviderGitLab
	case ProviderBitbucket, "bb":
		return ProviderBitbucket
	case ProviderGitea, "gitea-local":
		return ProviderGitea
	default:
		return ""
	}
}

// DefaultScopes for each provider.
func DefaultScopes(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderGitHub:
		return "repo read:user"
	case ProviderGitLab:
		return "read_api read_repository"
	case ProviderBitbucket:
		return "repository account"
	case ProviderGitea:
		return "read:repository,read:user"
	default:
		return ""
	}
}

func baseEndpoint(provider string, endpoint string) string {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	switch NormalizeProvider(provider) {
	case ProviderGitHub:
		if endpoint != "" {
			return endpoint // GitHub Enterprise or test server
		}
		return "https://github.com"
	case ProviderGitLab:
		if endpoint != "" {
			return endpoint
		}
		return "https://gitlab.com"
	case ProviderBitbucket:
		return "https://bitbucket.org"
	case ProviderGitea:
		if endpoint != "" {
			return endpoint
		}
		return "http://127.0.0.1:3000"
	default:
		return endpoint
	}
}

func apiBase(provider string, endpoint string) string {
	switch NormalizeProvider(provider) {
	case ProviderGitHub:
		if endpoint != "" && !strings.Contains(endpoint, "github.com") {
			// GHE often uses https://ghe.example.com/api/v3
			return strings.TrimRight(endpoint, "/") + "/api/v3"
		}
		return "https://api.github.com"
	case ProviderGitLab:
		return baseEndpoint(provider, endpoint) + "/api/v4"
	case ProviderBitbucket:
		return "https://api.bitbucket.org/2.0"
	case ProviderGitea:
		return baseEndpoint(provider, endpoint) + "/api/v1"
	default:
		return ""
	}
}

// AuthorizeURL builds the browser redirect for the authorization code flow.
func AuthorizeURL(provider string, app AppCredentials, redirectURI, state string) (string, error) {
	provider = NormalizeProvider(provider)
	if provider == "" {
		return "", fmt.Errorf("unknown provider")
	}
	if strings.TrimSpace(app.ClientID) == "" {
		return "", fmt.Errorf("oauth client id required")
	}
	base := baseEndpoint(provider, app.Endpoint)
	scopes := DefaultScopes(provider)
	q := url.Values{}
	q.Set("client_id", app.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")

	switch provider {
	case ProviderGitHub:
		q.Set("scope", scopes)
		return base + "/login/oauth/authorize?" + q.Encode(), nil
	case ProviderGitLab:
		q.Set("scope", scopes)
		return base + "/oauth/authorize?" + q.Encode(), nil
	case ProviderBitbucket:
		// Bitbucket uses scope as space-separated in query
		q.Set("scope", scopes)
		return base + "/site/oauth2/authorize?" + q.Encode(), nil
	case ProviderGitea:
		q.Set("scope", scopes)
		return base + "/login/oauth/authorize?" + q.Encode(), nil
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
}

// ExchangeCode swaps an authorization code for tokens.
func ExchangeCode(ctx context.Context, provider string, app AppCredentials, redirectURI, code string) (*TokenSet, error) {
	provider = NormalizeProvider(provider)
	base := baseEndpoint(provider, app.Endpoint)
	form := url.Values{}
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	var tokenURL string
	switch provider {
	case ProviderGitHub:
		tokenURL = base + "/login/oauth/access_token"
	case ProviderGitLab:
		tokenURL = base + "/oauth/token"
	case ProviderBitbucket:
		tokenURL = base + "/site/oauth2/access_token"
	case ProviderGitea:
		tokenURL = base + "/login/oauth/access_token"
	default:
		return nil, fmt.Errorf("unsupported provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if provider == ProviderBitbucket {
		req.SetBasicAuth(app.ClientID, app.ClientSecret)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}

	// GitHub may return form-encoded if Accept is ignored; prefer JSON.
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		vals, err2 := url.ParseQuery(string(body))
		if err2 != nil {
			return nil, fmt.Errorf("parsing token response: %w", err)
		}
		ts := &TokenSet{
			AccessToken:  vals.Get("access_token"),
			RefreshToken: vals.Get("refresh_token"),
			TokenType:    vals.Get("token_type"),
			Scope:        vals.Get("scope"),
		}
		if ts.AccessToken == "" {
			return nil, fmt.Errorf("no access_token in response")
		}
		return ts, nil
	}
	ts := &TokenSet{
		AccessToken:  strVal(raw["access_token"]),
		RefreshToken: strVal(raw["refresh_token"]),
		TokenType:    strVal(raw["token_type"]),
		Scope:        strVal(raw["scope"]),
	}
	switch v := raw["expires_in"].(type) {
	case float64:
		ts.ExpiresIn = int(v)
	case json.Number:
		n, _ := v.Int64()
		ts.ExpiresIn = int(n)
	}
	if ts.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response")
	}
	return ts, nil
}

// RefreshAccessToken refreshes an OAuth access token when supported.
func RefreshAccessToken(ctx context.Context, provider string, app AppCredentials, refreshToken string) (*TokenSet, error) {
	provider = NormalizeProvider(provider)
	if refreshToken == "" {
		return nil, fmt.Errorf("no refresh token")
	}
	base := baseEndpoint(provider, app.Endpoint)
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)

	var tokenURL string
	switch provider {
	case ProviderGitLab:
		tokenURL = base + "/oauth/token"
	case ProviderBitbucket:
		tokenURL = base + "/site/oauth2/access_token"
	case ProviderGitea:
		tokenURL = base + "/login/oauth/access_token"
	case ProviderGitHub:
		return nil, fmt.Errorf("github oauth apps do not support refresh")
	default:
		return nil, fmt.Errorf("unsupported provider")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if provider == ProviderBitbucket {
		req.SetBasicAuth(app.ClientID, app.ClientSecret)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("refresh HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	ts := &TokenSet{
		AccessToken:  strVal(raw["access_token"]),
		RefreshToken: strVal(raw["refresh_token"]),
		TokenType:    strVal(raw["token_type"]),
		Scope:        strVal(raw["scope"]),
	}
	if ts.RefreshToken == "" {
		ts.RefreshToken = refreshToken
	}
	switch v := raw["expires_in"].(type) {
	case float64:
		ts.ExpiresIn = int(v)
	}
	if ts.AccessToken == "" {
		return nil, fmt.Errorf("no access_token")
	}
	return ts, nil
}

// FetchAccount returns the authenticated user login.
func FetchAccount(ctx context.Context, provider, endpoint, accessToken string) (*Account, error) {
	provider = NormalizeProvider(provider)
	switch provider {
	case ProviderGitHub:
		var u struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		if err := apiGet(ctx, apiBase(provider, endpoint)+"/user", accessToken, provider, &u); err != nil {
			return nil, err
		}
		return &Account{Login: u.Login, Name: u.Name}, nil
	case ProviderGitLab:
		var u struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		}
		if err := apiGet(ctx, apiBase(provider, endpoint)+"/user", accessToken, provider, &u); err != nil {
			return nil, err
		}
		return &Account{Login: u.Username, Name: u.Name}, nil
	case ProviderBitbucket:
		var u struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
		}
		if err := apiGet(ctx, apiBase(provider, endpoint)+"/user", accessToken, provider, &u); err != nil {
			return nil, err
		}
		return &Account{Login: u.Username, Name: u.DisplayName}, nil
	case ProviderGitea:
		var u struct {
			Login    string `json:"login"`
			Username string `json:"username"`
			FullName string `json:"full_name"`
		}
		if err := apiGet(ctx, apiBase(provider, endpoint)+"/user", accessToken, provider, &u); err != nil {
			return nil, err
		}
		login := u.Login
		if login == "" {
			login = u.Username
		}
		return &Account{Login: login, Name: u.FullName}, nil
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

// ListRepos returns repositories visible to the authenticated user.
func ListRepos(ctx context.Context, provider, endpoint, accessToken, query string) ([]Repo, error) {
	provider = NormalizeProvider(provider)
	query = strings.ToLower(strings.TrimSpace(query))
	var repos []Repo
	var err error
	switch provider {
	case ProviderGitHub:
		repos, err = listGitHub(ctx, accessToken)
	case ProviderGitLab:
		repos, err = listGitLab(ctx, endpoint, accessToken)
	case ProviderBitbucket:
		repos, err = listBitbucket(ctx, accessToken)
	case ProviderGitea:
		repos, err = listGitea(ctx, endpoint, accessToken)
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
	if err != nil {
		return nil, err
	}
	if query == "" {
		return repos, nil
	}
	var filtered []Repo
	for _, r := range repos {
		hay := strings.ToLower(r.FullName + " " + r.Name + " " + r.CloneURL)
		if strings.Contains(hay, query) {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

// AuthenticatedCloneURL injects the OAuth token into an HTTPS clone URL.
func AuthenticatedCloneURL(provider, cloneURL, token string) (string, error) {
	provider = NormalizeProvider(provider)
	if token == "" {
		return cloneURL, nil
	}
	u, err := url.Parse(strings.TrimSpace(cloneURL))
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("only http(s) clone URLs supported for oauth")
	}
	switch provider {
	case ProviderGitHub, ProviderGitea:
		u.User = url.UserPassword("x-access-token", token)
	case ProviderGitLab:
		u.User = url.UserPassword("oauth2", token)
	case ProviderBitbucket:
		u.User = url.UserPassword("x-token-auth", token)
	default:
		u.User = url.UserPassword("oauth2", token)
	}
	return u.String(), nil
}

func listGitHub(ctx context.Context, token string) ([]Repo, error) {
	var raw []struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := apiGet(ctx, "https://api.github.com/user/repos?per_page=100&sort=updated", token, ProviderGitHub, &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{
			Name: r.Name, FullName: r.FullName, CloneURL: r.CloneURL,
			Private: r.Private, DefaultBranch: r.DefaultBranch,
		})
	}
	return out, nil
}

func listGitLab(ctx context.Context, endpoint, token string) ([]Repo, error) {
	var raw []struct {
		Name              string `json:"name"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		Visibility        string `json:"visibility"`
		DefaultBranch     string `json:"default_branch"`
	}
	urlStr := apiBase(ProviderGitLab, endpoint) + "/projects?membership=true&simple=true&per_page=100&order_by=last_activity_at"
	if err := apiGet(ctx, urlStr, token, ProviderGitLab, &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{
			Name: r.Name, FullName: r.PathWithNamespace, CloneURL: r.HTTPURLToRepo,
			Private: r.Visibility != "public", DefaultBranch: r.DefaultBranch,
		})
	}
	return out, nil
}

func listBitbucket(ctx context.Context, token string) ([]Repo, error) {
	var page struct {
		Values []struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			IsPrivate bool  `json:"is_private"`
			MainBranch *struct {
				Name string `json:"name"`
			} `json:"mainbranch"`
			Links struct {
				Clone []struct {
					Name string `json:"name"`
					Href string `json:"href"`
				} `json:"clone"`
			} `json:"links"`
		} `json:"values"`
	}
	if err := apiGet(ctx, "https://api.bitbucket.org/2.0/repositories?role=member&pagelen=100", token, ProviderBitbucket, &page); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(page.Values))
	for _, r := range page.Values {
		clone := ""
		for _, c := range r.Links.Clone {
			if c.Name == "https" {
				clone = c.Href
				break
			}
		}
		branch := "main"
		if r.MainBranch != nil && r.MainBranch.Name != "" {
			branch = r.MainBranch.Name
		}
		out = append(out, Repo{
			Name: r.Name, FullName: r.FullName, CloneURL: clone,
			Private: r.IsPrivate, DefaultBranch: branch,
		})
	}
	return out, nil
}

func listGitea(ctx context.Context, endpoint, token string) ([]Repo, error) {
	var raw []struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	urlStr := apiBase(ProviderGitea, endpoint) + "/user/repos?limit=100"
	if err := apiGet(ctx, urlStr, token, ProviderGitea, &raw); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(raw))
	for _, r := range raw {
		out = append(out, Repo{
			Name: r.Name, FullName: r.FullName, CloneURL: r.CloneURL,
			Private: r.Private, DefaultBranch: r.DefaultBranch,
		})
	}
	return out, nil
}

func apiGet(ctx context.Context, rawURL, token, provider string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xmanager-gitforge/1.0")
	switch NormalizeProvider(provider) {
	case ProviderBitbucket:
		req.Header.Set("Authorization", "Bearer "+token)
	default:
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("API HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
	}
	return json.Unmarshal(body, dest)
}

func strVal(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
