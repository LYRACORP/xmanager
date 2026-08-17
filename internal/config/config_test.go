package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClampRetentionDays(t *testing.T) {
	if ClampRetentionDays(-1) != 0 {
		t.Fatal("negative")
	}
	if ClampRetentionDays(0) != 0 {
		t.Fatal("zero keeps forever")
	}
	if ClampRetentionDays(30) != 30 {
		t.Fatal("default")
	}
	if ClampRetentionDays(400) != MaxLogRetentionDays {
		t.Fatal("cap 365")
	}
}

func TestNormalizeAccessKey(t *testing.T) {
	got, err := NormalizeAccessKey("  /Qekyfk7518312j1/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "qekyfk7518312j1" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeAccessKey("short"); err == nil {
		t.Fatal("expected too short")
	}
	if _, err := NormalizeAccessKey("login"); err == nil {
		t.Fatal("expected reserved")
	}
	if _, err := NormalizeAccessKey("has_underscore"); err == nil {
		t.Fatal("expected charset error")
	}
}

func TestGenerateAccessKey(t *testing.T) {
	a, err := GenerateAccessKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateAccessKey()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("expected unique keys")
	}
	if _, err := NormalizeAccessKey(a); err != nil {
		t.Fatal(err)
	}
	if len(a) != AccessKeyLen {
		t.Fatalf("len %d", len(a))
	}
}

func TestEnsureAccessKey(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{ConfigPath: filepath.Join(dir, "config.yaml")}
	key, err := EnsureAccessKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.AccessKey != key {
		t.Fatal("not stored on config")
	}
	if _, err := os.Stat(cfg.ConfigPath); err != nil {
		t.Fatal(err)
	}
	again, err := EnsureAccessKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again != key {
		t.Fatal("should keep existing valid key")
	}
}
