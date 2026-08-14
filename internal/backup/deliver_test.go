package backup

import (
	"path/filepath"
	"testing"

	"github.com/lyracorp/xmanager/internal/dbmanager"
)

func TestSafeBackupPath(t *testing.T) {
	ok, err := SafeBackupPath(DefaultDir + "/pg-app-1.sql.gz")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(ok) != "pg-app-1.sql.gz" && !filepath.IsAbs(ok) {
		t.Fatalf("got %q", ok)
	}
	if _, err := SafeBackupPath("/etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := SafeBackupPath(DefaultDir + "/../etc/passwd"); err == nil {
		t.Fatal("expected .. escape error")
	}
}

func TestParseDestConfig(t *testing.T) {
	c := ParseDestConfig(`{"host":"ftp.example","user":"u","path":"/backups"}`)
	if c.Host != "ftp.example" || c.User != "u" || c.Path != "/backups" {
		t.Fatalf("%+v", c)
	}
}

func TestSkipSystemDB(t *testing.T) {
	if !skipSystemDB(dbmanager.PostgreSQL, "template0") {
		t.Fatal("expected skip")
	}
	if skipSystemDB(dbmanager.PostgreSQL, "app") {
		t.Fatal("should not skip app")
	}
}
