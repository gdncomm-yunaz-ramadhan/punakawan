package recipe

import (
	"fmt"
	"regexp"
)

// secretPatterns recognizes common credential shapes so a value that looks
// like one never survives into durable knowledge (task q9r.7 #4; plan §13:
// "Do not store provider credentials, authentication headers, or secret
// field values"; §20's acceptance criterion "Recipes never store
// credentials or secret headers"). This is deliberately a narrow,
// well-known-shape allow-list rather than a general entropy/secret
// scanner: false negatives on an exotic token format are a smaller risk
// than false positives silently mangling a legitimate sprint name or
// ticket key that happens to look tokenish.
var secretPatterns = []*regexp.Regexp{
	// RFC 6750 bearer/basic auth header values, however they end up in a
	// map (e.g. a caller mistakenly passing a whole header as a binding).
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\bbasic\s+[a-z0-9+/=]{8,}`),
	// Atlassian API tokens (ATATT3...) and personal access tokens.
	regexp.MustCompile(`\bATATT3[A-Za-z0-9_-]{10,}`),
	// GitHub tokens (ghp_, gho_, ghu_, ghs_, ghr_, github_pat_).
	regexp.MustCompile(`\bgh[oprsu]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	// Generic long high-entropy-looking API key assignment, e.g.
	// api_key=..., token: "...", secret=... - conservative on length to
	// avoid flagging short, ordinary field values.
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd)\b\s*[:=]\s*["']?[a-z0-9._-]{16,}`),
	// AWS access key ids.
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	// Generic private key PEM headers.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
}

// SecretShapedValueError is returned when a value that looks like a
// credential was about to be persisted somewhere in a recipe record.
type SecretShapedValueError struct {
	// Path names where the value was found, e.g. "bindings[api_token]" or
	// "selector.value.resolver.arguments[header]", for an actionable error
	// message without echoing the offending value itself.
	Path string
}

func (e *SecretShapedValueError) Error() string {
	return fmt.Sprintf("recipe: refusing to persist secret-shaped value at %s (recipes must never store credentials or secret headers, plan §13/§20)", e.Path)
}

// looksLikeSecret reports whether s matches one of secretPatterns.
func looksLikeSecret(s string) bool {
	for _, p := range secretPatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// CheckNoSecrets walks an arbitrary free-form map (as accepted by
// bindings/resolver arguments - the only two genuinely open-ended
// map[string]interface{} shapes retrieval_recipe's schema allows; see this
// package's fingerprint/secrets test file for how that was confirmed
// against protocol/knowledge.schema.json) and returns a
// *SecretShapedValueError for the first value that looks like a
// credential. pathPrefix labels the error (e.g. "bindings").
//
// This is a boundary guard, not a content filter: it does not sanitize or
// strip the value, because a half-redacted binding could compile into a
// silently wrong query. It fails the call outright so the caller learns
// immediately that this input cannot be stored, per §13's flat
// prohibition rather than a best-effort scrub.
func CheckNoSecrets(pathPrefix string, values map[string]interface{}) error {
	for k, v := range values {
		if err := checkValueNoSecrets(fmt.Sprintf("%s[%s]", pathPrefix, k), v); err != nil {
			return err
		}
	}
	return nil
}

func checkValueNoSecrets(path string, v interface{}) error {
	switch t := v.(type) {
	case string:
		if looksLikeSecret(t) {
			return &SecretShapedValueError{Path: path}
		}
	case map[string]interface{}:
		for k, vv := range t {
			if err := checkValueNoSecrets(fmt.Sprintf("%s.%s", path, k), vv); err != nil {
				return err
			}
		}
	case []interface{}:
		for i, vv := range t {
			if err := checkValueNoSecrets(fmt.Sprintf("%s[%d]", path, i), vv); err != nil {
				return err
			}
		}
	}
	return nil
}
