package gitforge

import (
	"os"
	"strings"
)

// DefaultRelayURL is the Lyracorp-hosted OAuth relay.
// Every self-hosted XManager panel uses this to get Vercel-style
// GitHub/GitLab connect without any per-installation setup.
// Override with env XMANAGER_OAUTH_RELAY_URL (set to "" to disable).
const DefaultRelayURL = "https://xauth.devcenter.top"

// GetRelayURL returns the active relay URL (env override or default).
func GetRelayURL() string {
	if v, ok := os.LookupEnv("XMANAGER_OAUTH_RELAY_URL"); ok {
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return DefaultRelayURL
}

// defaultClientIDs are Lyracorp-registered OAuth apps for the device flow
// (used as fallback when relay is unavailable or disabled).
// Client IDs are public; secrets stay in env / Advanced settings only.
var defaultClientIDs = map[string]string{
	ProviderGitHub: "Ov23liFFBIVHJ0NLTNly",
	// ProviderGitLab: "",
}

// BuiltinCredentials resolves OAuth app credentials from env, then built-in defaults.
// Env keys: XMANAGER_GITHUB_OAUTH_CLIENT_ID / _SECRET (same for GITLAB, BITBUCKET, GITEA).
func BuiltinCredentials(provider string) AppCredentials {
	provider = NormalizeProvider(provider)
	prefix := "XMANAGER_" + strings.ToUpper(provider) + "_OAUTH_"
	cid := strings.TrimSpace(os.Getenv(prefix + "CLIENT_ID"))
	if cid == "" {
		cid = defaultClientIDs[provider]
	}
	return AppCredentials{
		ClientID:     cid,
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
