package config

import "testing"

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
