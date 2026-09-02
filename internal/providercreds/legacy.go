package providercreds

import (
	"strings"
	"time"
)

// LegacyEnv names the flat, single-organisation variables this package
// replaces. They stay readable so an existing machine keeps working
// across the upgrade without anyone re-entering a credential.
const (
	LegacyAtlassianHost   = "ATLASSIAN_HOST"
	LegacyAtlassianToken  = "ATLASSIAN_API_TOKEN"
	LegacyAtlassianEmail  = "ATLASSIAN_EMAIL"
	LegacyAtlassianScoped = "ATLASSIAN_API_TOKEN_SCOPED"
	LegacyGitHubToken     = "GITHUB_TOKEN"
	LegacyGitHubTokenAlt  = "GH_TOKEN"
)

// ImportLegacyEnv adopts the flat credential variables as organisations,
// once, for a provider that has none configured yet. It is deliberately
// not an error for env to be empty or incomplete: a machine that never
// configured a provider simply gets nothing, and is told to run setup.
//
// A provider that already has an organisation is left alone, so this can
// run on every start without ever undoing a later `punakawan setup`.
func ImportLegacyEnv(store *Store, env map[string]string) ([]Org, error) {
	var imported []Org

	if host := strings.TrimSpace(env[LegacyAtlassianHost]); host != "" && strings.TrimSpace(env[LegacyAtlassianToken]) != "" {
		existing, err := store.ListFor(ProviderJira)
		if err != nil {
			return nil, err
		}
		if len(existing) == 0 {
			id, err := DeriveOrgID(ProviderJira, host)
			if err != nil {
				return nil, err
			}
			baseURL, err := NormalizeBaseURL(host)
			if err != nil {
				return nil, err
			}
			org := Org{
				ID:          id,
				Provider:    ProviderJira,
				BaseURL:     baseURL,
				Email:       strings.TrimSpace(env[LegacyAtlassianEmail]),
				Token:       strings.TrimSpace(env[LegacyAtlassianToken]),
				TokenScoped: parseLegacyScoped(env),
				AddedAt:     time.Now().UTC(),
			}
			if err := store.Put(org); err != nil {
				return nil, err
			}
			imported = append(imported, org)
		}
	}

	token := strings.TrimSpace(env[LegacyGitHubToken])
	if token == "" {
		token = strings.TrimSpace(env[LegacyGitHubTokenAlt])
	}
	if token != "" {
		existing, err := store.ListFor(ProviderGitHub)
		if err != nil {
			return nil, err
		}
		if len(existing) == 0 {
			// A flat GITHUB_TOKEN names no organisation, so the imported
			// entry is the token's own account scope rather than a claim
			// about which organisation it belongs to.
			org := Org{
				ID:       "default",
				Provider: ProviderGitHub,
				BaseURL:  "https://github.com",
				Token:    token,
				AddedAt:  time.Now().UTC(),
			}
			if err := store.Put(org); err != nil {
				return nil, err
			}
			imported = append(imported, org)
		}
	}

	return imported, nil
}

// parseLegacyScoped mirrors the adapter's own reading of the scoped flag:
// unset means "scoped unless an email is configured", since a personal
// token with an email uses the site URL and Basic auth.
func parseLegacyScoped(env map[string]string) bool {
	switch strings.ToLower(strings.TrimSpace(env[LegacyAtlassianScoped])) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	return strings.TrimSpace(env[LegacyAtlassianEmail]) == ""
}
