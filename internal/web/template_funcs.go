package web

import (
	"encoding/json"
	"html/template"
	"net/url"
	"strings"

	"github.com/lyracorp/xmanager/internal/nodemetrics"
)

func webTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatBytes":  nodemetrics.FormatBytes,
		"formatUptime": nodemetrics.FormatUptime,
		"pathEscape":   url.PathEscape,
		"trimSlash":    func(s string) string { return strings.Trim(s, "/") },
		"trimDot": func(s string) string {
			for len(s) > 0 && s[len(s)-1] == '.' {
				s = s[:len(s)-1]
			}
			return s
		},
		"json": func(v interface{}) (template.JS, error) {
			b, err := json.Marshal(v)
			return template.JS(b), err
		},
		"loadPct":   loadGaugePct,
		"gaugeTone": gaugeToneClass,
	}
}

func loadGaugePct(load float64, cores int) float64 {
	if cores < 1 {
		cores = 1
	}
	p := load / float64(cores) * 100
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

func gaugeToneClass(pct float64) string {
	if pct >= 85 {
		return "is-danger"
	}
	if pct >= 65 {
		return "is-warn"
	}
	return ""
}
