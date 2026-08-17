package webpanel

import (
	"strings"

	"github.com/lyracorp/xmanager/internal/config"
)

func formatPanelLoginURL(host, port, key string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "host"
	}
	port = strings.TrimSpace(port)
	if port == "" {
		port = "8080"
	}
	base := "http://" + host + ":" + port
	key = strings.Trim(strings.TrimSpace(key), "/")
	if key == "" {
		return base
	}
	return base + "/" + key + "/login"
}

func parseYAMLAccessKey(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "access_key:") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, "access_key:"))
		v = strings.Trim(v, `"'`)
		if key, err := config.NormalizeAccessKey(v); err == nil {
			return key
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		return s[:157] + "…"
	}
	return s
}
