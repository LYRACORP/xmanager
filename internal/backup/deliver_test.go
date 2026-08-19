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
	c = ParseDestConfig(`{"bucket":"my-bucket","region":"eu-west-1","access_key":"ak","prefix":"backups"}`)
	if c.Bucket != "my-bucket" || c.Region != "eu-west-1" || c.AccessKey != "ak" || c.Prefix != "backups" {
		t.Fatalf("%+v", c)
	}
}

func TestS3ObjectKey(t *testing.T) {
	if got := s3ObjectKey("", "dump.sql.gz"); got != "dump.sql.gz" {
		t.Fatalf("got %q", got)
	}
	if got := s3ObjectKey("backups", "dump.sql.gz"); got != "backups/dump.sql.gz" {
		t.Fatalf("got %q", got)
	}
	if got := s3ObjectKey("/backups/", "/dump.sql.gz"); got != "backups/dump.sql.gz" {
		t.Fatalf("got %q", got)
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
