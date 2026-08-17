package web

import "testing"

func TestParseFormSSLEnabled(t *testing.T) {
	if !*parseFormSSLEnabled("", false) {
		t.Fatal("default on when not submitted")
	}
	if *parseFormSSLEnabled("", true) {
		t.Fatal("unchecked submitted form should be off")
	}
	if !*parseFormSSLEnabled("1", true) {
		t.Fatal("checked should be on")
	}
	if *parseFormSSLEnabled("off", false) {
		t.Fatal("explicit off")
	}
}
