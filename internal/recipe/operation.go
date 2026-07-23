package recipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DiscoveryNeededError signals §16's "no valid recipe -> guided
// discovery" branch: ResolveAndExecute found no candidate it may reuse
// automatically. A stale candidate is not included here - §7's "Stale
// candidate: revalidate before reuse" means ResolveAndExecute
// revalidates it inline instead of asking the caller to run discovery.
//
// §16's "Suggested BD expansion" (resolve recipe / discover mapping if
// missing / validate candidate / persist / retrieve / attach evidence)
// describes how a real orchestrating agent should decompose the task
// with `bd create`/`bd dep add` once this error is returned - that
// decomposition is the calling agent's job, not logic this package
// encodes: internal/recipe has no bd/task-creation dependency anywhere
// else, and adding one here would duplicate internal/beads rather than
// reuse it.
type DiscoveryNeededError struct {
	Outcome    Outcome
	Candidates []Candidate
}

func (e *DiscoveryNeededError) Error() string {
	return fmt.Sprintf("recipe: resolve_operation: no reusable recipe (%s) - guided discovery required", e.Outcome)
}

// MaxExecutionResults bounds a real (non-sampling) execution's page
// size - larger than Validator's 20-item preview, since an accepted
// recipe's actual reuse should return everything relevant.
const MaxExecutionResults = 200

// Executor implements §16's resolve_operation workflow step: resolve a
// cached recipe, revalidate it first if stale, compile and execute it
// against the provider, and record execution evidence (§13). It never
// runs guided discovery itself - that is DiscoverySession's job (§8),
// entered by the caller when ResolveAndExecute returns a
// *DiscoveryNeededError. "Resume the original task after discovery or
// revalidation" (this phase's own exit criterion) is likewise the
// caller's job: this package returns a result or a typed error and does
// not know what "the original task" is.
type Executor struct {
	Repo     *Repository
	Resolver *Resolver
	Compiler *Compiler
	Search   JiraSearchClient
	Ledger   *evidence.Ledger
}

// ExecutionResult is what a successful resolve_operation step returns.
type ExecutionResult struct {
	RecipeID      string
	RecipeVersion int
	CompiledQuery CompiledQuery
	Issues        []JiraIssue
	Evidence      protocol.EvidenceRecord
}

// ResolveAndExecute is the resolve_operation step's entry point.
func (e *Executor) ResolveAndExecute(ctx context.Context, req OperationRequest, bindings map[string]interface{}, runID, taskID string, now time.Time) (*ExecutionResult, error) {
	res, err := e.Resolver.Resolve(req)
	if err != nil {
		return nil, fmt.Errorf("recipe: resolve_operation: %w", err)
	}

	if res.Outcome == OutcomeNotFound || res.Outcome == OutcomeAmbiguous {
		return nil, &DiscoveryNeededError{Outcome: res.Outcome, Candidates: res.Candidates}
	}

	rec := res.Selected.Record
	switch res.Outcome {
	case OutcomeStale:
		if err := e.revalidate(ctx, rec, bindings); err != nil {
			return nil, fmt.Errorf("recipe: resolve_operation: stale recipe %q failed revalidation: %w", rec.Id, err)
		}
		// Verify (inside revalidate) updated the stored record; reload it
		// so execute persists last_execution on top of the now-verified
		// state instead of overwriting it back to stale with this stale
		// in-memory snapshot.
		refreshed, err := e.Repo.Store.Get(rec.Id)
		if err != nil {
			return nil, fmt.Errorf("recipe: resolve_operation: reload revalidated recipe %q: %w", rec.Id, err)
		}
		rec = refreshed
	case OutcomeResolved:
		// A verified candidate with a clear margin - proceed directly.
	default:
		return nil, fmt.Errorf("recipe: resolve_operation: unhandled outcome %q", res.Outcome)
	}

	return e.execute(ctx, rec, bindings, runID, taskID, now)
}

