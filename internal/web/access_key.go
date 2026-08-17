package web

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

const internalReqdumpPath = "/internal/reqdump"

type accessGate struct {
	mu    sync.RWMutex
	key   string
	inner http.Handler
}

func newAccessGate(key string, inner http.Handler) *accessGate {
	return &accessGate{key: key, inner: inner}
}

func (g *accessGate) setKey(key string) {
	g.mu.Lock()
	g.key = key
	g.mu.Unlock()
}

func (g *accessGate) getKey() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.key
}

func (g *accessGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isLoopbackReqdump(r) {
		g.inner.ServeHTTP(w, r)
		return
	}

	key := g.getKey()
	rest, ok := stripAccessKey(r.URL.Path, key)
	if !ok {
		http.NotFound(w, r)
		return
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = rest
	if r2.URL.RawPath != "" {
		r2.URL.RawPath = rest
	}

	pw := newPrefixWriter(w, "/"+key)
	g.inner.ServeHTTP(pw, r2)
	pw.flush()
}

func stripAccessKey(path, key string) (string, bool) {
	if key == "" || path == "" {
		return "", false
	}
	want := "/" + key
	var got string
	if path == want {
		got = want
	} else if strings.HasPrefix(path, want+"/") {
		got = want
	} else {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return "", false
	}
	rest := path[len(want):]
	if rest == "" {
		return "/", true
	}
	return rest, true
}

func isLoopbackReqdump(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.Path != internalReqdumpPath {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func prefixLocation(prefix, loc string) string {
	if loc == "" || !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") {
		return loc
	}
	if loc == prefix || strings.HasPrefix(loc, prefix+"/") {
		return loc
	}
	if loc == "/" {
		return prefix + "/"
	}
	return prefix + loc
}

func rewriteHTMLPaths(body []byte, prefix string) []byte {
	key := strings.TrimPrefix(prefix, "/")
	if key == "" || len(body) == 0 {
		return body
	}
	pre := []byte(key + "/")
	out := make([]byte, 0, len(body)+len(prefix))
	for i := 0; i < len(body); i++ {
		if i+2 < len(body) && body[i] == '=' && (body[i+1] == '"' || body[i+1] == '\'') && body[i+2] == '/' {
			q := body[i+1]
			rest := body[i+3:]
			if len(rest) > 0 && rest[0] == '/' {
				out = append(out, body[i])
				continue
			}
			if bytes.HasPrefix(rest, pre) {
				out = append(out, body[i])
				continue
			}
			if htmlURLAttrName(body, i) {
				out = append(out, '=', q)
				out = append(out, prefix...)
				out = append(out, '/')
				i += 2
				continue
			}
		}
		out = append(out, body[i])
	}
	return out
}

func htmlURLAttrName(body []byte, eq int) bool {
	j := eq - 1
	for j >= 0 {
		c := body[j]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			j--
			continue
		}
		break
	}
	switch strings.ToLower(string(body[j+1 : eq])) {
	case "href", "src", "action", "hx-get", "hx-post", "hx-put", "hx-delete", "hx-patch":
		return true
	default:
		return false
	}
}

type prefixWriter struct {
	http.ResponseWriter
	prefix        string
	status        int
	headerWritten bool
	html          bool
	buf           []byte
}

func newPrefixWriter(w http.ResponseWriter, prefix string) *prefixWriter {
	return &prefixWriter{ResponseWriter: w, prefix: prefix, status: http.StatusOK}
}

func (w *prefixWriter) WriteHeader(code int) {
	if w.headerWritten {
		return
	}
	w.status = code
	if loc := w.Header().Get("Location"); loc != "" {
		w.Header().Set("Location", prefixLocation(w.prefix, loc))
	}
	if strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		w.html = true
		return
	}
	w.ResponseWriter.WriteHeader(code)
	w.headerWritten = true
}

func (w *prefixWriter) Write(b []byte) (int, error) {
	if !w.html && strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		w.html = true
	}
	if w.html {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		if loc := w.Header().Get("Location"); loc != "" {
			w.Header().Set("Location", prefixLocation(w.prefix, loc))
		}
		w.buf = append(w.buf, b...)
		return len(b), nil
	}
	if !w.headerWritten {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *prefixWriter) flush() {
	if !w.html {
		if !w.headerWritten {
			w.WriteHeader(w.status)
		}
		return
	}
	out := rewriteHTMLPaths(w.buf, w.prefix)
	if !w.headerWritten {
		w.Header().Del("Content-Length")
		w.ResponseWriter.WriteHeader(w.status)
		w.headerWritten = true
	}
	if len(out) > 0 {
		_, _ = w.ResponseWriter.Write(out)
	}
	w.buf = nil
}

func (w *prefixWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.html = false
	w.buf = nil
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack not supported")
	}
	if !w.headerWritten {
		w.headerWritten = true
	}
	return h.Hijack()
}

func (w *prefixWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.html {
		f.Flush()
	}
}

func (w *prefixWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
