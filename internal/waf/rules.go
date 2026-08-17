package waf

import (
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/lyracorp/xmanager/internal/security"
)

const maxHeaderBytes = 32 * 1024
const maxBodyCheck = 8192

var (
	sqliRe      = regexp.MustCompile(`(?i)(union[\s\+]+select|select[\s\+].+[\s\+]from|insert[\s\+]+into|drop[\s\+]+table|;\s*--|or[\s\+]+1[\s\+]*=[\s\+]*1)`)
	xssRe       = regexp.MustCompile(`(?i)(<script|javascript:|onerror\s*=|onload\s*=|<iframe|document\.cookie)`)
	traversalRe = regexp.MustCompile(`(?i)(\.\./|\.\.%2f|%2e%2e/|/etc/passwd|/proc/self)`)
	badBotRe    = regexp.MustCompile(`(?i)(nikto|sqlmap|masscan|nmap|dirbuster|gobuster|zgrab|acunetix)`)
)

// Result of WAF evaluation.
type Result struct {
	Blocked bool
	RuleID  string
	Detail  string
}

// Evaluate checks a request against policy rules.
func Evaluate(r *http.Request, p security.Policy) Result {
	if !p.Enabled || !p.WAFBuiltin || r == nil {
		return Result{}
	}
	if r.URL.Path == "/static/" || strings.HasPrefix(r.URL.Path, "/static/") {
		return Result{}
	}

	target := strings.ToLower(r.URL.Path + "?" + r.URL.RawQuery)
	ua := strings.ToLower(r.UserAgent())

	if p.WAFTraversal {
		if traversalRe.MatchString(target) || security.ScanProbe(r.URL.Path, r.URL.RawQuery) {
			return Result{Blocked: true, RuleID: "traversal", Detail: r.URL.Path}
		}
	}
	if p.WAFSQLi && sqliRe.MatchString(target) {
		return Result{Blocked: true, RuleID: "sqli", Detail: truncate(target, 120)}
	}
	if p.WAFXSS && xssRe.MatchString(target) {
		return Result{Blocked: true, RuleID: "xss", Detail: truncate(target, 120)}
	}
	if p.WAFBadBots && badBotRe.MatchString(ua) {
		return Result{Blocked: true, RuleID: "bad_bot", Detail: r.UserAgent()}
	}

	// Header checks
	var headerSize int
	for k, vals := range r.Header {
		headerSize += len(k)
		for _, v := range vals {
			headerSize += len(v)
			combined := strings.ToLower(k + ":" + v)
			if p.WAFSQLi && sqliRe.MatchString(combined) {
				return Result{Blocked: true, RuleID: "sqli_header", Detail: k}
			}
			if p.WAFXSS && xssRe.MatchString(combined) {
				return Result{Blocked: true, RuleID: "xss_header", Detail: k}
			}
		}
	}
	if headerSize > maxHeaderBytes {
		return Result{Blocked: true, RuleID: "oversized_headers", Detail: "headers too large"}
	}

	// Body sample for POST/PUT/PATCH
	if r.Body != nil && r.ContentLength != 0 {
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxBodyCheck))
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		bs := strings.ToLower(string(body))
		if p.WAFSQLi && sqliRe.MatchString(bs) {
			return Result{Blocked: true, RuleID: "sqli_body", Detail: "body"}
		}
		if p.WAFXSS && xssRe.MatchString(bs) {
			return Result{Blocked: true, RuleID: "xss_body", Detail: "body"}
		}
	}

	return Result{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
