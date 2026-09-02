package delivery

import (
	"net/url"
	"strings"
)

// RepositoryIdentity returns a stable lookup key for common Git remote URL
// forms. It deliberately preserves repository-path casing because not every
// Git host treats paths case-insensitively.
func RepositoryIdentity(repositoryURL string) string {
	host, path, ok := repositoryLocation(repositoryURL)
	if !ok {
		return trimRepositoryURL(repositoryURL)
	}
	return host + "/" + path
}

// repositoryURLVariants returns the raw remote plus equivalent common forms.
// Rows created before repository_identity existed are still found through the
// indexed repository_url column until their next upsert records the identity.
func repositoryURLVariants(repositoryURL string) []string {
	raw := strings.TrimSpace(repositoryURL)
	host, path, ok := repositoryLocation(raw)
	if !ok {
		return []string{raw}
	}

	base := host + "/" + path
	pathWithGit := path
	if !strings.HasSuffix(strings.ToLower(pathWithGit), ".git") {
		pathWithGit += ".git"
	}
	variants := []string{
		raw,
		"https://" + base,
		"https://" + host + "/" + pathWithGit,
		"http://" + base,
		"http://" + host + "/" + pathWithGit,
		"git@" + host + ":" + path,
		"git@" + host + ":" + pathWithGit,
		"ssh://git@" + base,
		"ssh://git@" + host + "/" + pathWithGit,
	}
	return uniqueRepositoryURLs(variants)
}

func repositoryLocation(repositoryURL string) (host, path string, ok bool) {
	raw := strings.TrimSpace(repositoryURL)
	if raw == "" {
		return "", "", false
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "ssh", "git":
			return normalizeRepositoryLocation(parsed.Host, parsed.Path)
		}
	}
	if at := strings.LastIndexByte(raw, '@'); at >= 0 {
		if colon := strings.IndexByte(raw[at+1:], ':'); colon >= 0 {
			return normalizeRepositoryLocation(raw[at+1:at+1+colon], raw[at+2+colon:])
		}
	}
	return "", "", false
}

func normalizeRepositoryLocation(host, path string) (string, string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = trimGitSuffix(path)
	if host == "" || path == "" {
		return "", "", false
	}
	return host, path, true
}

func trimRepositoryURL(repositoryURL string) string {
	return trimGitSuffix(strings.TrimRight(strings.TrimSpace(repositoryURL), "/"))
}

func trimGitSuffix(path string) string {
	if len(path) >= len(".git") && strings.EqualFold(path[len(path)-len(".git"):], ".git") {
		return path[:len(path)-len(".git")]
	}
	return path
}

func uniqueRepositoryURLs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
