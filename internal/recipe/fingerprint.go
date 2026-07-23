package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// InstanceFingerprint identifies *which* Jira instance a recipe was
// validated against, without ever containing a credential (task q9r.7 #1:
// "provider-instance compatibility checks... without storing secrets").
//
// A recipe's selector (project keys, component names, custom field IDs) is
// only meaningful relative to one Jira site's schema. Two different Jira
// Cloud instances (or a Cloud vs. Data Center instance) can use the same
// project key for entirely different projects, so a recipe validated
// against instance A must never be silently reused against instance B just
// because its capability/provider/resource/operation/scope all match -
// that is the exact staleness class this type guards against.
//
// Host is packages/adapter-atlassian's `restClient.ts` config.host (e.g.
// "example.atlassian.net"), the same value that (via resolveCloudId) is
// hashed together with CloudID below - never a token, never a header, only
// public site-identity strings a browser URL bar would also show.
type InstanceFingerprint struct {
	// Host is the Jira site hostname, e.g. "example.atlassian.net".
	Host string
	// CloudID is Atlassian's opaque per-site identifier (resolveCloudId in
	// restClient.ts), stable across a rename of Host but unique per
	// instance - the strongest available signal without touching auth.
	CloudID string
}

// String renders a fingerprint into the recipe schema's
// provider_instance_fingerprint string field (§4's
// `provider_instance_fingerprint: jira-cloud-company` example is exactly
// this shape: human-readable, not itself a hash - a hash is used only when
// Host/CloudID must be redacted, see Hash below).
func (f InstanceFingerprint) String() string {
	host := strings.TrimSpace(f.Host)
	cloud := strings.TrimSpace(f.CloudID)
	switch {
	case host != "" && cloud != "":
		return fmt.Sprintf("jira:%s:%s", host, cloud)
	case cloud != "":
		return fmt.Sprintf("jira:cloud:%s", cloud)
	case host != "":
		return fmt.Sprintf("jira:%s", host)
	default:
		return ""
	}
}

// Hash returns a sha256 digest of String(), for callers that want a
// fixed-shape, non-reversible fingerprint (e.g. to avoid ever printing a
// customer's Jira hostname in a shared log) while keeping the same
// no-secrets guarantee - a hash of a hostname is exactly as safe to store
// as the hostname itself, just less legible.
func (f InstanceFingerprint) Hash() string {
	sum := sha256.Sum256([]byte(f.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// FingerprintMismatchError is returned when a recipe's stored
// provider_instance_fingerprint does not match the instance a caller is
// about to run it against. This must block automatic reuse exactly like
// an unrevalidated stale recipe (§12) - a matching field/selector schema
// on the wrong instance is not a compile error, so nothing else in this
// package would otherwise catch it.
type FingerprintMismatchError struct {
	RecipeID string
	Stored   string
	Current  string
}

func (e *FingerprintMismatchError) Error() string {
	return fmt.Sprintf("recipe: %q was validated against Jira instance %q, not the current instance %q - revalidate before reuse", e.RecipeID, e.Stored, e.Current)
}

// CheckInstanceFingerprint compares rec's stored validated-against
// fingerprint (if any) with current, returning a *FingerprintMismatchError
// when they disagree. A recipe with no stored fingerprint predates this
// check (or was validated by a caller that didn't supply one) and cannot
// be proven mismatched, so it is treated as compatible rather than
// blocked - the same "absence of a signal isn't proof of staleness"
// stance internal/knowledge.CheckStale takes for a record with no
// content_hash.
func CheckInstanceFingerprint(rec protocol.KnowledgeRecord, current InstanceFingerprint) error {
	if rec.RetrievalRecipe == nil || rec.RetrievalRecipe.Validation == nil {
		return nil
	}
	storedPtr := rec.RetrievalRecipe.Validation.ProviderInstanceFingerprint
	if storedPtr == nil || *storedPtr == "" {
		return nil
	}
	stored := *storedPtr
	curStr := current.String()
	if curStr == "" {
		// No current fingerprint to compare against (caller has no live
		// Jira connection info) - cannot prove a mismatch, same stance as
		// "no stored fingerprint" above.
		return nil
	}
	if stored != curStr {
		return &FingerprintMismatchError{RecipeID: rec.Id, Stored: stored, Current: curStr}
	}
	return nil
}
