package security

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// Policy holds WAF, rate limit, and traffic control settings for a server.
type Policy struct {
	Enabled          bool
	RateRPM          int
	RateBurst        int
	ConnLimit        int
	WAFBuiltin       bool
	WAFSQLi          bool
	WAFXSS           bool
	WAFTraversal     bool
	WAFBadBots       bool
	WAFModsec        bool
	ModsecDetectOnly bool
	ModsecParanoia   int
	BlockIPs         []string
	AllowIPs         []string
	AutoFail2ban     int
	AnalysisEnabled  bool
}

// DefaultPolicy returns off-by-default settings with sensible limits when enabled.
func DefaultPolicy() Policy {
	return Policy{
		RateRPM:        120,
		RateBurst:      30,
		ConnLimit:      20,
		WAFSQLi:        true,
		WAFXSS:         true,
		WAFTraversal:   true,
		WAFBadBots:     true,
		ModsecParanoia: 1,
	}
}

// LoadPolicy reads policy for a server.
func LoadPolicy(db *gorm.DB, serverID uint) Policy {
	p := DefaultPolicy()
	if db == nil || serverID == 0 {
		return p
	}
	var row storage.SecurityPolicy
	if err := db.Where("server_id = ?", serverID).First(&row).Error; err != nil {
		return p
	}
	p.Enabled = row.Enabled
	if row.ConfigJSON == "" || row.ConfigJSON == "{}" {
		return p
	}
	var raw map[string]interface{}
	if json.Unmarshal([]byte(row.ConfigJSON), &raw) != nil {
		return p
	}
	p.RateRPM = intVal(raw, "rate_rpm", p.RateRPM)
	p.RateBurst = intVal(raw, "rate_burst", p.RateBurst)
	p.ConnLimit = intVal(raw, "conn_limit", p.ConnLimit)
	p.WAFBuiltin = boolVal(raw, "waf_builtin")
	p.WAFSQLi = boolValDefault(raw, "waf_sqli", true)
	p.WAFXSS = boolValDefault(raw, "waf_xss", true)
	p.WAFTraversal = boolValDefault(raw, "waf_traversal", true)
	p.WAFBadBots = boolValDefault(raw, "waf_bad_bots", true)
	p.WAFModsec = boolVal(raw, "waf_modsec")
	p.ModsecDetectOnly = boolValDefault(raw, "modsec_detect_only", true)
	p.ModsecParanoia = intVal(raw, "modsec_paranoia", 1)
	p.AutoFail2ban = intVal(raw, "auto_fail2ban", 0)
	p.AnalysisEnabled = boolVal(raw, "analysis_enabled")
	p.BlockIPs = strSlice(raw, "block_ips")
	p.AllowIPs = strSlice(raw, "allow_ips")
	return p
}

// SavePolicy persists policy for a server.
func SavePolicy(db *gorm.DB, serverID uint, p Policy) error {
	if db == nil || serverID == 0 {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"rate_rpm":           p.RateRPM,
		"rate_burst":         p.RateBurst,
		"conn_limit":         p.ConnLimit,
		"waf_builtin":        p.WAFBuiltin,
		"waf_sqli":           p.WAFSQLi,
		"waf_xss":            p.WAFXSS,
		"waf_traversal":      p.WAFTraversal,
		"waf_bad_bots":       p.WAFBadBots,
		"waf_modsec":         p.WAFModsec,
		"modsec_detect_only": p.ModsecDetectOnly,
		"modsec_paranoia":    p.ModsecParanoia,
		"auto_fail2ban":      p.AutoFail2ban,
		"analysis_enabled":   p.AnalysisEnabled,
		"block_ips":          p.BlockIPs,
		"allow_ips":          p.AllowIPs,
	})
	var row storage.SecurityPolicy
	res := db.Where("server_id = ?", serverID).First(&row)
	if res.Error != nil {
		row = storage.SecurityPolicy{
			ServerID:   serverID,
			Enabled:    p.Enabled,
			ConfigJSON: string(raw),
		}
		return db.Create(&row).Error
	}
	row.Enabled = p.Enabled
	row.ConfigJSON = string(raw)
	return db.Save(&row).Error
}

// IsAllowed checks allow/block lists. Allow wins over block.
func IsAllowed(ip string, p Policy) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return true
	}
	for _, cidr := range p.AllowIPs {
		if ipMatch(ip, cidr) {
			return true
		}
	}
	for _, cidr := range p.BlockIPs {
		if ipMatch(ip, cidr) {
			return false
		}
	}
	return true
}

func ipMatch(ip, cidr string) bool {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return false
	}
	if !strings.Contains(cidr, "/") {
		return ip == cidr
	}
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && n.Contains(parsed)
}

func boolVal(m map[string]interface{}, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "on" || t == "1"
	default:
		return false
	}
}

func boolValDefault(m map[string]interface{}, key string, def bool) bool {
	if _, ok := m[key]; !ok {
		return def
	}
	return boolVal(m, key)
}

func intVal(m map[string]interface{}, key string, def int) int {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		return def
	}
}

func strSlice(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []interface{}:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		var parts []string
		for _, p := range strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == '\n'
		}) {
			p = strings.TrimSpace(p)
			if p != "" {
				parts = append(parts, p)
			}
		}
		return parts
	default:
		return nil
	}
}

// AddBlockIP appends an IP to the block list if not present.
func AddBlockIP(p *Policy, ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	for _, b := range p.BlockIPs {
		if b == ip {
			return
		}
	}
	p.BlockIPs = append(p.BlockIPs, ip)
}

// RemoveBlockIP removes an IP from the block list.
func RemoveBlockIP(p *Policy, ip string) {
	ip = strings.TrimSpace(ip)
	var out []string
	for _, b := range p.BlockIPs {
		if b != ip {
			out = append(out, b)
		}
	}
	p.BlockIPs = out
}
