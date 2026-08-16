package mailinbox

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	ModeStalwart = "stalwart"
	ModeMiaB     = "mailinabox"
)

// Config is persisted in ServiceInstance.ConfigJSON.
type Config struct {
	SMTPPort      string `json:"smtp_port"`
	IMAPPort      string `json:"imap_port"`
	HTTPSPort     string `json:"https_port"`
	Hostname      string `json:"hostname"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
	APIBase       string `json:"api_base"`     // e.g. http://127.0.0.1:8085 or https://box/admin
	WebmailURL    string `json:"webmail_url"` // optional override; else derived from APIBase/Hostname
	Mode          string `json:"mode"`         // stalwart | mailinabox
}

// MailUser is one mailbox on a Mail-in-a-Box domain.
type MailUser struct {
	Email      string   `json:"email"`
	Status     string   `json:"status"`
	Privileges []string `json:"privileges"`
}

// MailDomain groups users under one mail domain (MiaB list response shape).
type MailDomain struct {
	Domain string     `json:"domain"`
	Users  []MailUser `json:"users"`
}

func DefaultConfig() Config {
	pass := randomHex(12)
	return Config{
		SMTPPort:      "25",
		IMAPPort:      "143",
		HTTPSPort:     "8085",
		Hostname:      "mail.example.com",
		AdminUser:     "admin",
		AdminPassword: pass,
		Mode:          ModeStalwart,
	}
}

func ParseConfig(raw string) Config {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || strings.Contains(raw, `"user_disabled":true`) {
		return DefaultConfig()
	}
	cfg := Config{}
	_ = json.Unmarshal([]byte(raw), &cfg)
	if cfg.SMTPPort == "" {
		cfg.SMTPPort = "25"
	}
	if cfg.IMAPPort == "" {
		cfg.IMAPPort = "143"
	}
	if cfg.HTTPSPort == "" {
		cfg.HTTPSPort = "8085"
	}
	if cfg.Hostname == "" {
		cfg.Hostname = "mail.example.com"
	}
	if cfg.AdminUser == "" {
		cfg.AdminUser = "admin"
	}
	// Do not invent a password here — Enable() generates and persists one.
	if cfg.Mode == "" {
		cfg.Mode = ModeStalwart
	}
	return cfg
}

func (c Config) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

func (c Config) BaseURL() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	if c.Mode == ModeMiaB {
		return "https://127.0.0.1/admin"
	}
	return fmt.Sprintf("http://127.0.0.1:%s", c.HTTPSPort)
}

// AdminURL is the mail admin panel (MiaB /admin or Stalwart base).
func (c Config) AdminURL() string {
	return c.BaseURL()
}

// ResolvedWebmailURL returns Roundcube (/mail) or an explicit override.
func (c Config) ResolvedWebmailURL() string {
	if u := strings.TrimSpace(c.WebmailURL); u != "" {
		return strings.TrimRight(u, "/")
	}
	base := c.BaseURL()
	// MiaB: https://box.example/admin → https://box.example/mail
	if strings.HasSuffix(base, "/admin") {
		return strings.TrimSuffix(base, "/admin") + "/mail"
	}
	host := strings.TrimSpace(c.Hostname)
	if host != "" && host != "mail.example.com" {
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = "https://" + host
		}
		return strings.TrimRight(host, "/") + "/mail"
	}
	if u, err := url.Parse(base); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host + "/mail"
	}
	return ""
}

func LoadConfig(db *gorm.DB, serverID uint) Config {
	if db == nil {
		return DefaultConfig()
	}
	var inst struct {
		ConfigJSON string
	}
	err := db.Table("service_instances").
		Select("config_json").
		Where("server_id = ? AND service_type = ?", serverID, serviceType).
		First(&inst).Error
	if err != nil {
		return DefaultConfig()
	}
	return ParseConfig(inst.ConfigJSON)
}

// Client provisions domains/mailboxes via Stalwart or Mail-in-a-Box APIs.
type Client struct {
	Cfg  Config
	HTTP *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		Cfg: cfg,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // local/self-signed mail admin
			},
		},
	}
}

func (c *Client) authHeader() string {
	token := base64.StdEncoding.EncodeToString([]byte(c.Cfg.AdminUser + ":" + c.Cfg.AdminPassword))
	return "Basic " + token
}

func (c *Client) doJSON(method, path string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Cfg.BaseURL()+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return data, res.StatusCode, fmt.Errorf("mail %s %s: %s (%s)", method, path, res.Status, truncate(string(data), 200))
	}
	return data, res.StatusCode, nil
}

func (c *Client) doForm(method, path string, form url.Values) ([]byte, int, error) {
	req, err := http.NewRequest(method, c.Cfg.BaseURL()+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return data, res.StatusCode, fmt.Errorf("mail %s %s: %s (%s)", method, path, res.Status, truncate(string(data), 200))
	}
	return data, res.StatusCode, nil
}

// EnsureDomain registers a mail domain (Stalwart principal or MiaB zone via first mailbox domain).
func (c *Client) EnsureDomain(domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return fmt.Errorf("empty domain")
	}
	if c.Cfg.Mode == ModeMiaB {
		// MiaB creates domains implicitly when adding users; no-op.
		return nil
	}
	payload := map[string]any{
		"type":                "domain",
		"name":                domain,
		"quota":               0,
		"secrets":             []string{},
		"emails":              []string{},
		"urls":                []string{},
		"memberOf":            []string{},
		"roles":               []string{},
		"lists":               []string{},
		"members":             []string{},
		"enabledPermissions":  []string{},
		"disabledPermissions": []string{},
		"externalMembers":     []string{},
	}
	_, code, err := c.doJSON("POST", "/api/principal", payload)
	if err != nil {
		// try deploy endpoint (Thunderbird client)
		_, _, err2 := c.doJSON("POST", "/api/principal/deploy", payload)
		if err2 == nil {
			return nil
		}
		if code == http.StatusConflict || strings.Contains(strings.ToLower(err.Error()), "already") {
			return nil
		}
		return fmt.Errorf("%v; deploy: %w", err, err2)
	}
	return nil
}

// CreateMailbox provisions local@domain with password.
func (c *Client) CreateMailbox(local, domain, password string) error {
	local = strings.ToLower(strings.TrimSpace(local))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if local == "" || domain == "" {
		return fmt.Errorf("local part and domain required")
	}
	if password == "" {
		return fmt.Errorf("password required")
	}
	addr := local + "@" + domain

	if c.Cfg.Mode == ModeMiaB {
		form := url.Values{}
		form.Set("email", addr)
		form.Set("password", password)
		form.Set("privileges", "")
		_, _, err := c.doForm("POST", "/mail/users/add", form)
		return err
	}

	if err := c.EnsureDomain(domain); err != nil {
		// continue — domain may already exist under a different error shape
		_ = err
	}
	payload := map[string]any{
		"type":                "individual",
		"name":                local,
		"secrets":             []string{password},
		"emails":              []string{addr},
		"urls":                []string{},
		"memberOf":            []string{},
		"roles":               []string{"user"},
		"lists":               []string{},
		"members":             []string{},
		"enabledPermissions":  []string{},
		"disabledPermissions": []string{},
		"externalMembers":     []string{},
		"quota":               0,
	}
	_, code, err := c.doJSON("POST", "/api/principal", payload)
	if err != nil {
		_, _, err2 := c.doJSON("POST", "/api/principal/deploy", payload)
		if err2 == nil {
			return nil
		}
		if code == http.StatusConflict || strings.Contains(strings.ToLower(err.Error()), "already") {
			return nil
		}
		return fmt.Errorf("%v; deploy: %w", err, err2)
	}
	return nil
}

// DeleteMailbox removes a mailbox/user.
func (c *Client) DeleteMailbox(address string) error {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return fmt.Errorf("empty address")
	}
	if c.Cfg.Mode == ModeMiaB {
		form := url.Values{}
		form.Set("email", address)
		_, _, err := c.doForm("POST", "/mail/users/remove", form)
		return err
	}
	local := address
	if i := strings.Index(address, "@"); i >= 0 {
		local = address[:i]
	}
	_, _, err := c.doJSON("DELETE", "/api/principal/"+url.PathEscape(local), nil)
	return err
}

// ListUsers returns mail users grouped by domain (Mail-in-a-Box only).
func (c *Client) ListUsers() ([]MailDomain, error) {
	if c.Cfg.Mode != ModeMiaB {
		return nil, fmt.Errorf("list requires mailinabox mode")
	}
	data, _, err := c.doJSON("GET", "/mail/users?format=json", nil)
	if err != nil {
		return nil, err
	}
	var domains []MailDomain
	if err := json.Unmarshal(data, &domains); err != nil {
		return nil, fmt.Errorf("decode mail users: %w", err)
	}
	for i := range domains {
		domains[i].Domain = strings.ToLower(strings.TrimSpace(domains[i].Domain))
		for j := range domains[i].Users {
			domains[i].Users[j].Email = strings.ToLower(strings.TrimSpace(domains[i].Users[j].Email))
			if domains[i].Users[j].Status == "" {
				domains[i].Users[j].Status = "active"
			}
		}
	}
	return domains, nil
}

// ListDomains returns hosted mail domain names (Mail-in-a-Box only).
func (c *Client) ListDomains() ([]string, error) {
	if c.Cfg.Mode != ModeMiaB {
		return nil, fmt.Errorf("list requires mailinabox mode")
	}
	req, err := http.NewRequest("GET", c.Cfg.BaseURL()+"/mail/domains", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "text/plain, text/html, application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		// Fall back to domains from user list.
		grouped, err2 := c.ListUsers()
		if err2 != nil {
			return nil, fmt.Errorf("mail domains: %s", res.Status)
		}
		out := make([]string, 0, len(grouped))
		for _, d := range grouped {
			if d.Domain != "" {
				out = append(out, d.Domain)
			}
		}
		return out, nil
	}
	raw := strings.TrimSpace(string(data))
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "<") {
			continue
		}
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		grouped, err2 := c.ListUsers()
		if err2 == nil {
			for _, d := range grouped {
				if d.Domain != "" && !seen[d.Domain] {
					seen[d.Domain] = true
					out = append(out, d.Domain)
				}
			}
		}
	}
	return out, nil
}

// Ping checks admin API reachability (best-effort).
func (c *Client) Ping() error {
	paths := []string{"/api/principal?types=domain&page=0&limit=1", "/api/principal", "/"}
	if c.Cfg.Mode == ModeMiaB {
		paths = []string{"/mail/users?format=json", "/admin/login"}
	}
	var last error
	for _, path := range paths {
		req, err := http.NewRequest("GET", c.Cfg.BaseURL()+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", c.authHeader())
		req.Header.Set("Accept", "application/json")
		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		code := res.StatusCode
		_ = res.Body.Close()
		// 401/403 means the server is up but auth differs — treat as reachable.
		if code < 400 || code == 401 || code == 403 {
			return nil
		}
		last = fmt.Errorf("mail ping: %s", res.Status)
		if code != 404 {
			return last
		}
	}
	if last == nil {
		last = fmt.Errorf("mail ping: unreachable")
	}
	return last
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:min(n*2, 24)]
	}
	return hex.EncodeToString(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
