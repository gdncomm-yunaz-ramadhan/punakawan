package knowledge

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// testRetrievalRecipeRecord builds a KnowledgeRecord shaped like
// punakawan-procedural-knowledge-retrieval-recipe-plan-final.md §4's Jira
// next-sprint example, at the given lifecycle state. It exists so Recipe
// Phase 0's exit criterion - "recipe records can be stored and displayed
// as inert knowledge; no recipe can execute yet" - has a concrete fixture
// for every lifecycle state to round-trip against, rather than asserting
// the schema in the abstract.
func testRetrievalRecipeRecord(id string, state protocol.KnowledgeRecordValidityState) protocol.KnowledgeRecord {
	now := time.Now().UTC()
	literal := func(v string) interface{} {
		return map[string]interface{}{"literal": v}
	}
	resolver := func(name string, args map[string]interface{}) interface{} {
		return map[string]interface{}{"resolver": name, "arguments": args}
	}

	return protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRetrievalRecipe,
		Status: "active",
		Title:  "Find Affiliate Platform Jira work for the next sprint",
		Aliases: []string{
			"affiliate platform next sprint Jira",
			"project Jira scope",
			"TRF affiliate issues",
		},
		Scope: &protocol.KnowledgeRecordScope{Repository: strPtr("affiliate-api")},
		Source: protocol.KnowledgeRecordSource{
			Provider:    "user_instruction",
			RetrievedAt: now,
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      state,
			VerifiedBy: []string{"user"},
			VerifiedAt: &now,
		},
		Relations: []protocol.KnowledgeRecordRelationsElem{
			{Type: protocol.KnowledgeRecordRelationsElemTypeAppliesTo, Target: "workspace:affiliate-platform"},
			{Type: protocol.KnowledgeRecordRelationsElemTypeUsesAdapter, Target: "adapter:jira"},
		},
		RetrievalRecipe: &protocol.KnowledgeRecordRetrievalRecipe{
			Capability:    "jira.issue.search",
			Intent:        "project.next-sprint.issues",
			Provider:      "jira",
			Resource:      "issue",
			Operation:     "search",
			ReadOnly:      true,
			RecipeVersion: intPtr(3),
			AppliesTo: &protocol.KnowledgeRecordRetrievalRecipeAppliesTo{
				WorkspaceIds:  []string{"affiliate-platform"},
				RepositoryIds: []string{"affiliate-api", "affiliate-ui"},
			},
			Inputs: []protocol.KnowledgeRecordRetrievalRecipeInputsElem{
				{Name: "sprint_selector", Type: "sprint_selector", Required: boolPtr(true), Default: strPtr("next")},
			},
			Selector: protocol.KnowledgeRecordRetrievalRecipeSelector{
				All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
					{
						Field:    strPtr("project"),
						Operator: recipeOperatorPtr(protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals),
						Value:    literal("TRF"),
					},
					{
						Any: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElem{
							{
								Field:    strPtr("component"),
								Operator: (*protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperator)(strPtr("equals")),
								Value:    literal("AFFILIATE-PLATFORM"),
							},
							{
								Field:    strPtr("summary"),
								Operator: (*protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperator)(strPtr("phrase_contains")),
								Value:    literal("AFFILIATE PLATFORM"),
							},
						},
					},
					{
						Field:    strPtr("sprint"),
						Operator: recipeOperatorPtr(protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals),
						Value: resolver("jira.next_sprint", map[string]interface{}{
							"board": resolver("jira.board_for_project", map[string]interface{}{"project_key": "TRF"}),
						}),
					},
				},
			},
			Ordering: []protocol.KnowledgeRecordRetrievalRecipeOrderingElem{
				{Field: "rank", Direction: protocol.KnowledgeRecordRetrievalRecipeOrderingElemDirectionAscending},
			},
			Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
				EntityType:    "jira_issue",
				IdentityField: "key",
				Fields:        []string{"key", "summary", "status", "assignee", "priority", "sprint", "component"},
			},
			Validation: &protocol.KnowledgeRecordRetrievalRecipeValidation{
				Status:                      (*protocol.KnowledgeRecordRetrievalRecipeValidationStatus)(strPtr("passed")),
				ValidationId:                strPtr("val-20260723-0041"),
				ProviderInstanceFingerprint: strPtr("jira-cloud-company"),
				SampleSize:                  intPtr(20),
				AcceptedResultCount:         intPtr(14),
				AcceptedBy:                  strPtr("user"),
				AcceptedAt:                  &now,
				EvidenceIds:                 []string{"ev-jql-compile-001", "ev-jql-sample-001", "ev-user-acceptance-001"},
			},
		},
	}
}

