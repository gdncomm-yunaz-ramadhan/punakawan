package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
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
}

// CanonicalKey returns the exact, provider-specific identifier used to
// dedup and pin a requirement source. It is never derived from title or
// summary text, so a similarly-worded but distinct source can never be
// mistaken for an already-pinned one (punokawan-14yn.2 acceptance
// criterion 8).
func CanonicalKey(in SourceInput) (string, error) {
	switch in.Provider {
	case "jira":
		if in.ExternalID == "" {
			return "", fmt.Errorf("delivery: jira source requires external_id (issue key)")
		}
		return "jira:" + in.ExternalID, nil
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
