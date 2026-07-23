package recipe

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSelectorProjectKeyExtractsLiteralEqualsClause(t *testing.T) {
	sel := protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
			allEquals("component", literalValue("AFFILIATE-PLATFORM")),
		},
	}
	key, ok := selectorProjectKey(sel)
	if !ok || key != "TRF" {
		t.Fatalf("selectorProjectKey = (%q, %v), want (\"TRF\", true)", key, ok)
	}
}

func TestSelectorProjectKeyMissingWhenNoProjectClause(t *testing.T) {
	sel := protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("component", literalValue("AFFILIATE-PLATFORM")),
		},
	}
	if _, ok := selectorProjectKey(sel); ok {
		t.Fatal("selectorProjectKey: want ok=false when the selector has no project clause")
	}
}

func TestReferencedBuiltinFieldsDeduplicatesAcrossAllAndAny(t *testing.T) {
	anyOp := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperatorEquals
	sel := protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
			allEquals("project", literalValue("TRF")), // duplicate on purpose
			{
				Any: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElem{
					{Field: strField("component"), Operator: &anyOp, Value: literalValue("AFFILIATE-PLATFORM")},
				},
			},
		},
	}
	got := referencedBuiltinFields(sel)
	want := map[string]bool{"project": true, "component": true}
	if len(got) != len(want) {
		t.Fatalf("referencedBuiltinFields = %v, want exactly %v", got, want)
	}
	for _, f := range got {
		if !want[f] {
			t.Fatalf("referencedBuiltinFields returned unexpected field %q", f)
		}
	}
}

// fakeSchemaClient is JiraFieldSchemaClient's test double, mirroring
// fakeSearch's shape in validation_test.go.
type fakeSchemaClient struct {
	fields map[string]FieldMeta
	err    error
	calls  int
}

func (f *fakeSchemaClient) FieldMeta(ctx context.Context, projectKey, issueTypeID string) (FieldSchema, error) {
	f.calls++
	if f.err != nil {
		return FieldSchema{}, f.err
	}
	return FieldSchema{Fields: f.fields}, nil
}

// withProjectAndComponentSelector gives rec a two-clause selector
// (project equals "TRF" AND component equals "AFFILIATE-PLATFORM") so
// schema-drift tests have a built-in field beyond "project" itself to
// simulate as removed.
func withProjectAndComponentSelector(rec protocol.KnowledgeRecord) protocol.KnowledgeRecord {
	rec.RetrievalRecipe.Selector = protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
			allEquals("component", literalValue("AFFILIATE-PLATFORM")),
		},
	}
	return rec
}

// TestResolveAndExecuteMarksRecipeStaleOnSchemaDrift is task q9r.7.2's
// core proactive-detection check: a JiraFieldSchemaClient reporting that
// "component" is gone from TRF's field configuration must mark the
// recipe stale (StalenessSchemaDrift) before ever running a live query
// against it, not just after one fails. Search returns zero results so
// the forced revalidation attempt fails and the persisted state is
// observable as stale rather than silently re-verified.
func TestResolveAndExecuteMarksRecipeStaleOnSchemaDrift(t *testing.T) {
	search := &fakeSearch{issues: nil}
	exec, repo := newTestExecutor(t, search)
	exec.Schema = &fakeSchemaClient{fields: map[string]FieldMeta{
		"project": {ID: "project", Type: "project"},
		// "component" is intentionally absent: simulates it having been
		// removed from TRF's field configuration.
	}}

	rec := withProjectAndComponentSelector(verifiedRecipeFixture("pkw:recipe/a/schema-drift"))
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err == nil {
		t.Fatal("ResolveAndExecute: want an error when forced revalidation after a detected schema drift fails")
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateStale {
		t.Fatalf("Validity.State = %q, want stale after a detected schema drift", updated.Validity.State)
	}
}

// TestResolveAndExecuteDoesNotMarkStaleWhenSchemaMatches is the negative
// case: a schema response that still contains every referenced field must
// not force an unnecessary revalidation, matching
// TestResolveAndExecuteDoesNotRevalidateOnMatchingInstance's fingerprint
// analogue.
func TestResolveAndExecuteDoesNotMarkStaleWhenSchemaMatches(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)
	schema := &fakeSchemaClient{fields: map[string]FieldMeta{
		"project":   {ID: "project", Type: "project"},
		"component": {ID: "components", Type: "array"},
	}}
	exec.Schema = schema

	rec := withProjectAndComponentSelector(verifiedRecipeFixture("pkw:recipe/a/schema-ok"))
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err != nil {
		t.Fatalf("ResolveAndExecute: %v", err)
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateVerified {
		t.Fatalf("Validity.State = %q, want it to remain verified when the schema matches", updated.Validity.State)
	}
	if schema.calls != 1 {
		t.Fatalf("FieldMeta called %d times, want exactly 1", schema.calls)
	}
	if search.calls != 1 {
		t.Fatalf("Search called %d times, want exactly 1 (no extra revalidation dry run)", search.calls)
	}
}

// TestResolveAndExecuteIgnoresSchemaClientErrors proves a flaky/erroring
// JiraFieldSchemaClient degrades gracefully rather than blocking an
// otherwise-healthy resolve_operation call - the proactive check is a
// best-effort enhancement, not a new hard dependency.
func TestResolveAndExecuteIgnoresSchemaClientErrors(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)
	exec.Schema = &fakeSchemaClient{err: errRejected("field meta service unavailable")}

	rec := withProjectAndComponentSelector(verifiedRecipeFixture("pkw:recipe/a/schema-client-error"))
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, nil, "run-1", "task-1", time.Now()); err != nil {
		t.Fatalf("ResolveAndExecute: %v, want a schema-client error to degrade gracefully rather than block execution", err)
	}

	updated, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if updated.Validity.State != protocol.KnowledgeRecordValidityStateVerified {
		t.Fatalf("Validity.State = %q, want it to remain verified when the schema client errors", updated.Validity.State)
	}
}
