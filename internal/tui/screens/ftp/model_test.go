package ftp

import (
	"testing"

	"github.com/lyracorp/xmanager/internal/tui/shared"
)

func TestFTPScreenName(t *testing.T) {
	m := New(&shared.AppContext{})
	if m.Name() != "FTP" {
		t.Fatalf("Name() = %q", m.Name())
	}
}
