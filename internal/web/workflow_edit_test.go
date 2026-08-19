package web

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func TestWorkflowEditTemplateDrawflow(t *testing.T) {
	tmpl, err := templateParse(t)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err = tmpl.ExecuteTemplate(&buf, "workflow_edit", pageData{
		Title:         "Workflow",
		Workflow:      &storage.Workflow{Model: gorm.Model{ID: 1}, Name: "demo", Trigger: "manual"},
		WorkflowGraph: `{"nodes":[],"edges":[]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"drawflow.min.js",
		"drawflow.min.css",
		"workflow.js",
		"wf-canvas",
		"wf-zoom-in",
		"right",
		"left",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDrawflowVendorEmbedded(t *testing.T) {
	for _, p := range []string{
		"static/vendor/drawflow.min.js",
		"static/vendor/drawflow.min.css",
		"static/vendor/NOTICE",
		"static/workflow.js",
		"static/workflow.css",
	} {
		if _, err := fs.Stat(assets, p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
}
