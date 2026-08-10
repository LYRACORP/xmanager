package gitforge

import (
	"os"
	"testing"
)

func TestBuiltinCredentialsEnvOverridesDefault(t *testing.T) {
	prev := os.Getenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID")
	t.Cleanup(func() {
		_ = os.Setenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID", prev)
	})

	_ = os.Setenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID", "env-client-id")
	got := BuiltinCredentials(ProviderGitHub)
	if got.ClientID != "env-client-id" {
		t.Fatalf("ClientID = %q, want env-client-id", got.ClientID)
	}
}

func TestBuiltinCredentialsFallsBackToDefaultMap(t *testing.T) {
	prev := os.Getenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID")
	t.Cleanup(func() {
		_ = os.Setenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID", prev)
	})
	_ = os.Unsetenv("XMANAGER_GITHUB_OAUTH_CLIENT_ID")

	want := defaultClientIDs[ProviderGitHub]
	got := BuiltinCredentials(ProviderGitHub)
	if got.ClientID != want {
		t.Fatalf("ClientID = %q, want default %q", got.ClientID, want)
	}
}
