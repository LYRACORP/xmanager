package gitforge

import (
	"os"
	"strings"
)

// BuiltinCredentials resolves OAuth app credentials from env, then optional overrides.
// Env keys: XMANAGER_GITHUB_OAUTH_CLIENT_ID / _SECRET (same for GITLAB, BITBUCKET, GITEA).
func BuiltinCredentials(provider string) AppCredentials {
	provider = NormalizeProvider(provider)
	prefix := "XMANAGER_" + strings.ToUpper(provider) + "_OAUTH_"
	return AppCredentials{
		ClientID:     strings.TrimSpace(os.Getenv(prefix + "CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv(prefix + "CLIENT_SECRET")),
		Endpoint:     strings.TrimSpace(os.Getenv(prefix + "ENDPOINT")),
	}
}

// MergeCredentials prefers override fields when set, else falls back to base.
func MergeCredentials(base, override AppCredentials) AppCredentials {
	out := base
	if strings.TrimSpace(override.ClientID) != "" {
		out.ClientID = strings.TrimSpace(override.ClientID)
	}
	if strings.TrimSpace(override.ClientSecret) != "" {
		out.ClientSecret = strings.TrimSpace(override.ClientSecret)
	}
	if strings.TrimSpace(override.Endpoint) != "" {
		out.Endpoint = strings.TrimSpace(override.Endpoint)
	}
	return out
}
