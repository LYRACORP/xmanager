package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/gitforge"
	"github.com/lyracorp/xmanager/internal/storage"
)

func randomClientPair() (id, secret string) {
	a := make([]byte, 16)
	b := make([]byte, 24)
	_, _ = rand.Read(a)
	_, _ = rand.Read(b)
	return hex.EncodeToString(a), hex.EncodeToString(b)
}

// ensureGiteaOAuthApp makes sure a Gitea OAuth application exists for this panel.
func (h *handler) ensureGiteaOAuthApp(r *http.Request) error {
	endpoint := h.defaultGiteaEndpoint()
	if app, err := h.loadOAuthApp(gitforge.ProviderGitea); err == nil && app.ClientID != "" {
		if app.Endpoint == "" {
			app.Endpoint = endpoint
			_ = h.opts.DB.Save(app).Error
		}
		return nil
	}
	if builtin := gitforge.BuiltinCredentials(gitforge.ProviderGitea); builtin.ClientID != "" {
		return h.saveGiteaOAuthApp(builtin.ClientID, builtin.ClientSecret, firstNonEmpty(builtin.Endpoint, endpoint))
	}

	redirect := h.oauthRedirectURI(r, gitforge.ProviderGitea)
	token := strings.TrimSpace(os.Getenv("XMANAGER_GITEA_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("XMANAGER_GITEA_ADMIN_TOKEN"))
	}
	if token != "" {
		id, secret, err := createGiteaOAuthApp(endpoint, token, redirect)
		if err != nil {
			return err
		}
		return h.saveGiteaOAuthApp(id, secret, endpoint)
	}

	if id, secret, err := h.createGiteaOAuthAppViaDocker(redirect, endpoint); err == nil {
		return h.saveGiteaOAuthApp(id, secret, endpoint)
	}

	// Last resort: stash generated IDs for the admin to paste into Gitea once.
	clientID, clientSecret := randomClientPair()
	_ = h.saveGiteaOAuthApp(clientID, clientSecret, endpoint)
	return fmt.Errorf("add OAuth2 app in Gitea (Settings → Applications) with Client ID %s, matching secret from Advanced, redirect %s — or set XMANAGER_GITEA_TOKEN and Connect again", clientID, redirect)
}

func (h *handler) saveGiteaOAuthApp(clientID, clientSecret, endpoint string) error {
	enc := ""
	if clientSecret != "" {
		var err error
		enc, err = config.Encrypt(clientSecret)
		if err != nil {
			return err
		}
	}
	var app storage.GitOAuthApp
	err := h.opts.DB.Where("provider = ?", gitforge.ProviderGitea).First(&app).Error
	if err != nil {
		app = storage.GitOAuthApp{Provider: gitforge.ProviderGitea, Enabled: true}
	}
	app.ClientID = clientID
	if enc != "" {
		app.ClientSecretEncrypted = enc
	}
	app.Endpoint = endpoint
	if app.ID == 0 {
		return h.opts.DB.Create(&app).Error
	}
	return h.opts.DB.Save(&app).Error
}

func createGiteaOAuthApp(endpoint, token, redirect string) (clientID, clientSecret string, err error) {
	endpoint = strings.TrimRight(endpoint, "/")
	payload, _ := json.Marshal(map[string]interface{}{
		"name":                "XManager",
		"redirect_uris":       []string{redirect},
		"confidential_client": true,
	})
	req, err := http.NewRequest(http.MethodPost, endpoint+"/api/v1/user/applications/oauth2", strings.NewReader(string(payload)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "token "+token)
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("gitea oauth create HTTP %d: %s", res.StatusCode, string(body))
	}
	var created struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", "", err
	}
	if created.ClientID == "" {
		return "", "", fmt.Errorf("gitea oauth create: empty client_id")
	}
	return created.ClientID, created.ClientSecret, nil
}

func (h *handler) createGiteaOAuthAppViaDocker(redirect, endpoint string) (string, string, error) {
	exec := h.localExec()
	if exec == nil {
		return "", "", fmt.Errorf("no local executor")
	}
	out := exec.RunQuiet(`docker exec gitea su-exec git gitea admin user generate-access-token --username admin --token-name xmanager-oauth --scopes all 2>/dev/null | tail -1`)
	token := strings.TrimSpace(out)
	if token == "" || strings.Contains(strings.ToLower(token), "error") {
		return "", "", fmt.Errorf("could not generate gitea admin token")
	}
	return createGiteaOAuthApp(endpoint, token, redirect)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
