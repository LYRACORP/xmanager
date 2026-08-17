package proxy

import (
	"fmt"
	"strings"
)

// VHostMeta is parsed from an nginx site file.
type VHostMeta struct {
	Domain   string
	Upstream string
	Includes []string
	SSL      bool
}

// HTTPVHostConfig renders an HTTP-only reverse-proxy site.
func HTTPVHostConfig(domain, upstream string, includes []string) string {
	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;

    location / {
%s        proxy_pass %s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`, domain, includeLines(includes), upstream)
}

// HTTPSVHostConfig renders HTTP→HTTPS redirect plus a TLS reverse-proxy site.
func HTTPSVHostConfig(domain, upstream, cert, key string, includes []string) string {
	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name %s;
    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
%s        proxy_pass %s;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`, domain, domain, cert, key, includeLines(includes), upstream)
}

func includeLines(includes []string) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, inc := range includes {
		inc = strings.TrimSpace(inc)
		if inc == "" || seen[inc] {
			continue
		}
		seen[inc] = true
		b.WriteString("        include " + inc + ";\n")
	}
	return b.String()
}

// ParseVHostConfig extracts domain, upstream, snippet includes, and SSL from nginx config text.
func ParseVHostConfig(content string) VHostMeta {
	meta := VHostMeta{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "server_name ") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "server_name "), ";")
			if meta.Domain == "" {
				meta.Domain = strings.TrimSpace(name)
			}
		}
		if strings.HasPrefix(line, "proxy_pass ") {
			meta.Upstream = strings.TrimSuffix(strings.TrimPrefix(line, "proxy_pass "), ";")
		}
		if strings.HasPrefix(line, "include ") && strings.Contains(line, "/snippets/") {
			inc := strings.TrimSuffix(strings.TrimPrefix(line, "include "), ";")
			inc = strings.TrimSpace(inc)
			if inc != "" {
				meta.Includes = append(meta.Includes, inc)
			}
		}
		if strings.Contains(line, "ssl_certificate") && !strings.Contains(line, "ssl_certificate_key") {
			meta.SSL = true
		}
	}
	return meta
}
