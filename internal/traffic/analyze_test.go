package traffic

import "testing"

func TestParseNginxAccessLine(t *testing.T) {
	line := `203.0.113.5 - - [17/Aug/2026:10:00:01 +0000] "GET /api/health HTTP/1.1" 200 42 "-" "curl/7"`
	ip, method, path, status, ok := ParseNginxAccessLine(line)
	if !ok {
		t.Fatal("parse failed")
	}
	if ip != "203.0.113.5" || method != "GET" || path != "/api/health" || status != 200 {
		t.Fatalf("got %q %q %q %d", ip, method, path, status)
	}
}
