package ssh

import (
	"context"
	"strings"
	"testing"
)

func TestLocalExecutorRun(t *testing.T) {
	ex := NewLocalExecutor()
	if !ex.IsLocal() {
		t.Fatal("expected local executor")
	}
	res, err := ex.Run("echo hello-local")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "hello-local" {
		t.Fatalf("got %#v", res)
	}
	res, err = ex.Run("false")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit")
	}
	if got := ex.RunQuiet("echo quiet"); got != "quiet" {
		t.Fatalf("RunQuiet got %q", got)
	}
	var lines []string
	if err := ex.StreamWait(context.Background(), "printf 'a\\nb\\n'", func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "a,b" {
		t.Fatalf("StreamWait lines %v", lines)
	}
}
