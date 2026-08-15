package backup

import (
	"strings"
	"testing"
)

func TestTaskConfigRoundTrip(t *testing.T) {
	cases := []TaskConfig{
		{Name: "dir", Path: "/var/www", Compress: true},
		{Name: "log", Path: "/var/log/app.log", KeepLines: 500},
		{Name: "url", URL: "https://example.com", Timeout: 15},
		{Name: "sh", Script: "echo hi"},
	}
	for _, c := range cases {
		got := ParseTaskConfig(c.JSON())
		if got.Name != c.Name || got.Path != c.Path || got.URL != c.URL || got.Script != c.Script {
			t.Fatalf("round-trip mismatch: %+v vs %+v", c, got)
		}
		if got.KeepLines != c.KeepLines || got.Timeout != c.Timeout || got.Compress != c.Compress {
			t.Fatalf("round-trip nums: %+v vs %+v", c, got)
		}
	}
}

func TestCmdSyncTime(t *testing.T) {
	cmd := CmdSyncTime()
	if !strings.Contains(cmd, "ntpdate") || !strings.Contains(cmd, "chronyc") {
		t.Fatalf("unexpected: %s", cmd)
	}
}

func TestCmdFreeRAM(t *testing.T) {
	cmd := CmdFreeRAM()
	if !strings.Contains(cmd, "drop_caches") {
		t.Fatalf("unexpected: %s", cmd)
	}
}

func TestCmdAccessURL(t *testing.T) {
	cmd := CmdAccessURL("https://example.com/health", 5)
	if !strings.Contains(cmd, "https://example.com/health") || !strings.Contains(cmd, "--max-time 5") {
		t.Fatalf("unexpected: %s", cmd)
	}
}

func TestCmdBackupDirectory(t *testing.T) {
	cmd, out, name := CmdBackupDirectory("webroot", "/var/www", true)
	if !strings.Contains(cmd, "tar czf") || !strings.Contains(cmd, "/var/www") {
		t.Fatalf("cmd=%s", cmd)
	}
	if !strings.HasSuffix(name, ".tar.gz") || !strings.Contains(out, DefaultDir) {
		t.Fatalf("out=%s name=%s", out, name)
	}
}

func TestCmdCutLog(t *testing.T) {
	cmd := CmdCutLog("/var/log/app.log", 200)
	if !strings.Contains(cmd, "tail -n 200") || !strings.Contains(cmd, "/var/log/app.log") {
		t.Fatalf("unexpected: %s", cmd)
	}
}

func TestTaskTypeLabel(t *testing.T) {
	if TaskTypeLabel(TaskShell) != "Shell Script" {
		t.Fatal(TaskTypeLabel(TaskShell))
	}
	if !IsTaskType(TaskFreeRAM) || IsTaskType("nope") {
		t.Fatal("IsTaskType")
	}
}

func TestBuildHostCommand(t *testing.T) {
	cmd, err := BuildHostCommand(TaskSyncTime, "", TaskConfig{})
	if err != nil || !strings.Contains(cmd, "ntpdate") {
		t.Fatalf("sync: %v %s", err, cmd)
	}
	cmd, err = BuildHostCommand(TaskFreeRAM, "", TaskConfig{})
	if err != nil || !strings.Contains(cmd, "drop_caches") {
		t.Fatalf("ram: %v %s", err, cmd)
	}
	cmd, err = BuildHostCommand(TaskAccessURL, "", TaskConfig{URL: "https://ex.com", Timeout: 7})
	if err != nil || !strings.Contains(cmd, "https://ex.com") || !strings.Contains(cmd, "--max-time 7") {
		t.Fatalf("url: %v %s", err, cmd)
	}
	cmd, err = BuildHostCommand(TaskBackupDirectory, "web", TaskConfig{Path: "/var/www", Compress: true})
	if err != nil || !strings.Contains(cmd, "tar czf") || !strings.Contains(cmd, `\%Y`) {
		t.Fatalf("dir: %v %s", err, cmd)
	}
	cmd, err = BuildHostCommand("postgres", "app", TaskConfig{})
	if err != nil || !strings.Contains(cmd, "pg_dump") || !strings.Contains(cmd, "xm-postgres") {
		t.Fatalf("db: %v %s", err, cmd)
	}
	if _, err := BuildHostCommand(TaskAccessURL, "", TaskConfig{}); err == nil {
		t.Fatal("expected url required")
	}
}
