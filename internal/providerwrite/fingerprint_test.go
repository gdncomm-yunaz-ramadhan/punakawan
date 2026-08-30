package providerwrite

import "testing"

func TestFingerprintsAreStableAndOrderIndependentForSets(t *testing.T) {
	if got, want := JiraCommentFingerprint("d1", "delivery.completed", "ABC-1"), "jira.comment:d1:delivery.completed:ABC-1"; got != want {
		t.Fatalf("JiraCommentFingerprint = %q, want %q", got, want)
	}
	if got, want := JiraTransitionFingerprint("d1", "ABC-1", "In Progress", "Done"), "jira.transition:d1:ABC-1:In Progress:Done"; got != want {
		t.Fatalf("JiraTransitionFingerprint = %q, want %q", got, want)
	}
	if got, want := JiraWorklogFingerprint("wl-1"), "jira.worklog:wl-1"; got != want {
		t.Fatalf("JiraWorklogFingerprint = %q, want %q", got, want)
	}
	if got, want := GitHubResolveThreadFingerprint("thread-1"), "github.resolve-thread:thread-1"; got != want {
		t.Fatalf("GitHubResolveThreadFingerprint = %q, want %q", got, want)
	}

	a := GitHubLabelsFingerprint("acme/widgets", 7, []string{"bug", "urgent"})
	b := GitHubLabelsFingerprint("acme/widgets", 7, []string{"urgent", "bug"})
	if a != b {
		t.Fatalf("GitHubLabelsFingerprint must be order-independent: %q != %q", a, b)
	}
	c := GitHubLabelsFingerprint("acme/widgets", 7, []string{"bug"})
	if a == c {
		t.Fatal("GitHubLabelsFingerprint must differ for a different label set")
	}
}

func TestJiraCreateSubtaskFingerprintNormalizesSummary(t *testing.T) {
	a := JiraCreateSubtaskFingerprint("d1", "ABC-1", "  Fix   the Bug ")
	b := JiraCreateSubtaskFingerprint("d1", "ABC-1", "fix the bug")
	if a != b {
		t.Fatalf("expected whitespace/case-insensitive normalization: %q != %q", a, b)
	}
}
