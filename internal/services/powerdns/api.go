package powerdns

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Config holds runtime API settings loaded from ServiceInstance.ConfigJSON.
type Config struct {
	DNSPort    string `json:"dns_port"`
	APIPort    string `json:"api_port"`
	APIKey     string `json:"api_key"`
	DBPassword string `json:"db_password"`
	BaseURL    string `json:"base_url"` // optional override, default http://127.0.0.1:{api_port}
}

func DefaultConfig() Config {
	return Config{
		DNSPort:    "53",
		APIPort:    "8081",
		APIKey:     randomHex(16),
		DBPassword: randomHex(12),
	}
}

func ParseConfig(raw string) Config {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || strings.Contains(raw, `"user_disabled":true`) {
		return DefaultConfig()
	}
	cfg := Config{}
	_ = json.Unmarshal([]byte(raw), &cfg)
	if cfg.DNSPort == "" {
		cfg.DNSPort = "53"
	}
	if cfg.APIPort == "" {
		cfg.APIPort = "8081"
	}
	// Empty API key is filled during Enable(); do not invent on every LoadConfig.
	return cfg
}

func (c Config) URL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return "http://127.0.0.1:" + c.APIPort
}

func (c Config) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// LoadConfig reads persisted PowerDNS config for a server.
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

// Client talks to the PowerDNS Authoritative HTTP API.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		BaseURL: cfg.URL(),
		APIKey:  cfg.APIKey,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(method, path string, body any) ([]byte, int, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-API-Key", c.APIKey)
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
		return data, res.StatusCode, fmt.Errorf("powerdns %s %s: %s (%s)", method, path, res.Status, strings.TrimSpace(string(data)))
	}
	return data, res.StatusCode, nil
}

func fqdn(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	return name
}

// EnsureZone creates a Native zone if missing and upserts A (+ optional MX) records.
func (c *Client) EnsureZone(domain, publicIP string, mailMX string) error {
	zone := fqdn(domain)
	if zone == "" {
		return fmt.Errorf("empty domain")
	}
	ns1 := "ns1." + zone

	_, code, err := c.do("GET", "/api/v1/servers/localhost/zones/"+zone, nil)
	exists := err == nil && code < 400
	if !exists {
		payload := map[string]any{
			"name":        zone,
			"kind":        "Native",
			"masters":     []string{},
			"nameservers": []string{ns1},
		}
		if _, _, err := c.do("POST", "/api/v1/servers/localhost/zones", payload); err != nil {
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "409") && !strings.Contains(msg, "already") {
				return err
			}
		}
	}

	var rrsets []map[string]any
	if publicIP != "" {
		rrsets = append(rrsets, map[string]any{
			"name":       zone,
			"type":       "A",
			"ttl":        300,
			"changetype": "REPLACE",
			"records":    []map[string]any{{"content": publicIP, "disabled": false}},
		})
		rrsets = append(rrsets, map[string]any{
			"name":       "www." + zone,
			"type":       "A",
			"ttl":        300,
			"changetype": "REPLACE",
			"records":    []map[string]any{{"content": publicIP, "disabled": false}},
		})
		rrsets = append(rrsets, map[string]any{
			"name":       ns1,
			"type":       "A",
			"ttl":        300,
			"changetype": "REPLACE",
			"records":    []map[string]any{{"content": publicIP, "disabled": false}},
		})
	}
	if mailMX != "" {
		mxHost := fqdn(mailMX)
		rrsets = append(rrsets, map[string]any{
			"name":       zone,
			"type":       "MX",
			"ttl":        300,
			"changetype": "REPLACE",
			"records":    []map[string]any{{"content": "10 " + mxHost, "disabled": false}},
		})
		rrsets = append(rrsets, map[string]any{
			"name":       zone,
			"type":       "TXT",
			"ttl":        300,
			"changetype": "REPLACE",
			"records":    []map[string]any{{"content": `"v=spf1 mx a -all"`, "disabled": false}},
		})
	}
	if len(rrsets) == 0 {
		return nil
	}
	_, _, err = c.do("PATCH", "/api/v1/servers/localhost/zones/"+zone, map[string]any{"rrsets": rrsets})
	return err
}

// Ping checks API reachability.
func (c *Client) Ping() error {
	_, _, err := c.do("GET", "/api/v1/servers/localhost", nil)
	return err
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:n*2]
	}
	return hex.EncodeToString(b)
}
