package search

import (
	"testing"
)

func tokenSet(tokens []string) map[string]bool {
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		set[t] = true
	}
	return set
}

func requireTokens(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := tokenSet(got)
	for _, w := range want {
		if !set[w] {
			t.Errorf("Tokenize(...) = %v, missing expected token %q", got, w)
		}
	}
}

func TestTokenizePreservesOriginalToken(t *testing.T) {
	got := Tokenize("PermissionService")
	requireTokens(t, got, "PermissionService", "permissionservice")
}

func TestTokenizeSplitsPascalCase(t *testing.T) {
	got := Tokenize("BrsProductRecommendationMapper")
	requireTokens(t, got, "BrsProductRecommendationMapper", "Brs", "Product", "Recommendation", "Mapper", "brs", "product", "recommendation", "mapper")
}

func TestTokenizeSplitsSnakeCase(t *testing.T) {
	got := Tokenize("validate_access_token")
	requireTokens(t, got, "validate_access_token", "validate", "access", "token")
}

func TestTokenizeSplitsKebabCase(t *testing.T) {
	got := Tokenize("setara-core")
	requireTokens(t, got, "setara-core", "setara", "core")
}

func TestTokenizeSplitsDottedPackageNames(t *testing.T) {
	got := Tokenize("org.thymeleaf.extras")
	requireTokens(t, got, "org.thymeleaf.extras", "org", "thymeleaf", "extras")
}

func TestTokenizeSplitsColonCoordinates(t *testing.T) {
	got := Tokenize("org.thymeleaf.extras:thymeleaf-extras-java8time")
	requireTokens(t, got,
		"org.thymeleaf.extras:thymeleaf-extras-java8time",
		"org.thymeleaf.extras",
		"thymeleaf-extras-java8time",
		"thymeleaf", "extras", "java8time",
	)
}

func TestTokenizeSplitsSlashPaths(t *testing.T) {
	got := Tokenize("src/main/java/com/example/PermissionService.java")
	requireTokens(t, got, "src", "main", "java", "com", "example", "PermissionService", "Permission", "Service")
}

func TestTokenizeDoesNotSplitDigitsFromLetters(t *testing.T) {
	got := Tokenize("java8time")
	set := tokenSet(got)
	if !set["java8time"] {
		t.Fatalf("Tokenize(java8time) = %v, expected the whole token java8time to survive intact", got)
	}
	if set["java"] || set["8time"] {
		t.Fatalf("Tokenize(java8time) = %v, must not split digits away from adjoining letters", got)
	}
}

func TestTokenizePreservesAcronymRuns(t *testing.T) {
	got := Tokenize("URLParser")
	requireTokens(t, got, "URLParser", "URL", "Parser")
	set := tokenSet(got)
	if set["U"] || set["R"] || set["L"] {
		t.Fatalf("Tokenize(URLParser) = %v, must not explode the URL acronym into single letters", got)
	}
}

func TestTokenizeHandlesMultipleWords(t *testing.T) {
	got := Tokenize("PermissionService validateAccess")
	requireTokens(t, got, "PermissionService", "validateAccess", "validate", "Access")
}

func TestTokenizeEmptyInputProducesNoTokens(t *testing.T) {
	if got := Tokenize(""); len(got) != 0 {
		t.Fatalf("Tokenize(\"\") = %v, want no tokens", got)
	}
}
