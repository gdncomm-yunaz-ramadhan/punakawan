package delivery

import (
	"context"
	"testing"
)

func TestJiraFieldMappingCachesAndRefreshesExactContext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.UpsertJiraFieldMapping(ctx, "field-create", "cloud-1", "TRF", "10002", StoryPointsFieldPurpose, "6", "Story Points")
	if err != nil {
		t.Fatalf("UpsertJiraFieldMapping: %v", err)
	}
	if created.FieldID != "6" {
		t.Fatalf("created field id = %q, want 6", created.FieldID)
	}

	cached, err := s.GetJiraFieldMapping(ctx, "cloud-1", "TRF", "10002", StoryPointsFieldPurpose)
	if err != nil {
		t.Fatalf("GetJiraFieldMapping: %v", err)
	}
	if cached.FieldID != "6" || cached.FieldName != "Story Points" {
		t.Fatalf("cached mapping = %+v, want field 6 Story Points", cached)
	}

	refreshed, err := s.UpsertJiraFieldMapping(ctx, "field-refresh", "cloud-1", "TRF", "10002", StoryPointsFieldPurpose, "customfield_10101", "Delivery points")
	if err != nil {
		t.Fatalf("refresh mapping: %v", err)
	}
	if refreshed.FieldID != "customfield_10101" || refreshed.FieldName != "Delivery points" {
		t.Fatalf("refreshed mapping = %+v", refreshed)
	}
}
