package web

import "testing"

func TestSanitizeContainerRef(t *testing.T) {
	if _, err := sanitizeContainerRef("../evil"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := sanitizeContainerRef("xm-postgres"); err != nil {
		t.Fatal(err)
	}
	if _, err := sanitizeContainerRef("abc123def"); err != nil {
		t.Fatal(err)
	}
}
