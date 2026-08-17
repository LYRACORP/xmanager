package powerdns

import (
	"bytes"
	"crypto/rand"
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

// Config holds runtime API settings loaded from ServiceInstance.ConfigJSON.
type Config struct {
	DNSPort string `json:"dns_port"`
	APIPort string `json:"api_port"`
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"` // optional override, default http://127.0.0.1:{api_port}
}

func DefaultConfig() Config {
	return Config{
		DNSPort: "53",
		APIPort: DefaultAPIPort,
		APIKey:  randomHex(16),
	}
}

// DefaultAPIPort is the host port published for the PowerDNS HTTP API.
// Kept off 8081 so it does not collide with Adminer (xm-adminer).
const DefaultAPIPort = "8082"

// ContainerAPIPort is the port PowerDNS listens on inside the container.
const ContainerAPIPort = "8081"

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
		cfg.APIPort = DefaultAPIPort
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

// Zone is a PowerDNS zone summary or detail.
type Zone struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	Serial     int     `json:"serial"`
	RRSets     []RRSet `json:"rrsets,omitempty"`
	RecordCount int    `json:"-"`
}

// RRSet is a resource record set within a zone.
type RRSet struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl"`
	Records []Record `json:"records"`
}

// Record is a single DNS record value.
type Record struct {
	Content  string `json:"content"`
	Disabled bool   `json:"disabled"`
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

func zonePath(domain string) string {
	return "/api/v1/servers/localhost/zones/" + url.PathEscape(fqdn(domain))
}

// EnsureZone creates a Native zone if missing and upserts A (+ optional MX) records.
// Optional nameservers (ns1, ns2, …) override the default ns1.<zone>.
func (c *Client) EnsureZone(domain, publicIP string, mailMX string, nameservers ...string) error {
	zone := fqdn(domain)
	if zone == "" {
		return fmt.Errorf("empty domain")
	}

	var nsList []string
	for _, ns := range nameservers {
		ns = strings.TrimSpace(strings.ToLower(ns))
		if ns == "" {
			continue
		}
		nsList = append(nsList, fqdn(ns))
	}
	if len(nsList) == 0 {
		nsList = []string{"ns1." + zone}
	}

	_, code, err := c.do("GET", zonePath(domain), nil)
	exists := err == nil && code < 400
	if !exists {
		payload := map[string]any{
			"name":        zone,
			"kind":        "Native",
			"masters":     []string{},
			"nameservers": nsList,
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
		// Glue A for each nameserver hostname under this zone.
		for _, ns := range nsList {
			if strings.HasSuffix(ns, "."+zone) || ns == zone {
				rrsets = append(rrsets, map[string]any{
					"name":       ns,
					"type":       "A",
					"ttl":        300,
					"changetype": "REPLACE",
					"records":    []map[string]any{{"content": publicIP, "disabled": false}},
				})
			}
		}
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
	_, _, err = c.do("PATCH", zonePath(domain), map[string]any{"rrsets": rrsets})
	return err
}

// ListZones returns all zones (without full RRSets).
func (c *Client) ListZones() ([]Zone, error) {
	data, _, err := c.do("GET", "/api/v1/servers/localhost/zones", nil)
	if err != nil {
		return nil, err
	}
	var zones []Zone
	if err := json.Unmarshal(data, &zones); err != nil {
		return nil, fmt.Errorf("decode zones: %w", err)
	}
	for i := range zones {
		zones[i].Name = strings.TrimSuffix(zones[i].Name, ".")
		zones[i].RecordCount = len(zones[i].RRSets)
	}
	return zones, nil
}

// GetZone returns a zone with its RRSets.
func (c *Client) GetZone(domain string) (*Zone, error) {
	data, _, err := c.do("GET", zonePath(domain), nil)
	if err != nil {
		return nil, err
	}
	var z Zone
	if err := json.Unmarshal(data, &z); err != nil {
		return nil, fmt.Errorf("decode zone: %w", err)
	}
	z.Name = strings.TrimSuffix(z.Name, ".")
	count := 0
	for _, rr := range z.RRSets {
		count += len(rr.Records)
		if len(rr.Records) == 0 {
			count++
		}
	}
	z.RecordCount = count
	return &z, nil
}

// DeleteZone removes a zone.
func (c *Client) DeleteZone(domain string) error {
	_, _, err := c.do("DELETE", zonePath(domain), nil)
	return err
}

// UpsertRecord replaces a single RRSet (changetype REPLACE).
func (c *Client) UpsertRecord(zone, name, rtype string, ttl int, contents []string) error {
	zone = fqdn(zone)
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == "@" {
		name = zone
	} else if !strings.HasSuffix(name, ".") {
		// Relative name → absolute under zone.
		if strings.HasSuffix(name, "."+strings.TrimSuffix(zone, ".")) {
			name = fqdn(name)
		} else {
			name = fqdn(name + "." + strings.TrimSuffix(zone, "."))
		}
	}
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	if rtype == "" {
		return fmt.Errorf("record type required")
	}
	if ttl <= 0 {
		ttl = 300
	}
	var records []map[string]any
	for _, content := range contents {
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		records = append(records, map[string]any{"content": content, "disabled": false})
	}
	if len(records) == 0 {
		return fmt.Errorf("record content required")
	}
	payload := map[string]any{
		"rrsets": []map[string]any{{
			"name":       name,
			"type":       rtype,
			"ttl":        ttl,
			"changetype": "REPLACE",
			"records":    records,
		}},
	}
	_, _, err := c.do("PATCH", zonePath(zone), payload)
	return err
}

// DeleteRecord removes a single RRSet (changetype DELETE).
func (c *Client) DeleteRecord(zone, name, rtype string) error {
	zone = fqdn(zone)
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || name == "@" {
		name = zone
	} else if !strings.HasSuffix(name, ".") {
		name = fqdn(name)
	}
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	if rtype == "" {
		return fmt.Errorf("record type required")
	}
	payload := map[string]any{
		"rrsets": []map[string]any{{
			"name":       name,
			"type":       rtype,
			"changetype": "DELETE",
		}},
	}
	_, _, err := c.do("PATCH", zonePath(zone), payload)
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
