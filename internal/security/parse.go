package security

import (
	"regexp"
	"strings"
)

var (
	authIPRe   = regexp.MustCompile(`from ([0-9a-fA-F:.]+)`)
	authUserRe = regexp.MustCompile(`for (?:invalid user )?([^\s]+)`)
)

// ParseUFWStatus parses `ufw status verbose` output.
func ParseUFWStatus(raw string) FirewallStatus {
	st := FirewallStatus{Active: strings.Contains(strings.ToLower(raw), "status: active")}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Status:") || strings.HasPrefix(line, "Logging:") ||
			strings.HasPrefix(line, "Default:") || strings.HasPrefix(line, "New profiles:") ||
			strings.HasPrefix(line, "To ") || strings.HasPrefix(line, "--") {
			continue
		}
		// To                         Action      From
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		if parts[0] == "To" || parts[0] == "--" {
			continue
		}
		rule := FirewallRule{
			Port:   parts[0],
			Action: parts[1],
		}
		if len(parts) >= 4 {
			rule.From = parts[3]
		}
		if idx := strings.Index(rule.Port, "/"); idx > 0 {
			rule.Proto = rule.Port[idx+1:]
			rule.Port = rule.Port[:idx]
		}
		st.Rules = append(st.Rules, rule)
	}
	return st
}

// ParseIPTables parses a simple iptables -L listing.
func ParseIPTables(raw string) []FirewallRule {
	var rules []FirewallRule
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ACCEPT") && !strings.HasPrefix(line, "DROP") && !strings.HasPrefix(line, "REJECT") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		r := FirewallRule{Action: parts[0]}
		for i, p := range parts {
			if p == "dpt:" && i+1 < len(parts) {
				r.Port = parts[i+1]
			}
			if p == "tcp" || p == "udp" {
				r.Proto = p
			}
		}
		rules = append(rules, r)
	}
	return rules
}

// ParseCertbot parses certbot certificates output.
func ParseCertbot(raw string) []SSLCert {
	var certs []SSLCert
	var cur *SSLCert
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Certificate Name:") {
			if cur != nil {
				certs = append(certs, *cur)
			}
			cur = &SSLCert{Name: strings.TrimSpace(strings.TrimPrefix(line, "Certificate Name:"))}
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(line, "Domains:") {
			dom := strings.TrimSpace(strings.TrimPrefix(line, "Domains:"))
			cur.Domains = strings.Fields(strings.ReplaceAll(dom, ",", " "))
		}
		if strings.HasPrefix(line, "Expiry Date:") {
			cur.Expiry = strings.TrimSpace(strings.TrimPrefix(line, "Expiry Date:"))
		}
		if strings.HasPrefix(line, "Certificate Path:") {
			cur.CertPath = strings.TrimSpace(strings.TrimPrefix(line, "Certificate Path:"))
		}
	}
	if cur != nil {
		certs = append(certs, *cur)
	}
	return certs
}

// ParseFail2ban parses fail2ban-client status.
func ParseFail2ban(raw string) Fail2banStatus {
	st := Fail2banStatus{Raw: raw}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Jail list:") {
			list := strings.TrimSpace(strings.TrimPrefix(line, "Jail list:"))
			for _, j := range strings.Split(list, ",") {
				j = strings.TrimSpace(j)
				if j != "" {
					st.Jails = append(st.Jails, Fail2banJail{Name: j})
				}
			}
		}
	}
	return st
}

// ParseFail2banBanned extracts banned IP list from jail status.
func ParseFail2banBanned(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Banned IP list:") {
			list := strings.TrimSpace(strings.TrimPrefix(line, "Banned IP list:"))
			if list == "" {
				return nil
			}
			var ips []string
			for _, ip := range strings.Fields(strings.ReplaceAll(list, ",", " ")) {
				ip = strings.TrimSpace(ip)
				if ip != "" {
					ips = append(ips, ip)
				}
			}
			return ips
		}
	}
	return nil
}

// ParseAuthFailures extracts SSH failure rows from log text.
func ParseAuthFailures(raw string) []AuthFail {
	var out []AuthFail
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "failed") && !strings.Contains(lower, "invalid user") && !strings.Contains(lower, "authentication failure") {
			continue
		}
		fail := AuthFail{Detail: line}
		// timestamp: first 15 chars often "Mon DD HH:MM:SS" or ISO prefix
		if len(line) > 15 {
			fail.Time = line[:15]
		}
		if m := authIPRe.FindStringSubmatch(line); len(m) > 1 {
			fail.IP = m[1]
		}
		if m := authUserRe.FindStringSubmatch(line); len(m) > 1 {
			fail.User = m[1]
		}
		out = append(out, fail)
	}
	return out
}

// ScanProbe returns true if path/query looks like a common probe.
func ScanProbe(path, query string) bool {
	s := strings.ToLower(path + "?" + query)
	probes := []string{"../", ".env", "wp-admin", "xmlrpc.php", "phpmyadmin", "shell", "eval(", "union select", "/.git", "cgi-bin"}
	for _, p := range probes {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
