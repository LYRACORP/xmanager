package waf

import (
	"net/http"
	"testing"

	"github.com/lyracorp/xmanager/internal/security"
)

func testPolicy() security.Policy {
	return security.Policy{
		Enabled:      true,
		WAFBuiltin:   true,
		WAFSQLi:      true,
		WAFXSS:       true,
		WAFTraversal: true,
		WAFBadBots:   true,
	}
}

func TestEvaluateTraversal(t *testing.T) {
	req, _ := http.NewRequest("GET", "/../../etc/passwd", nil)
	res := Evaluate(req, testPolicy())
	if !res.Blocked || res.RuleID != "traversal" {
		t.Fatalf("got %+v", res)
	}
}

func TestEvaluateSQLi(t *testing.T) {
	req, _ := http.NewRequest("GET", "/search?q=1+union+select+null", nil)
	res := Evaluate(req, testPolicy())
	if !res.Blocked {
		t.Fatal("expected sqli block")
	}
}

func TestEvaluateClean(t *testing.T) {
	req, _ := http.NewRequest("GET", "/api/health", nil)
	res := Evaluate(req, testPolicy())
	if res.Blocked {
		t.Fatalf("unexpected block %+v", res)
	}
}
