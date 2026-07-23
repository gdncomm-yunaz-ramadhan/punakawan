package recipe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeBearerToken/fakeAPIKey/fakeAtlassianToken/fakeAWSKey are
// credential-shaped values that must never survive into durable
// knowledge, per plan §13/§20 (task q9r.7 #4). None of these are real
// credentials - they are shaped like ones (fixed test fixtures) purely to
// exercise looksLikeSecret's pattern matching.
const (
	fakeBearerToken    = "Bearer sk-fake-not-a-real-token-1234567890abcdef"
	fakeAPIKeyAssign   = `api_key="fakeFAKEfakeFAKEfakeFAKEfakeFAKE1234"`
	fakeAtlassianToken = "ATATT3xFfGF0fakefakefakefakefakefakefakefakeFAKE"
	fakeGithubToken    = "ghp_fakefakefakefakefakefakefakefake12"
	fakeAWSKey         = "AKIAFAKE1234567890AB"
)

func TestLooksLikeSecretRecognizesCommonShapes(t *testing.T) {
	cases := []string{fakeBearerToken, fakeAPIKeyAssign, fakeAtlassianToken, fakeGithubToken, fakeAWSKey}
	for _, s := range cases {
		if !looksLikeSecret(s) {
			t.Errorf("looksLikeSecret(%q) = false, want true", s)
		}
	}
}

func TestLooksLikeSecretDoesNotFlagOrdinaryValues(t *testing.T) {
	ordinary := []string{
		"TRF", "AFFILIATE-PLATFORM", "1834", "next", "Affiliate Delivery",
		"TRF Sprint 42", "kr-jira-affiliate-platform-next-sprint", "user",
	}
	for _, s := range ordinary {
		if looksLikeSecret(s) {
			t.Errorf("looksLikeSecret(%q) = true, want false (false positive on an ordinary value)", s)
		}
	}
}

func TestCheckNoSecretsCatchesTopLevelStringValue(t *testing.T) {
	err := CheckNoSecrets("bindings", map[string]interface{}{
		"sprint_id":  "1834",
		"auth_token": fakeBearerToken,
	})
	var secretErr *SecretShapedValueError
	if !errors.As(err, &secretErr) {
		t.Fatalf("err = %v, want *SecretShapedValueError", err)
	}
	if !strings.Contains(secretErr.Path, "auth_token") {
		t.Fatalf("Path = %q, want it to identify the offending key", secretErr.Path)
	}
	// The error message itself must not echo the secret value back out -
	// otherwise the guard would defeat its own purpose by putting the
	// token in logs/error strings instead of the knowledge store.
	if strings.Contains(err.Error(), fakeBearerToken) {
		t.Fatal("error message leaked the secret value")
	}
}

func TestCheckNoSecretsCatchesNestedValue(t *testing.T) {
	err := CheckNoSecrets("selector.resolver.arguments", map[string]interface{}{
		"headers": map[string]interface{}{
			"Authorization": fakeBearerToken,
		},
	})
	if err == nil {
		t.Fatal("CheckNoSecrets: want an error for a secret nested inside a map value")
	}
}

func TestCheckNoSecretsCatchesValueInsideList(t *testing.T) {
	err := CheckNoSecrets("bindings", map[string]interface{}{
		"extra": []interface{}{"ok", fakeAtlassianToken},
	})
	if err == nil {
		t.Fatal("CheckNoSecrets: want an error for a secret nested inside a list value")
	}
}

func TestCheckNoSecretsPassesForOrdinaryBindings(t *testing.T) {
	err := CheckNoSecrets("bindings", map[string]interface{}{
		"sprint_id":       "1834",
		"sprint_selector": "next",
		"board_id":        42,
	})
	if err != nil {
		t.Fatalf("CheckNoSecrets: %v, want nil for ordinary binding values", err)
	}
}

