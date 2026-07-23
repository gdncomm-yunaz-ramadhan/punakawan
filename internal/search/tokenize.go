// Package search implements the local, embeddings-free knowledge search
// pipeline from punakawan-architecture-enhancement-plan.md §11: a technical
// tokenizer (§11.6), exact-identifier recognition (§11.3), and a Bleve-backed
// BM25F index (§11.4/§11.11) over internal/knowledge.Store's records.
package search

import "regexp"

// camelBoundary matches a lower-or-digit character immediately followed by
// an upper-case character - the boundary inside "productMapper" or
// "java8Time" (but not "java8time", which has no upper-case letter to
// transition into).
var camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// acronymBoundary matches a run of upper-case letters immediately followed
// by an upper-case-then-lower-case pair - the boundary inside "URLParser"
// that keeps "URL" together instead of exploding it into single letters.
var acronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)

// delimiterSplit splits s on any of the given single-byte delimiters,
// dropping empty parts (consecutive delimiters, or a delimiter at either
// end, produce no empty-string tokens).
func delimiterSplit(s string, delims string) []string {
	var parts []string
	start := 0
	for i, r := range s {
		if r < 128 && containsByte(delims, byte(r)) {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

func containsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// splitCase splits word on camelCase/PascalCase boundaries, preserving
// acronym runs (URLParser -> URL, Parser; not U, R, L, Parser).
func splitCase(word string) []string {
	spaced := camelBoundary.ReplaceAllString(word, "$1 $2")
	spaced = acronymBoundary.ReplaceAllString(spaced, "$1 $2")
	return delimiterSplit(spaced, " ")
}

// Tokenize implements §11.6's technical tokenization: it splits text into
// whitespace-separated words, then recursively decomposes each word along
// colon (dependency coordinates), slash (paths), dot (package names), dash
// and underscore (kebab/snake case), and camelCase/PascalCase boundaries -
// emitting every intermediate composite alongside the fully-decomposed leaf
// tokens. The original word and a lower-cased copy are always included, so
// exact-case identifiers (a Java class, an API route) remain matchable
// verbatim even after decomposition. Numeric runs are never split off their
// adjoining letters (java8time stays whole), since that is not a case,
// case, or structural-delimiter boundary - splitting it would be exactly the
// aggressive stemming §11.6 says not to do.
func Tokenize(text string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(tok string) {
		if tok == "" || seen[tok] {
			return
		}
		seen[tok] = true
		out = append(out, tok)
	}

	for _, word := range delimiterSplit(text, " \t\n\r") {
		tokenizeWord(word, add)
	}
	return out
}

func tokenizeWord(word string, add func(string)) {
	if word == "" {
		return
	}
	add(word)
	add(toLowerASCII(word))

	for _, colonPart := range delimiterSplit(word, ":") {
		if colonPart != word {
			add(colonPart)
		}
		for _, slashPart := range delimiterSplit(colonPart, "/") {
			if slashPart != colonPart {
				add(slashPart)
			}
			for _, dotPart := range delimiterSplit(slashPart, ".") {
				if dotPart != slashPart {
					add(dotPart)
				}
				for _, dashPart := range delimiterSplit(dotPart, "-_") {
					if dashPart != dotPart {
						add(dashPart)
					}
					for _, leaf := range splitCase(dashPart) {
						add(leaf)
						add(toLowerASCII(leaf))
					}
				}
			}
		}
	}
}

func toLowerASCII(s string) string {
	b := []byte(s)
	changed := false
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
			changed = true
		}
	}
	if !changed {
		return s
	}
	return string(b)
}
