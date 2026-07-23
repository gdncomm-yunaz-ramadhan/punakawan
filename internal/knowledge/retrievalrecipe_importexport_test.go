package knowledge

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// recipeRecordFixture builds a minimally valid retrieval-recipe
// KnowledgeRecord exercising the fields task q9r.7 #6 asks to confirm
// survive a round trip: lineage (SupersededBy + a supersedes relation),
// RecipeVersion, and validity state.
func recipeRecordFixture(id string, version int, supersedesID string) protocol.KnowledgeRecord {
	now := time.Now().UTC()
	status := protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed
	fingerprint := "jira:example.atlassian.net:cloud-abc"
	rec := protocol.KnowledgeRecord{
		Id:      id,
		Type:    protocol.KnowledgeRecordTypeRetrievalRecipe,
		Status:  "active",
		Title:   "Find Affiliate Platform Jira work for the next sprint",
		Aliases: []string{"affiliate platform next sprint"},
		Source: protocol.KnowledgeRecordSource{
			Provider:    "user_instruction",
			RetrievedAt: now,
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"user"},
		},
		RetrievalRecipe: &protocol.KnowledgeRecordRetrievalRecipe{
			Capability:    "jira.issue.search",
			Intent:        "project.next-sprint.issues",
			Provider:      "jira",
			Resource:      "issue",
			Operation:     "search",
			ReadOnly:      true,
			RecipeVersion: &version,
			AppliesTo: &protocol.KnowledgeRecordRetrievalRecipeAppliesTo{
				WorkspaceIds: []string{"affiliate-platform"},
			},
			Selector: protocol.KnowledgeRecordRetrievalRecipeSelector{},
			Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
				EntityType:    "jira_issue",
				IdentityField: "key",
				Fields:        []string{"key", "summary"},
			},
			Validation: &protocol.KnowledgeRecordRetrievalRecipeValidation{
				Status:                      &status,
				ProviderInstanceFingerprint: &fingerprint,
			},
		},
	}
	if supersedesID != "" {
		rec.Relations = []protocol.KnowledgeRecordRelationsElem{
			{Type: protocol.KnowledgeRecordRelationsElemTypeSupersedes, Target: supersedesID},
		}
	}
	return rec
}

// TestExportImportRoundTripPreservesRetrievalRecipeFields is task q9r.7
// #6's check: this project's convention is to reuse the generic knowledge
// export/import mechanism (Store.Export/Import) for every KnowledgeRecord
// type, retrieval_recipe included, rather than build a recipe-specific
// one. No dedicated recipe export/import command exists anywhere in this
// repo (cmd/punakawan/knowledge_cmd.go only wires list/show/explain/
// validate/update/dispute/supersede - no export/import subcommand exists
// at all yet, generic or recipe-specific), so this test exercises the
// underlying Store.Export/Import mechanism directly, confirming it is
// already recipe-type-agnostic and preserves the fields a recipe actually
// needs: RecipeVersion, validity.state, the validation block (including
// provider_instance_fingerprint, task q9r.7 #1), and the supersedes
// lineage relation between two versions.
func TestExportImportRoundTripPreservesRetrievalRecipeFields(t *testing.T) {
	store := newTestStore(t)

	v1 := recipeRecordFixture("pkw:recipe/fixture/lineage@1", 1, "")
	if err := store.Put(v1); err != nil {
		t.Fatalf("Put v1: %v", err)
	}

	v2 := recipeRecordFixture("pkw:recipe/fixture/lineage@2", 2, v1.Id)
	if err := store.Put(v2); err != nil {
		t.Fatalf("Put v2: %v", err)
	}
	if err := store.Supersede(v1.Id, v2.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	var buf bytes.Buffer
	if err := store.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	fresh := newTestStore(t)
	if err := fresh.Import(strings.NewReader(buf.String())); err != nil {
		t.Fatalf("Import: %v", err)
	}

	gotV1, err := fresh.Get(v1.Id)
	if err != nil {
		t.Fatalf("Get v1 after import: %v", err)
	}
	if gotV1.Validity.State != protocol.KnowledgeRecordValidityStateSuperseded {
		t.Fatalf("v1.Validity.State = %q, want superseded", gotV1.Validity.State)
	}
	if gotV1.SupersededBy == nil || *gotV1.SupersededBy != v2.Id {
		t.Fatalf("v1.SupersededBy = %v, want %q", gotV1.SupersededBy, v2.Id)
	}
	if gotV1.RetrievalRecipe == nil || gotV1.RetrievalRecipe.RecipeVersion == nil || *gotV1.RetrievalRecipe.RecipeVersion != 1 {
		t.Fatalf("v1.RetrievalRecipe.RecipeVersion = %v, want 1", gotV1.RetrievalRecipe)
	}

	gotV2, err := fresh.Get(v2.Id)
	if err != nil {
		t.Fatalf("Get v2 after import: %v", err)
	}
	if gotV2.RetrievalRecipe == nil {
		t.Fatal("v2.RetrievalRecipe is nil after import")
	}
	if gotV2.RetrievalRecipe.RecipeVersion == nil || *gotV2.RetrievalRecipe.RecipeVersion != 2 {
		t.Fatalf("v2.RetrievalRecipe.RecipeVersion = %v, want 2", gotV2.RetrievalRecipe.RecipeVersion)
	}
	if gotV2.Validity.State != protocol.KnowledgeRecordValidityStateVerified {
		t.Fatalf("v2.Validity.State = %q, want verified", gotV2.Validity.State)
	}
	if len(gotV2.Relations) != 1 || gotV2.Relations[0].Type != protocol.KnowledgeRecordRelationsElemTypeSupersedes || gotV2.Relations[0].Target != v1.Id {
		t.Fatalf("v2.Relations = %+v, want a supersedes relation targeting %q", gotV2.Relations, v1.Id)
	}
	if gotV2.RetrievalRecipe.Validation == nil || gotV2.RetrievalRecipe.Validation.ProviderInstanceFingerprint == nil {
		t.Fatal("v2.RetrievalRecipe.Validation.ProviderInstanceFingerprint is nil after import, want it preserved")
	}
	if got := *gotV2.RetrievalRecipe.Validation.ProviderInstanceFingerprint; got != "jira:example.atlassian.net:cloud-abc" {
		t.Fatalf("ProviderInstanceFingerprint = %q, want it unchanged across the round trip", got)
	}

	// The lineage's reverse edge must also be rebuilt via Import's own
	// Put calls (the same relation-index rebuild TestExportImportRoundTrip
	// already exercises for non-recipe types), so a fresh store can answer
	// "what supersedes v1?" without special-casing recipes.
	related, err := fresh.Related(v1.Id)
	if err != nil {
		t.Fatalf("Related after import: %v", err)
	}
	if len(related) != 1 || related[0].Id != v2.Id {
		t.Fatalf("Related(v1) = %+v, want [%s]", related, v2.Id)
	}

	recipes, err := fresh.ListByType(protocol.KnowledgeRecordTypeRetrievalRecipe)
	if err != nil {
		t.Fatalf("ListByType retrieval-recipe: %v", err)
	}
	if len(recipes) != 2 {
		t.Fatalf("ListByType retrieval-recipe returned %d records, want 2", len(recipes))
	}
}
