// Package redact applies best-effort, pattern-based secret redaction to
// text before it leaves the process (e.g. an evidence preview response).
// This is not a security boundary against a determined leak - it is a
// pattern matcher against known credential shapes (AWS keys, GitHub/GitLab
// tokens, Atlassian tokens, JWTs, bearer headers, generic KEY=value
// assignments) - and will miss anything that doesn't match one of those
// shapes. It exists because evidence records can capture raw command
// output or external API responses, which may echo back a credential that
// was passed as an argument or environment variable.
package redact

import "regexp"

var patterns = []*regexp.Regexp{
	// AWS access key id.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// GitHub/GitLab personal access tokens.
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
	// Atlassian API tokens.
	regexp.MustCompile(`ATATT[A-Za-z0-9_=-]{20,}`),
	// OpenAI/Anthropic-shaped API keys.
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	// JSON Web Tokens (three base64url segments separated by dots).
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
	// Bearer/Basic authorization header values.
	regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9._~+/=-]{12,}`),
	// Generic KEY=value or KEY: value assignments where KEY looks secret-ish.
	regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|access[_-]?key)\s*[=:]\s*\S+`),
}

const redacted = "[REDACTED]"

// Text returns s with every substring matching a known secret shape
// replaced by "[REDACTED]".
func Text(s string) string {
	for _, p := range patterns {
		s = p.ReplaceAllString(s, redacted)
	}
	return s
}
