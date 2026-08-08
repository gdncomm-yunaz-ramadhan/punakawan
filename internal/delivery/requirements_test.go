package delivery

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func createTestOrchestration(t *testing.T, s *Store) *protocol.DeliveryOrchestration {
	t.Helper()
	orch, err := s.CreateOrchestration(context.Background(), "orch-"+NewID(), NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	return orch
}

// TestFixturesCoverEveryProviderType exercises acceptance criterion 1
// (Jira parent/subtask, Confluence, GitHub, URL, free-text) at the
// normalization layer: each provider's already-retrieved metadata
// captures into a distinct canonical requirement source.
func TestFixturesCoverEveryProviderType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)

	fixtures := []SourceInput{
		{Provider: "jira", ExternalID: "PAY-1842", Title: "Refund API"},
		{Provider: "jira", ExternalID: "PAY-1843", Title: "Refund subtask", ParentKey: "PAY-1842"},
		{Provider: "confluence", ExternalID: "123456", Title: "Refund design doc"},
		{Provider: "github", ExternalID: "acme/checkout#42", Title: "Refund PR"},
		{Provider: "url", URL: "https://example.com/spec", Title: "External spec"},
		{Provider: "freetext", Title: "Ad-hoc note", Summary: "handle refunds for EU"},
	}

	seen := map[string]bool{}
	var subtaskID, parentID string
	for _, f := range fixtures {
		src, err := s.CaptureRequirement(ctx, "capture-"+NewID(), orch.Id, f)
		if err != nil {
			t.Fatalf("CaptureRequirement(%s): %v", f.Provider, err)
		}
		if seen[src.CanonicalKey] {
			t.Fatalf("duplicate canonical_key %q across distinct fixtures", src.CanonicalKey)
		}
		seen[src.CanonicalKey] = true
		if f.ExternalID == "PAY-1842" {
			parentID = src.Id
		}
		if f.ExternalID == "PAY-1843" {
			subtaskID = src.Id
		}
	}

	subtask, err := s.GetRequirementSource(ctx, orch.Id, subtaskID)
	if err != nil {
		t.Fatalf("GetRequirementSource(subtask): %v", err)
	}
	if subtask.ParentSourceId == nil || *subtask.ParentSourceId != parentID {
		t.Fatalf("jira subtask ParentSourceId = %v, want %s", subtask.ParentSourceId, parentID)
	}
}

// TestPinnedCanonicalKeyResistsSimilarWording confirms acceptance
// criterion 8: a differently-keyed source with similar title text is
// never merged into an already-pinned one, while re-capturing the exact
// same canonical_key with identical content is a harmless no-op.
func TestPinnedCanonicalKeyResistsSimilarWording(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	orch := createTestOrchestration(t, s)

	original, err := s.CaptureRequirement(ctx, "cap-1", orch.Id, SourceInput{Provider: "jira", ExternalID: "PAY-1842", Title: "Refund API for EU checkout"})
	if err != nil {
		t.Fatalf("capture original: %v", err)
	}

	similar, err := s.CaptureRequirement(ctx, "cap-2", orch.Id, SourceInput{Provider: "jira", ExternalID: "PAY-9999", Title: "Refund API for EU checkout"})
	if err != nil {
		t.Fatalf("capture similar: %v", err)
	}
	if similar.Id == original.Id || similar.CanonicalKey == original.CanonicalKey {
		t.Fatalf("similarly-worded source must not be merged into the pinned one: original=%+v similar=%+v", original, similar)
	}

	reCaptured, err := s.CaptureRequirement(ctx, "cap-3", orch.Id, SourceInput{Provider: "jira", ExternalID: "PAY-1842", Title: "Refund API for EU checkout"})
	if err != nil {
		t.Fatalf("re-capture identical: %v", err)
	}
	if reCaptured.Id != original.Id || reCaptured.Revision != original.Revision {
		t.Fatalf("identical re-capture must be a no-op: original=%+v reCaptured=%+v", original, reCaptured)
	}

	updated, err := s.CaptureRequirement(ctx, "cap-4", orch.Id, SourceInput{Provider: "jira", ExternalID: "PAY-1842", Title: "Refund API for EU checkout, revised"})
	if err != nil {
		t.Fatalf("re-capture with changed content: %v", err)
	}
	if updated.Id != original.Id {
		t.Fatalf("content update must preserve the source's id: original=%s updated=%s", original.Id, updated.Id)
	}
	if updated.Revision <= original.Revision {
		t.Fatalf("content update must advance revision: original=%d updated=%d", original.Revision, updated.Revision)
	}
}
