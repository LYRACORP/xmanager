package webpanel

import (
	"strings"
	"testing"
)

func TestCmdStopDoesNotRemoveBinary(t *testing.T) {
	s := CmdStop()
	if strings.Contains(s, binPath) {
		t.Fatalf("stop should keep binary: %s", s)
	}
	if strings.Contains(s, "rm ") {
		t.Fatalf("stop should not rm files: %s", s)
	}
	if !strings.Contains(s, "systemctl disable --now xmanager-web") {
		t.Fatalf("expected disable --now: %s", s)
	}
}

func TestCmdUninstallFilesRemovesPaths(t *testing.T) {
	s := CmdUninstallFiles()
	for _, p := range []string{unitPath, binPath, installDir, dataDir} {
		if !strings.Contains(s, p) {
			t.Fatalf("uninstall missing %s: %s", p, s)
		}
	}
}

func TestCmdPresentCheck(t *testing.T) {
	s := CmdPresentCheck()
	if !strings.Contains(s, unitPath) || !strings.Contains(s, binPath) {
		t.Fatalf("present check: %s", s)
	}
}

func TestCmdDeferredWrapsSleep(t *testing.T) {
	s := CmdDeferred(CmdStop())
	if !strings.Contains(s, "sleep 2") || !strings.Contains(s, "nohup") {
		t.Fatalf("deferred: %s", s)
	}
	if !strings.Contains(s, "systemctl disable --now") {
		t.Fatalf("deferred inner: %s", s)
	}
}
