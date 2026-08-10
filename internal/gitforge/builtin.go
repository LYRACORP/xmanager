package gitforge

import (
	"os"
	"strings"
)

// defaultClientIDs are Lyracorp-registered OAuth apps for the device flow.
// Client IDs are public; secrets stay in env / Advanced settings only.
// Override via env: XMANAGER_GITHUB_OAUTH_CLIENT_ID etc.
//
// After registering at https://github.com/settings/applications/new
// (Enable Device Flow checked), put the Client ID here.
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
