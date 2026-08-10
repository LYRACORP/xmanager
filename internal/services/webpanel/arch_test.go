package webpanel

import "testing"

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":           "amd64",
		"amd64":            "amd64",
		"X86_64\n":         "amd64",
		"aarch64":          "arm64",
		"arm64":            "arm64",
		"":                 "",
		"ppc64le":          "",
		"  x86_64 extra  ": "amd64",
	}
	for in, want := range cases {
		if got := normalizeArch(in); got != want {
			t.Errorf("normalizeArch(%q)=%q want %q", in, got, want)
		}
	}
}
