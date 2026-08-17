package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

// Record is a Cloudflare DNS record.
type Record struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	ZoneID   string `json:"zone_id"`
	Priority int    `json:"priority,omitempty"`
}

// Client talks to the Cloudflare v4 API.
type Client struct {
	Cfg  Config
	HTTP *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		Cfg: cfg,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) do(method, path string, body any) (json.RawMessage, error) {
	if c.Cfg.APIToken == "" {
		return nil, fmt.Errorf("cloudflare: API token not set (Settings → Node Identity)")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, apiBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)

	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("cloudflare: decode %s: %w (%s)", res.Status, err, truncate(string(raw), 120))
	}
	if !ar.Success || res.StatusCode >= 400 {
		msg := res.Status
		if len(ar.Errors) > 0 {
			msg = ar.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: %s", msg)
	}
	return ar.Result, nil
}

// Ping checks token + zone reachability.
func (c *Client) Ping() error {
	if c.Cfg.ZoneID == "" {
		// Token-only check via user endpoint.
		_, err := c.do("GET", "/user/tokens/verify", nil)
		return err
	}
	_, err := c.do("GET", "/zones/"+url.PathEscape(c.Cfg.ZoneID), nil)
	return err
}

// LookupZoneID resolves a domain name to a Cloudflare Zone ID.
func (c *Client) LookupZoneID(domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(domain, ".")))
	if domain == "" {
		return "", fmt.Errorf("domain required")
	}
	path := "/zones?name=" + url.QueryEscape(domain) + "&status=active"
	result, err := c.do("GET", path, nil)
	if err != nil {
		return "", err
	}
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result, &zones); err != nil {
		return "", fmt.Errorf("decode zones: %w", err)
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("no Cloudflare zone found for %s (add the domain in Cloudflare first)", domain)
	}
	return zones[0].ID, nil
}

// ListRecords returns DNS records for the configured zone.
func (c *Client) ListRecords() ([]Record, error) {
	if c.Cfg.ZoneID == "" {
		return nil, fmt.Errorf("cloudflare: zone ID required")
	}
	path := "/zones/" + url.PathEscape(c.Cfg.ZoneID) + "/dns_records?per_page=100"
	result, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var recs []Record
	if err := json.Unmarshal(result, &recs); err != nil {
		return nil, fmt.Errorf("decode records: %w", err)
	}
	return recs, nil
}

// UpsertRecord creates or updates a DNS record matching name+type.
func (c *Client) UpsertRecord(name, rtype, content string, ttl int, proxied bool) error {
	if c.Cfg.ZoneID == "" {
		return fmt.Errorf("cloudflare: zone ID required")
	}
	name = strings.TrimSpace(name)
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	content = strings.TrimSpace(content)
	if rtype == "" || content == "" {
		return fmt.Errorf("type and content required")
	}
	if ttl <= 0 {
		ttl = 1 // Cloudflare "automatic"
	}
	if name == "" || name == "@" {
		name = "@"
	}

	existing, _ := c.findRecord(name, rtype)
	payload := map[string]any{
		"type":    rtype,
		"name":    name,
		"content": content,
		"ttl":     ttl,
		"proxied": proxied && (rtype == "A" || rtype == "AAAA" || rtype == "CNAME"),
	}

	base := "/zones/" + url.PathEscape(c.Cfg.ZoneID) + "/dns_records"
	if existing != nil {
		_, err := c.do("PUT", base+"/"+url.PathEscape(existing.ID), payload)
		return err
	}
	_, err := c.do("POST", base, payload)
	return err
}

// DeleteRecord removes records matching name+type.
func (c *Client) DeleteRecord(name, rtype string) error {
	if c.Cfg.ZoneID == "" {
		return fmt.Errorf("cloudflare: zone ID required")
	}
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	recs, err := c.ListRecords()
	if err != nil {
		return err
	}
	wantName := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, ".")))
	apex := ""
	if wantName == "" || wantName == "@" {
		apex, _ = c.zoneName()
		apex = strings.ToLower(strings.TrimSuffix(apex, "."))
	}
	deleted := 0
	for _, r := range recs {
		if !strings.EqualFold(r.Type, rtype) {
			continue
		}
		got := strings.ToLower(strings.TrimSuffix(r.Name, "."))
		match := false
		if apex != "" {
			match = got == apex
		} else {
			match = recordNameMatch(r.Name, wantName)
		}
		if !match {
			continue
		}
		if _, err := c.do("DELETE", "/zones/"+url.PathEscape(c.Cfg.ZoneID)+"/dns_records/"+url.PathEscape(r.ID), nil); err != nil {
			return err
		}
		deleted++
	}
	if deleted == 0 {
		return fmt.Errorf("no matching %s record for %s", rtype, name)
	}
	return nil
}

func (c *Client) findRecord(name, rtype string) (*Record, error) {
	recs, err := c.ListRecords()
	if err != nil {
		return nil, err
	}
	name = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, ".")))
	apex := ""
	if name == "" || name == "@" {
		apex, _ = c.zoneName()
		apex = strings.ToLower(strings.TrimSuffix(apex, "."))
	}
	for i := range recs {
		r := &recs[i]
		if !strings.EqualFold(r.Type, rtype) {
			continue
		}
		got := strings.ToLower(strings.TrimSuffix(r.Name, "."))
		if apex != "" {
			if got == apex {
				return r, nil
			}
			continue
		}
		if recordNameMatch(r.Name, name) {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (c *Client) zoneName() (string, error) {
	if c.Cfg.ZoneID == "" {
		return "", fmt.Errorf("no zone id")
	}
	result, err := c.do("GET", "/zones/"+url.PathEscape(c.Cfg.ZoneID), nil)
	if err != nil {
		return "", err
	}
	var z struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result, &z); err != nil {
		return "", err
	}
	return z.Name, nil
}

func recordNameMatch(got, rawWant string) bool {
	got = strings.ToLower(strings.TrimSuffix(got, "."))
	rawWant = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(rawWant, ".")))
	if rawWant == "" || rawWant == "@" {
		return true // caller already filtered by type; apex handled via CF name field
	}
	if got == rawWant {
		return true
	}
	return strings.HasPrefix(got, rawWant+".")
}

// CreateOriginCertificate issues an Origin CA certificate (POST /certificates).
func (c *Client) CreateOriginCertificate(payload map[string]any) (json.RawMessage, error) {
	return c.do("POST", "/certificates", payload)
}

// SetZoneSSLMode sets the zone SSL/TLS mode (flexible, full, strict).
func (c *Client) SetZoneSSLMode(mode string) error {
	if c.Cfg.ZoneID == "" {
		return fmt.Errorf("cloudflare: zone ID required")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "strict"
	}
	_, err := c.do("PATCH", "/zones/"+url.PathEscape(c.Cfg.ZoneID)+"/settings/ssl", map[string]any{"value": mode})
	return err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
