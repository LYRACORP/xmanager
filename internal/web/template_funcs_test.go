package web

import (
	"bytes"
	"html/template"
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/nodemetrics"
)

func TestLoadGaugePct(t *testing.T) {
	if got := loadGaugePct(0.5, 1); got != 50 {
		t.Fatalf("got %v", got)
	}
	if got := loadGaugePct(4, 2); got != 100 {
		t.Fatalf("cap got %v", got)
	}
	if got := loadGaugePct(0.03, 0); got != 3 {
		t.Fatalf("zero cores got %v", got)
	}
}

func TestGaugeToneClass(t *testing.T) {
	if gaugeToneClass(50) != "" || gaugeToneClass(70) != "is-warn" || gaugeToneClass(90) != "is-danger" {
		t.Fatal("tone thresholds")
	}
}

func TestNodeMetricsGauges(t *testing.T) {
	tmpl, err := templateParse(t)
	if err != nil {
		t.Fatal(err)
	}
	data := pageData{
		Node: nodemetrics.Snapshot{
			CPUPct: 52.4, RAMPct: 49.5, DiskPct: 81,
			RAMUsedMB: 806, RAMTotalMB: 1629,
			DiskUsedGB: 18, DiskTotalGB: 24,
			Load1: 0.03, Load5: 0.17, Load15: 0.24, Cores: 1,
		},
		UptimeHuman: "24m",
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "node_metrics", data); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"xm-gauge", "CPU", "RAM", "Disk", "Load", "--gauge-pct", "52.4%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(out, "rounded-full h-2") {
		t.Fatal("linear bars should be gone")
	}
}

func templateParse(t *testing.T) (*template.Template, error) {
	t.Helper()
	return template.New("").Funcs(webTemplateFuncs()).ParseFS(assets, "templates/*.html")
}