func intPtr(v int) *int       { return &v }
func boolPtr(v bool) *bool    { return &v }
func strPtr(v string) *string { return &v }

func recipeOperatorPtr(op protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperator) *protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperator {
	return &op
}

func TestRetrievalRecipeRoundTrip(t *testing.T) {
	store := newTestStore(t)

	rec := testRetrievalRecipeRecord("pkw:recipe/affiliate-api/jira-next-sprint", protocol.KnowledgeRecordValidityStateVerified)
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetrievalRecipe == nil {
		t.Fatal("RetrievalRecipe = nil after round-trip")
	}
	if got.RetrievalRecipe.Capability != "jira.issue.search" {
		t.Fatalf("Capability = %q, want jira.issue.search", got.RetrievalRecipe.Capability)
	}
	if !got.RetrievalRecipe.ReadOnly {
		t.Fatal("ReadOnly = false, want true (§14: no execution engine can run a non-read-only recipe yet)")
	}
	if len(got.RetrievalRecipe.Selector.All) != 3 {
		t.Fatalf("Selector.All = %+v, want 3 top-level clauses", got.RetrievalRecipe.Selector.All)
	}
	nested := got.RetrievalRecipe.Selector.All[1]
	if len(nested.Any) != 2 {
		t.Fatalf("nested Any = %+v, want 2 leaf clauses (component, summary)", nested.Any)
	}
	if got.RetrievalRecipe.Validation == nil || got.RetrievalRecipe.Validation.AcceptedResultCount == nil || *got.RetrievalRecipe.Validation.AcceptedResultCount != 14 {
		t.Fatalf("Validation.AcceptedResultCount round-trip mismatch: %+v", got.RetrievalRecipe.Validation)
	}
}

// TestRetrievalRecipeFixturesForEveryLifecycleState stores and reads back
// one recipe per state in §12's table (draft, validating, verified, stale,
// disputed, superseded, invalid), proving every state is at minimum
// storable and displayable as inert knowledge - Phase 0's exit criterion -
// without any executor existing yet to act on it.
func TestRetrievalRecipeFixturesForEveryLifecycleState(t *testing.T) {
	store := newTestStore(t)

	states := []protocol.KnowledgeRecordValidityState{
		protocol.KnowledgeRecordValidityStateDraft,
		protocol.KnowledgeRecordValidityStateValidating,
		protocol.KnowledgeRecordValidityStateVerified,
		protocol.KnowledgeRecordValidityStateStale,
		protocol.KnowledgeRecordValidityStateDisputed,
		protocol.KnowledgeRecordValidityStateSuperseded,
		protocol.KnowledgeRecordValidityStateInvalid,
	}

	for i, state := range states {
		id := "pkw:recipe/affiliate-api/fixture-" + string(state)
		rec := testRetrievalRecipeRecord(id, state)
		if err := store.Put(rec); err != nil {
			t.Fatalf("Put(%s): %v", state, err)
		}

		got, err := store.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", state, err)
		}
		if got.Validity.State != state {
			t.Fatalf("fixture %d: Validity.State = %q, want %q", i, got.Validity.State, state)
		}
		if got.RetrievalRecipe == nil {
			t.Fatalf("fixture %d (%s): RetrievalRecipe = nil", i, state)
		}
	}

	list, err := store.ListByType(protocol.KnowledgeRecordTypeRetrievalRecipe)
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	if len(list) != len(states) {
		t.Fatalf("ListByType returned %d records, want %d (one per lifecycle state)", len(list), len(states))
	}
}
