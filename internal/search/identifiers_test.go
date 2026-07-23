package search

import "testing"

func TestDetectIdentifiersRecognizesEachKind(t *testing.T) {
	cases := []struct {
		word string
		kind IdentifierKind
	}{
		{"CVE-2026-12345", IdentifierKindCVE},
		{"GHSA-abcd-1234-efgh", IdentifierKindGHSA},
		{"java:S3776", IdentifierKindSonarRule},
		{"SETARA-142", IdentifierKindJiraKey},
		{"punokawan-67q", IdentifierKindBDTask},
		{"#42", IdentifierKindPullRequest},
		{"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", IdentifierKindGitHash},
		{"v2.6.0", IdentifierKindVersionString},
		{"@types/node", IdentifierKindNpmPackage},
		{"github.com/ygrip/punakawan", IdentifierKindGoModule},
		{"org.thymeleaf.extras:thymeleaf-extras-java8time", IdentifierKindMavenCoord},
		{"src/main/java/com/example/PermissionService.java", IdentifierKindFilePath},
		{"/api/v1/orders/:id", IdentifierKindApiRoute},
		{"PermissionService.validateAccess", IdentifierKindJavaClassOrMethod},
		{"BrsProductRecommendationMapper", IdentifierKindSymbol},
	}

	for _, c := range cases {
		got := DetectIdentifiers(c.word)
		if len(got) != 1 || got[0].Kind != c.kind || got[0].Value != c.word {
			t.Errorf("DetectIdentifiers(%q) = %+v, want exactly one %s match for %q", c.word, got, c.kind, c.word)
		}
	}
}

func TestDetectIdentifiersDoesNotFlagOrdinaryWords(t *testing.T) {
	got := DetectIdentifiers("please review the refund logic")
	if len(got) != 0 {
		t.Fatalf("DetectIdentifiers(ordinary words) = %+v, want no matches", got)
	}
}

func TestDetectIdentifiersDoesNotMistakePlainNumberForGitHash(t *testing.T) {
	got := DetectIdentifiers("1234567890")
	if len(got) != 0 {
		t.Fatalf("DetectIdentifiers(plain decimal number) = %+v, want no git-hash match", got)
	}
}

func TestDetectIdentifiersFindsMultipleInOneQuery(t *testing.T) {
	got := DetectIdentifiers("fix CVE-2026-12345 referenced in SETARA-142")
	if len(got) != 2 {
		t.Fatalf("DetectIdentifiers(...) = %+v, want 2 matches", got)
	}
}

func TestDetectIdentifiersDoesNotConfuseSonarRuleWithMavenCoordinate(t *testing.T) {
	got := DetectIdentifiers("java:S3776")
	if len(got) != 1 || got[0].Kind != IdentifierKindSonarRule {
		t.Fatalf("DetectIdentifiers(java:S3776) = %+v, want a sonar-rule match, not maven-coordinate", got)
	}
}
