package gitforge

import "testing"

func TestNormalizeProvider(t *testing.T) {
	if NormalizeProvider("GitHub") != ProviderGitHub {
		t.Fatal("github")
	}
	if NormalizeProvider("gitea-local") != ProviderGitea {
		t.Fatal("gitea")
	}
	if NormalizeProvider("nope") != "" {
		t.Fatal("unknown")
	}
}

func TestAuthenticatedCloneURL(t *testing.T) {
	cases := []struct {
		provider string
		in       string
		wantSub  string
	}{
		{ProviderGitHub, "https://github.com/acme/app.git", "x-access-token:"},
		{ProviderGitLab, "https://gitlab.com/acme/app.git", "oauth2:"},
		{ProviderBitbucket, "https://bitbucket.org/acme/app.git", "x-token-auth:"},
		{ProviderGitea, "http://127.0.0.1:3000/acme/app.git", "x-access-token:"},
	}
	for _, c := range cases {
		got, err := AuthenticatedCloneURL(c.provider, c.in, "secrettoken")
		if err != nil {
			t.Fatalf("%s: %v", c.provider, err)
		}
		if !contains(got, c.wantSub) || !contains(got, "secrettoken") {
			t.Fatalf("%s: got %q want substring %q", c.provider, got, c.wantSub)
		}
		if contains(got, "secrettoken@") == false && !contains(got, ":secrettoken@") {
			// url.UserPassword encodes as user:pass@host
			if !contains(got, "secrettoken") {
				t.Fatalf("token missing: %s", got)
			}
		}
	}
}

func TestAuthorizeURL(t *testing.T) {
	u, err := AuthorizeURL(ProviderGitHub, AppCredentials{ClientID: "cid"}, "https://panel.example.com/oauth/git/github/callback", "st")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(u, "github.com/login/oauth/authorize") || !contains(u, "client_id=cid") {
		t.Fatalf("url=%s", u)
	}
	u, err = AuthorizeURL(ProviderGitea, AppCredentials{ClientID: "c", Endpoint: "http://127.0.0.1:3000"}, "http://127.0.0.1:8080/oauth/git/gitea/callback", "st")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(u, "127.0.0.1:3000/login/oauth/authorize") {
		t.Fatalf("gitea url=%s", u)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
