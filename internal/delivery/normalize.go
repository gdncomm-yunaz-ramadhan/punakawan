package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// SourceInput is caller-supplied, already-retrieved metadata for one
// requirement source. Retrieval itself (calling Jira/Confluence/GitHub)
// is an adapter concern (internal/adapters, packages/adapter-*) owned
// elsewhere; this package only normalizes, canonicalizes, and grounds
// what a caller already fetched.
type SourceInput struct {
	Provider   string // jira | confluence | github | url | freetext
	ExternalID string // issue key, page id, "owner/repo#123"; empty for freetext
	URL        string // canonical source URL, if any
	Title      string
	Summary    string
	ParentKey  string // Jira/GitHub parent issue key/number, if this is a subtask
	// Tenant scopes a jira source to one connected adapter instance, so
	// the same issue key from two different Jira sites is never treated
	// as the same requirement source. Left empty (every pre-existing
	// caller of this type), a jira canonical key keeps its original
	// tenant-less "jira:<KEY>" shape exactly as before.
	Tenant string
}

// CanonicalKey returns the exact, provider-specific identifier used to
// dedup and pin a requirement source. It is never derived from title or
// summary text, so a similarly-worded but distinct source can never be
// mistaken for an already-pinned one.
func CanonicalKey(in SourceInput) (string, error) {
	switch in.Provider {
	case "jira":
		key := strings.ToUpper(strings.TrimSpace(in.ExternalID))
		if !jiraKeyPattern.MatchString(key) {
			return "", fmt.Errorf("delivery: jira source requires a valid external_id (issue key)")
		}
		if tenant := strings.TrimSpace(in.Tenant); tenant != "" {
			return "jira:" + tenant + ":" + key, nil
		}
		return "jira:" + key, nil
	case "confluence":
		if in.ExternalID == "" {
			return "", fmt.Errorf("delivery: confluence source requires external_id (page id)")
		}
		return "confluence:" + in.ExternalID, nil
	case "github":
		if in.ExternalID == "" {
			return "", fmt.Errorf("delivery: github source requires external_id (owner/repo#number)")
		}
		return "github:" + in.ExternalID, nil
	case "url":
		normalized, err := normalizeURL(in.URL)
		if err != nil {
			return "", err
		}
		return "url:" + normalized, nil
	case "freetext":
		if strings.TrimSpace(in.Title+in.Summary) == "" {
			return "", fmt.Errorf("delivery: freetext source requires title or summary")
		}
		return "freetext:" + contentDigest(in.Title, in.Summary)[:16], nil
	default:
		return "", fmt.Errorf("delivery: unknown source provider %q", in.Provider)
	}
}

// normalizeURL lowercases scheme/host and strips a trailing slash and
// fragment, so trivially-equivalent URLs dedup exactly while remaining
// an exact (not fuzzy) match.
func normalizeURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("delivery: url source requires a url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("delivery: parse url %q: %w", raw, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}

// contentHash returns the sha256:<hex> digest of a source's content
// fields (evidence.schema.json's existing convention), used to detect
// when a re-captured canonical_key's content actually changed.
func contentHash(in SourceInput) string {
	return "sha256:" + contentDigest(in.Title, in.Summary)
}

func contentDigest(fields ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}

// jiraKeyPattern matches the conventional Jira issue key shape
// (uppercase project key, hyphen, number) - e.g. "PAY-1842".
var jiraKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[0-9]+$`)

// githubShortRefPattern matches the conventional "owner/repo#number"
// shorthand for a GitHub issue or pull request.
var githubShortRefPattern = regexp.MustCompile(`^[\w.-]+/[\w.-]+#[0-9]+$`)

// ClassifyReference guesses a bare, unstructured reference string's
// SourceInput shape, for a caller (start_delivery and the `punakawan
// deliver` CLI) that only has a plain string rather than an
// already-typed SourceInput an adapter fetched. It reports confident
// only when the string unambiguously matches one recognized shape: an
// absolute http(s) URL, a Jira-style PROJECT-123 key, or a GitHub
// owner/repo#123 short reference. Everything else - including a bare
// Confluence page id, which has no distinguishable shape from an
// arbitrary number, and plain free text, which is indistinguishable
// from an unrecognized reference the caller simply mistyped - comes
// back not confident. Guessing wrong here would silently mis-file a
// requirement under the wrong provider (or invent a freetext source
// nobody asked for), so an unclear reference is left for the caller to
// resolve explicitly instead (grounded truth over confident
// performance).
func ClassifyReference(ref string) (SourceInput, bool) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return SourceInput{}, false
	}
	if u, err := url.Parse(trimmed); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return SourceInput{Provider: "url", URL: trimmed, Title: trimmed}, true
	}
	if jiraKeyPattern.MatchString(trimmed) {
		return SourceInput{Provider: "jira", ExternalID: trimmed, Title: trimmed}, true
	}
	if githubShortRefPattern.MatchString(trimmed) {
		return SourceInput{Provider: "github", ExternalID: trimmed, Title: trimmed}, true
	}
	return SourceInput{}, false
}
