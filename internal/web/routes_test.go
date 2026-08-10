package web

import (
	"net/http"
	"testing"

	"github.com/lyracorp/xmanager/internal/config"
)

func TestRegisterNodeRoutesNoConflict(t *testing.T) {
	h := &handler{
		opts: Options{
			Config: &config.Config{},
		},
		nodeMode: true,
	}
	mux := http.NewServeMux()
	// Must not panic: previously POST /projects/{id}/deploy conflicted with
	// POST /projects/templates/{id} (and /projects/oneclick/{id}) on overlapping paths.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registerNode panicked: %v", r)
		}
	}()
	h.registerNode(mux)
}