// revalidate re-runs the validation pipeline against a stale recipe and,
// only on a pass, marks it verified again - §12's "Stale: validate
// first," never a silent reuse of an unrevalidated stale candidate.
func (e *Executor) revalidate(ctx context.Context, rec protocol.KnowledgeRecord, bindings map[string]interface{}) error {
	v := &Validator{Compiler: e.Compiler, Search: e.Search}
	report, err := v.Validate(ctx, rec.RetrievalRecipe, bindings, nil, nil)
	if err != nil {
		return err
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed {
		return fmt.Errorf("revalidation failed: %s", report.FailureReason)
	}
	// A passing automated revalidation restores verified without asking
	// the human again - §12's "Stale: validate first" describes exactly
	// this re-check, not a second full acceptance (§9 step 10's human
	// acceptance already happened when the recipe was first verified).
	return e.Repo.Verify(rec.Id, "system:revalidation", "revalidated after staleness")
}

func (e *Executor) execute(ctx context.Context, rec protocol.KnowledgeRecord, bindings map[string]interface{}, runID, taskID string, now time.Time) (*ExecutionResult, error) {
	cq, err := e.Compiler.Compile(ctx, rec.RetrievalRecipe, bindings)
	if err != nil {
		return nil, fmt.Errorf("recipe: execute: %w", err)
	}
	if e.Search == nil {
		return nil, fmt.Errorf("recipe: execute: no Jira search client configured")
	}

	issues, searchErr := e.Search.Search(ctx, cq.JQL, cq.OrderBy, cq.Fields, MaxExecutionResults)
	status := protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatusSuccess
	if searchErr != nil {
		status = protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatusFailure
	}

	sum := sha256.Sum256([]byte(cq.JQL))
	hash := "sha256:" + hex.EncodeToString(sum[:])
	resultCount := len(issues)
	version := 1
	if rec.RetrievalRecipe.RecipeVersion != nil {
		version = *rec.RetrievalRecipe.RecipeVersion
	}

	evRec, evErr := recordExecutionEvidence(e.Ledger, runID, taskID, rec.Id, version, hash, resultCount, status, now)
	if evErr != nil {
		return nil, fmt.Errorf("recipe: execute: record evidence: %w", evErr)
	}
	if err := e.recordLastExecution(rec, runID, taskID, bindings, hash, resultCount, status, evRec.Id, now); err != nil {
		return nil, fmt.Errorf("recipe: execute: record last_execution: %w", err)
	}
	if searchErr != nil {
		return nil, fmt.Errorf("recipe: execute: provider search failed: %w", searchErr)
	}

	return &ExecutionResult{
		RecipeID:      rec.Id,
		RecipeVersion: version,
		CompiledQuery: cq,
		Issues:        issues,
		Evidence:      evRec,
	}, nil
}

// recordLastExecution updates rec's last_execution block in place via
// Store.Put (an upsert, per knowledge.Store.Put's own doc comment) -
// this is not a version change: §10's immutable-versioning rules govern
// the recipe's selector/spec, and last_execution is documented in
// Phase 0's schema as "the latest, for quick display," always mutable.
func (e *Executor) recordLastExecution(rec protocol.KnowledgeRecord, runID, taskID string, bindings map[string]interface{}, hash string, resultCount int, status protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatus, evidenceID string, now time.Time) error {
	rec.RetrievalRecipe.LastExecution = &protocol.KnowledgeRecordRetrievalRecipeLastExecution{
		SessionId:         &runID,
		TaskId:            &taskID,
		ExecutedAt:        &now,
		Bindings:          protocol.KnowledgeRecordRetrievalRecipeLastExecutionBindings(bindings),
		CompiledQueryHash: &hash,
		ResultCount:       &resultCount,
		Status:            &status,
		EvidenceId:        &evidenceID,
	}
	return e.Repo.Store.Put(rec)
}

// recordExecutionEvidence appends an evidence record for one execution
// (the plan's ev-recipe-execution-* example, §13).
func recordExecutionEvidence(l *evidence.Ledger, runID, taskID, recipeID string, recipeVersion int, hash string, resultCount int, status protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatus, now time.Time) (protocol.EvidenceRecord, error) {
	summary := fmt.Sprintf("recipe=%s version=%d compiled_query_hash=%s result_count=%d status=%s",
		recipeID, recipeVersion, hash, resultCount, status)
	rec := protocol.EvidenceRecord{
		Id:        fmt.Sprintf("ev-%s-%s-recipe-execution-%d", runID, taskID, now.UnixNano()),
		RunId:     runID,
		TaskId:    &taskID,
		Type:      protocol.EvidenceRecordTypeExternalResponse,
		Summary:   &summary,
		CreatedAt: now,
	}
	if err := l.Append(rec); err != nil {
		return protocol.EvidenceRecord{}, err
	}
	return rec, nil
}