// TestExecutorRefusesToPersistSecretShapedBinding is the end-to-end proof
// task q9r.7 #4 asks for: a fake bearer-token-shaped binding must never
// reach Store.Put (and therefore never round-trip back out of the
// knowledge store), not just fail some standalone unit check.
func TestExecutorRefusesToPersistSecretShapedBinding(t *testing.T) {
	search := &fakeSearch{issues: []JiraIssue{{Key: "TRF-1", Summary: "a"}}}
	exec, repo := newTestExecutor(t, search)

	rec := verifiedRecipeFixture("pkw:recipe/a/secret-binding")
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	maliciousBindings := map[string]interface{}{
		"sprint_id":      "1834",
		"jira_api_token": fakeAtlassianToken,
	}
	_, err := exec.ResolveAndExecute(context.Background(), OperationRequest{Capability: "jira.issue.search"}, maliciousBindings, "run-1", "task-1", time.Now())
	var secretErr *SecretShapedValueError
	if !errors.As(err, &secretErr) {
		t.Fatalf("ResolveAndExecute err = %v, want *SecretShapedValueError", err)
	}

	// Confirm nothing was persisted: last_execution must remain unset.
	got, err := repo.Store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetrievalRecipe.LastExecution != nil {
		t.Fatal("LastExecution was recorded despite a secret-shaped binding; the secret may have been persisted")
	}
}

// TestCompilerRefusesSecretShapedResolverArgument covers the other free-
// form object the schema allows: a resolver call's arguments, which flow
// into a *stored* selector (via CreateVersion) at authoring time, not just
// a transient execution binding.
func TestCompilerRefusesSecretShapedResolverArgument(t *testing.T) {
	c := NewCompiler(nil)
	c.Resolvers["test.echo"] = func(ctx context.Context, args map[string]interface{}, warnings *[]string) (interface{}, error) {
		return "unused", nil
	}

	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", resolverValue("test.echo", map[string]interface{}{
				"webhook_secret": fakeAPIKeyAssign,
			})),
		},
	})

	_, err := c.Compile(context.Background(), rr, nil)
	var secretErr *SecretShapedValueError
	if !errors.As(err, &secretErr) {
		t.Fatalf("Compile err = %v, want *SecretShapedValueError", err)
	}
}

// TestRecipeRoundTripNeverPersistsASecretShapedBindingEvenViaCreateVersion
// exercises the Repository.CreateVersion path directly (bypassing
// Executor) to prove the guard isn't something only ResolveAndExecute
// happens to enforce - though note CreateVersion itself does not (and per
// its own contract should not) accept bindings at all; last_execution is
// only ever written by Executor. This test documents that boundary: a
// hand-assembled record with a secret-shaped last_execution.bindings set
// directly bypasses the Go-level guard entirely, because
// knowledge.Store.Put performs no content inspection (see provenance.go's
// Validate) - only Executor's own call path is guarded. This is recorded
// explicitly rather than silently assumed.
func TestStorePutPerformsNoContentInspectionOnBindings(t *testing.T) {
	store := newTestStore(t)
	repo := &Repository{Store: store}

	rec := verifiedRecipeFixture("pkw:recipe/a/direct-put-secret")
	secret := fakeBearerToken
	rec.RetrievalRecipe.LastExecution = &protocol.KnowledgeRecordRetrievalRecipeLastExecution{
		Bindings: protocol.KnowledgeRecordRetrievalRecipeLastExecutionBindings{"leaked": secret},
	}
	if err := repo.Store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RetrievalRecipe.LastExecution.Bindings["leaked"] != secret {
		t.Fatal("expected the secret to round-trip through Store.Put/Get unmodified, confirming Store itself has no content guard")
	}
	// This is the documented gap, not a passing security assertion: only
	// Executor.recordLastExecution and Compiler.callResolver are guarded
	// (the only two producers of these free-form fields this package
	// owns). Any other future writer of a KnowledgeRecordRetrievalRecipe
	// (a hand-built import, a different caller) must apply CheckNoSecrets
	// itself; Store.Put does not do it for them.
}
