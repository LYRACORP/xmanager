package logreader

import (
	"strings"
	"testing"
)

func TestSanitizeID(t *testing.T) {
	cases := map[string]string{
		"web":           "web",
		"xm-postgres":   "xm-postgres",
		"abc123":        "abc123",
		"my-app":        "my-app",
		"evil;rm -rf":   "",
		"a b":           "",
		"":              "",
		"../../etc":     "../../etc", // dots/slashes allowed but still no shell metachar
	}
	// ../../etc has no shell metachar — sanitize allows . and /
	// That's ok for path-like ids; projectLogCmd uses it inside known prefix.
	for in, want := range cases {
		if got := sanitizeID(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
	if sanitizeID("$(reboot)") != "" {
		t.Fatal("metachar")
	}
}

func TestPanelCommands(t *testing.T) {
	cmds := PanelCommandsForTest(300)
	if len(cmds) < 1 || !strings.Contains(cmds[0], "journalctl") || !strings.Contains(cmds[0], "xmanager-web") {
		t.Fatalf("%v", cmds)
	}
}

func TestSystemCommands(t *testing.T) {
	cmds := SystemCommandsForTest(100)
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "journalctl") || !strings.Contains(joined, "syslog") {
		t.Fatalf("%s", joined)
	}
}

func TestTruncate(t *testing.T) {
	s := strings.Repeat("a", MaxBytes+10)
	out := TruncateForTest(s)
	if len(out) <= MaxBytes {
		t.Fatalf("expected marker, len=%d", len(out))
	}
	if !strings.Contains(out, "truncated") {
		t.Fatal(out[len(out)-40:])
	}
}

func TestIsValidSource(t *testing.T) {
	if !IsValidSource(SourcePanel) || IsValidSource("nope") {
		t.Fatal("IsValidSource")
	}
}

func TestProjectLogCmdSafe(t *testing.T) {
	cmd := projectLogCmd("my-app", 50)
	if !strings.Contains(cmd, "/opt/xmanager/projects/my-app") || !strings.Contains(cmd, "--tail=50") {
		t.Fatal(cmd)
	}
}
