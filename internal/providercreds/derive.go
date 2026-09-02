package providercreds

import (
	"fmt"
	"net/url"
	"strings"
)

// DeriveOrgID works out which organisation a site URL belongs to, so a
// person configuring one is asked for the URL they already know rather
// than an identifier they would have to invent - and so two people
// configuring the same site always arrive at the same id, which free text
// did not guarantee.
//
// Jira:
//
//	https://team.atlassian.net        -> team
//	https://jira.acme.com             -> acme
//	https://issues.acme.co.uk/jira    -> acme
//
// GitHub, where the organisation is in the path on github.com and in the
// host on an enterprise install:
//
//	https://github.com/acme           -> acme
//	https://github.com/acme/repo      -> acme
//	https://github.acme.com           -> acme
func DeriveOrgID(provider Provider, raw string) (string, error) {
	u, err := parseSiteURL(raw)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("providercreds: %q has no hostname", raw)
	}

	if provider == ProviderGitHub {
		if segment := firstPathSegment(u.Path); segment != "" {
			return NormalizeOrgID(segment), nil
		}
		if host == "github.com" || host == "www.github.com" {
			return "", fmt.Errorf("providercreds: %q names no organisation; use the organisation's own URL, for example https://github.com/acme", raw)
		}
	}

	labels := strings.Split(host, ".")
	// A site reached through a service-shaped prefix is named by what
	// follows it: jira.acme.com is acme's Jira, not an organisation called
	// jira.
	for len(labels) > 1 && isServicePrefix(labels[0]) {
		labels = labels[1:]
	}
	if labels[0] == "" {
		return "", fmt.Errorf("providercreds: cannot derive an organisation from %q", raw)
	}
	return NormalizeOrgID(labels[0]), nil
}

// NormalizeBaseURL is the site URL as it should be stored: scheme and
// host only, since everything below that is a page, not a site.
func NormalizeBaseURL(raw string) (string, error) {
	u, err := parseSiteURL(raw)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host, nil
}

func parseSiteURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("providercreds: a site URL is required")
	}
	// A person types team.atlassian.net far more often than they type the
	// scheme, and rejecting that would be pedantry rather than safety.
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("providercreds: %q is not a valid URL: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("providercreds: %q must be an http(s) URL", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("providercreds: %q has no hostname", raw)
	}
	return u, nil
}

func firstPathSegment(path string) string {
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			return segment
		}
	}
	return ""
}

// isServicePrefix reports whether a leading host label names the service
// rather than the organisation running it.
func isServicePrefix(label string) bool {
	switch label {
	case "jira", "issues", "tickets", "atlassian", "github", "git", "www", "code":
		return true
	}
	return false
}
