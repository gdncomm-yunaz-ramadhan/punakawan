package search

import (
	"regexp"
	"strings"
)

// IdentifierKind names the structured identifier family a token matched,
// per §11.3's recognized-patterns list.
type IdentifierKind string

const (
	IdentifierKindCVE               IdentifierKind = "cve"
	IdentifierKindGHSA              IdentifierKind = "ghsa"
	IdentifierKindSonarRule         IdentifierKind = "sonar-rule"
	IdentifierKindJiraKey           IdentifierKind = "jira-key"
	IdentifierKindBDTask            IdentifierKind = "bd-task"
	IdentifierKindPullRequest       IdentifierKind = "pull-request"
	IdentifierKindGitHash           IdentifierKind = "git-hash"
	IdentifierKindVersionString     IdentifierKind = "version-string"
	IdentifierKindMavenCoord        IdentifierKind = "maven-coordinate"
	IdentifierKindNpmPackage        IdentifierKind = "npm-package"
	IdentifierKindGoModule          IdentifierKind = "go-module"
	IdentifierKindFilePath          IdentifierKind = "file-path"
	IdentifierKindApiRoute          IdentifierKind = "api-route"
	IdentifierKindJavaClassOrMethod IdentifierKind = "java-class-or-method"
	IdentifierKindSymbol            IdentifierKind = "symbol"
)

// Identifier is one structured identifier §11.3 recognized in a query, kept
// alongside the exact substring it matched so callers can boost or display
// it verbatim.
type Identifier struct {
	Kind  IdentifierKind
	Value string
}

var (
	cvePattern               = regexp.MustCompile(`^(?i)CVE-\d{4}-\d{4,}$`)
	ghsaPattern              = regexp.MustCompile(`^(?i)GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}$`)
	sonarRulePattern         = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*:S\d+$`)
	jiraKeyPattern           = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-\d+$`)
	bdTaskPattern            = regexp.MustCompile(`^[a-z][a-z0-9]*-[a-z0-9]{2,}(\.\d+)?$`)
	pullRequestPattern       = regexp.MustCompile(`^#\d+$`)
	fullGitHashPattern       = regexp.MustCompile(`^(?i)[0-9a-f]{40}$|^(?i)[0-9a-f]{64}$`)
	shortGitHashPattern      = regexp.MustCompile(`^(?i)[0-9a-f]{7,39}$`)
	versionStringPattern     = regexp.MustCompile(`^v?\d+\.\d+(\.\d+)?(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	mavenCoordPattern        = regexp.MustCompile(`^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+(:[a-zA-Z0-9_.-]+)?$`)
	npmPackagePattern        = regexp.MustCompile(`^@[a-z0-9-]+/[a-z0-9._-]+$`)
	goModulePattern          = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z]{2,})+/[a-zA-Z0-9/_.-]+$`)
	javaClassOrMethodPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\.[A-Za-z_][A-Za-z0-9_]*$`)

	// fileExtensions lists extensions that make a slash-containing token a
	// file path rather than an API route - the two are otherwise
	// syntactically indistinguishable (both are "/word/word/word").
	fileExtensions = []string{
		".java", ".ts", ".tsx", ".js", ".jsx", ".go", ".py", ".rb", ".rs",
		".c", ".cc", ".cpp", ".h", ".hpp", ".md", ".json", ".yaml", ".yml",
		".sql", ".sh", ".toml", ".xml", ".html", ".css", ".proto",
	}
)

// hasHexLetter reports whether s contains at least one a-f/A-F character,
// used to avoid classifying a bare run of decimal digits as a git hash.
func hasHexLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			return true
		}
	}
	return false
}

func looksLikeGitHash(s string) bool {
	if fullGitHashPattern.MatchString(s) {
		return true
	}
	return shortGitHashPattern.MatchString(s) && hasHexLetter(s)
}

func hasKnownFileExtension(s string) bool {
	lower := strings.ToLower(s)
	for _, ext := range fileExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// looksLikeSymbol reports whether word is a single camelCase/PascalCase
// identifier (a Java/TS symbol name with no other structural delimiter) -
// i.e. splitCase decomposes it into more than one part, but it contains
// none of the delimiters the other, more specific patterns above already
// claim (: / . - _).
func looksLikeSymbol(word string) bool {
	for _, delim := range []string{":", "/", ".", "-", "_", "#"} {
		if strings.Contains(word, delim) {
			return false
		}
	}
	if !isAlpha(word) {
		return false
	}
	return len(splitCase(word)) > 1
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return len(s) > 0
}

// DetectIdentifiers implements §11.3's exact-identifier recognition: it
// splits text on whitespace and classifies each resulting word against an
// ordered list of structured-identifier patterns (most specific first, so a
// Sonar rule like "java:S3776" is not mistaken for a generic Maven
// coordinate). A word matching none of them is simply not an identifier -
// DetectIdentifiers reports only what it recognizes, leaving normal keyword
// matching to the BM25F stage.
func DetectIdentifiers(text string) []Identifier {
	var out []Identifier
	seen := make(map[Identifier]bool)
	add := func(kind IdentifierKind, value string) {
		id := Identifier{Kind: kind, Value: value}
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}

	for _, word := range delimiterSplit(text, " \t\n\r") {
		switch {
		case cvePattern.MatchString(word):
			add(IdentifierKindCVE, word)
		case ghsaPattern.MatchString(word):
			add(IdentifierKindGHSA, word)
		case sonarRulePattern.MatchString(word):
			add(IdentifierKindSonarRule, word)
		case jiraKeyPattern.MatchString(word):
			add(IdentifierKindJiraKey, word)
		case bdTaskPattern.MatchString(word):
			add(IdentifierKindBDTask, word)
		case pullRequestPattern.MatchString(word):
			add(IdentifierKindPullRequest, word)
		case looksLikeGitHash(word):
			add(IdentifierKindGitHash, word)
		case versionStringPattern.MatchString(word):
			add(IdentifierKindVersionString, word)
		case npmPackagePattern.MatchString(word):
			add(IdentifierKindNpmPackage, word)
		case goModulePattern.MatchString(word):
			add(IdentifierKindGoModule, word)
		case mavenCoordPattern.MatchString(word):
			add(IdentifierKindMavenCoord, word)
		case strings.Contains(word, "/") && hasKnownFileExtension(word):
			add(IdentifierKindFilePath, word)
		case strings.HasPrefix(word, "/"):
			add(IdentifierKindApiRoute, word)
		case javaClassOrMethodPattern.MatchString(word):
			add(IdentifierKindJavaClassOrMethod, word)
		case looksLikeSymbol(word):
			add(IdentifierKindSymbol, word)
		}
	}
	return out
}
