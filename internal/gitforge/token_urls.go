package gitforge

// TokenCreateURL is a browser URL where the user can mint an access token.
func TokenCreateURL(provider, endpoint string) string {
	provider = NormalizeProvider(provider)
	base := baseEndpoint(provider, endpoint)
	switch provider {
	case ProviderGitHub:
		return "https://github.com/settings/tokens/new?scopes=repo,read:user&description=XManager"
	case ProviderGitLab:
		return base + "/-/user_settings/personal_access_tokens?name=XManager&scopes=api,read_repository,read_api"
	case ProviderBitbucket:
		return "https://bitbucket.org/account/settings/app-passwords/"
	case ProviderGitea:
		return base + "/user/settings/applications"
	default:
		return base
	}
}

// TokenHint explains what to create on the forge.
func TokenHint(provider string) string {
	switch NormalizeProvider(provider) {
	case ProviderGitHub:
		return "GitHub opens in your browser. Create a token with repo + read:user, copy it, paste below."
	case ProviderGitLab:
		return "GitLab opens in your browser. Create a PAT with api/read_repository, copy it, paste below."
	case ProviderBitbucket:
		return "Bitbucket opens in your browser. Create an app password (Repositories: Read), paste it below."
	case ProviderGitea:
		return "Gitea opens in your browser. Create an access token (read:repository, read:user), paste below."
	default:
		return "Create an access token and paste it below."
	}
}
