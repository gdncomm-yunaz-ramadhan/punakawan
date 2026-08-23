package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func seedRecipe(t *testing.T, dir, id string, state protocol.KnowledgeRecordValidityState) protocol.KnowledgeRecord {
	t.Helper()
	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}

	validity := protocol.KnowledgeRecordValidity{State: state}
	if state == protocol.KnowledgeRecordValidityStateVerified {
		validity.VerifiedBy = []string{"user"}
	}
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRetrievalRecipe,
		Status: "active",
		Title:  "Find work items",
		Source: protocol.KnowledgeRecordSource{Provider: "user_instruction", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: validity,
		RetrievalRecipe: &protocol.KnowledgeRecordRetrievalRecipe{
			Capability: "jira.issue.search",
			Intent:     "project.next-sprint.issues",
			Provider:   "jira",
			Resource:   "issue",
			Operation:  "search",
			ReadOnly:   true,
			Selector: protocol.KnowledgeRecordRetrievalRecipeSelector{
				All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
					{
						Field:    strPtrLocal("project"),
						Operator: opPtrLocal(protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals),
						Value:    map[string]interface{}{"literal": "TRF"},
					},
				},
			},
			Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
				EntityType:    "jira_issue",
				IdentityField: "key",
				Fields:        []string{"key"},
			},
		},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return rec
}

func strPtrLocal(s string) *string { return &s }
func opPtrLocal(o protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperator) *protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperator {
	return &o
}

// seedRequirement puts a minimal requirement-type knowledge record scoped
// to repository, for exercising `knowledge reset`'s scope filtering.
func seedRequirement(t *testing.T, dir, id, repository string) protocol.KnowledgeRecord {
	t.Helper()
	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Fixture requirement",
		Scope:  &protocol.KnowledgeRecordScope{Repository: strPtrLocal(repository)},
		Source: protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return rec
}

func TestKnowledgeResetRequiresAScopeField(t *testing.T) {
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "knowledge", "reset")
	if err == nil {
		t.Fatalf("expected an error when no scope flag is given, got output: %s", out)
	}
}

func TestKnowledgeResetDefaultsToDryRun(t *testing.T) {
	dir := newSmokeWorkspace(t)
	rec := seedRequirement(t, dir, "pkw:req/fixture/reset-1", "checkout-service")

	out, err := runCLI(t, dir, "knowledge", "reset", "--repository", "checkout-service")
	if err != nil {
		t.Fatalf("knowledge reset: %v\n%s", err, out)
	}
	if !strings.Contains(out, "dry run") || !strings.Contains(out, rec.Id) {
		t.Fatalf("unexpected dry-run output: %s", out)
	}

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	if _, err := store.Get(rec.Id); err != nil {
		t.Fatalf("expected the record to still exist after a dry run: %v", err)
	}
}

func TestKnowledgeResetDeletesOnlyMatchedScopeWhenConfirmed(t *testing.T) {
	dir := newSmokeWorkspace(t)
	inScope := seedRequirement(t, dir, "pkw:req/fixture/reset-2", "checkout-service")
	outOfScope := seedRequirement(t, dir, "pkw:req/fixture/reset-3", "other-service")

	out, err := runCLI(t, dir, "knowledge", "reset", "--repository", "checkout-service", "--confirm")
	if err != nil {
		t.Fatalf("knowledge reset: %v\n%s", err, out)
	}
	if !strings.Contains(out, "deleted 1 record") || !strings.Contains(out, "commit:") {
		t.Fatalf("unexpected confirmed-reset output: %s", out)
	}

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	if _, err := store.Get(inScope.Id); err == nil {
		t.Fatal("expected the in-scope record to be deleted")
	}
	if _, err := store.Get(outOfScope.Id); err != nil {
		t.Fatalf("expected the out-of-scope record to survive: %v", err)
	}
}

func TestRecipeListAndShow(t *testing.T) {
	dir := newSmokeWorkspace(t)
	rec := seedRecipe(t, dir, "pkw:recipe/smoke/next-sprint", protocol.KnowledgeRecordValidityStateVerified)

	out, err := runCLI(t, dir, "knowledge", "recipe", "list")
	if err != nil {
		t.Fatalf("recipe list: %v\n%s", err, out)
	}
	if !strings.Contains(out, rec.Id) || !strings.Contains(out, "verified") {
		t.Fatalf("unexpected list output: %s", out)
	}

	out, err = runCLI(t, dir, "knowledge", "recipe", "show", rec.Id)
	if err != nil {
		t.Fatalf("recipe show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "jira.issue.search") {
		t.Fatalf("unexpected show output: %s", out)
	}
}

func TestRecipeExplainAndValidate(t *testing.T) {
	dir := newSmokeWorkspace(t)
	rec := seedRecipe(t, dir, "pkw:recipe/smoke/next-sprint", protocol.KnowledgeRecordValidityStateVerified)

	out, err := runCLI(t, dir, "knowledge", "recipe", "explain", rec.Id)
	if err != nil {
		t.Fatalf("recipe explain: %v\n%s", err, out)
	}
	if !strings.Contains(out, `project = "TRF"`) {
		t.Fatalf("unexpected explain output: %s", out)
	}

	out, err = runCLI(t, dir, "knowledge", "recipe", "validate", rec.Id)
	if err != nil {
		t.Fatalf("recipe validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "compiles cleanly") {
		t.Fatalf("unexpected validate output: %s", out)
	}
}

func TestRecipeDisputeExcludesFromReuse(t *testing.T) {
	dir := newSmokeWorkspace(t)
	rec := seedRecipe(t, dir, "pkw:recipe/smoke/next-sprint", protocol.KnowledgeRecordValidityStateVerified)

	if _, err := runCLI(t, dir, "knowledge", "recipe", "dispute", rec.Id, "--reason", "wrong issues"); err != nil {
		t.Fatalf("recipe dispute: %v", err)
	}

	out, err := runCLI(t, dir, "knowledge", "recipe", "list")
	if err != nil {
		t.Fatalf("recipe list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "disputed") {
		t.Fatalf("expected disputed state in list output: %s", out)
	}
}

func TestRecipeSupersede(t *testing.T) {
	dir := newSmokeWorkspace(t)
	old := seedRecipe(t, dir, "pkw:recipe/smoke/old", protocol.KnowledgeRecordValidityStateVerified)
	replacement := seedRecipe(t, dir, "pkw:recipe/smoke/replacement", protocol.KnowledgeRecordValidityStateVerified)

	out, err := runCLI(t, dir, "knowledge", "recipe", "supersede", old.Id, "--with", replacement.Id)
	if err != nil {
		t.Fatalf("recipe supersede: %v\n%s", err, out)
	}
	if !strings.Contains(out, replacement.Id) {
		t.Fatalf("unexpected supersede output: %s", out)
	}

	list, err := runCLI(t, dir, "knowledge", "recipe", "list")
	if err != nil {
		t.Fatalf("recipe list: %v\n%s", err, list)
	}
	if !strings.Contains(list, "superseded") {
		t.Fatalf("expected superseded state in list output: %s", list)
	}
}

func TestRecipeUpdateMovesToValidating(t *testing.T) {
	dir := newSmokeWorkspace(t)
	rec := seedRecipe(t, dir, "pkw:recipe/smoke/next-sprint", protocol.KnowledgeRecordValidityStateVerified)

	out, err := runCLI(t, dir, "knowledge", "recipe", "update", rec.Id)
	if err != nil {
		t.Fatalf("recipe update: %v\n%s", err, out)
	}
	if !strings.Contains(out, "validating") {
		t.Fatalf("unexpected update output: %s", out)
	}
}
