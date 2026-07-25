package contradiction

import (
	"strings"
	"unicode"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// NormalizeKey canonicalizes a subject key for deterministic comparison, per
// §20 / CONTRA-012. Matching contradictions to their subject must be exact and
// explainable - NO embeddings, no fuzzy similarity - so the same config key,
// id, or path written with different casing, surrounding whitespace, or
// punctuation/separator style compares equal. It lowercases, then collapses
// every run of non-alphanumeric characters (dots, underscores, slashes,
// spaces, ...) to a single space and trims, so e.g.
// "Payout.Retry.Max_Attempts" and "payout retry max attempts" normalize to the
// same value. It is deliberately punctuation-insensitive because the same
// logical key is routinely spelled `a.b.c`, `a_b_c`, or `a/b/c` across config,
// code, and prose.
func NormalizeKey(s string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
			continue
		}
		// Any non-alphanumeric run becomes at most one separating space; a
		// leading run produces none because b is still empty.
		pendingSpace = true
	}
	return b.String()
}

// FindCandidates returns existing records whose subject matches subjectType and
// whose subject.key normalizes equal to key, so a caller detecting a new
// contradiction can find an already-recorded one for the same subject instead
// of creating a duplicate (§20 / CONTRA-012's deterministic matching). A record
// with no subject.key never matches (there is nothing to compare against).
// subjectType is compared as-is against each record's subject.type.
func FindCandidates(root, subjectType, key string) ([]protocol.Contradiction, error) {
	records, err := List(root)
	if err != nil {
		return nil, err
	}
	target := NormalizeKey(key)
	out := make([]protocol.Contradiction, 0)
	for _, c := range records {
		if string(c.Subject.Type) != subjectType {
			continue
		}
		if c.Subject.Key == nil {
			continue
		}
		if NormalizeKey(*c.Subject.Key) == target {
			out = append(out, c)
		}
	}
	return out, nil
}
