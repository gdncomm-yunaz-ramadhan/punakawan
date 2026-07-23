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
	// Instance identifies which Jira instance Search actually talks to
	// (task q9r.7 #1). Zero-value InstanceFingerprint{} disables the
	// check below entirely (String() == ""), preserving every existing
	// caller's behavior exactly - only a caller that populates this can
	// opt into instance-mismatch protection.
	Instance InstanceFingerprint
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

	// A recipe validated against a different Jira instance than the one
	// e.Search actually talks to is exactly the staleness class task
	// q9r.7 #1 guards against: its selector/field references may look
	// structurally fine and still be meaningless (or worse, silently
	// matching the wrong project) on this instance. Treat a mismatch
	// identically to OutcomeStale - revalidate before reuse, never a
	// silent block or a silent execute.
	fingerprintErr := CheckInstanceFingerprint(rec, e.Instance)
	if fingerprintErr != nil && res.Outcome == OutcomeResolved {
		res.Outcome = OutcomeStale
	}

	switch res.Outcome {
	case OutcomeStale:
		if err := e.revalidate(ctx, rec, bindings); err != nil {
			reason := fmt.Sprintf("stale recipe %q failed revalidation: %v", rec.Id, err)
			if fingerprintErr != nil {
				reason = fmt.Sprintf("recipe %q failed revalidation after instance-fingerprint mismatch (%v): %v", rec.Id, fingerprintErr, err)
			}
			return nil, fmt.Errorf("recipe: resolve_operation: %s", reason)
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
		if err := e.recordFingerprint(rec); err != nil {
			return nil, fmt.Errorf("recipe: resolve_operation: record instance fingerprint for %q: %w", rec.Id, err)
		}
		refreshed, err = e.Repo.Store.Get(rec.Id)
		if err != nil {
			return nil, fmt.Errorf("recipe: resolve_operation: reload recipe %q after fingerprint update: %w", rec.Id, err)
		}
		rec = refreshed
	case OutcomeResolved:
		// A verified candidate with a clear margin and (if e.Instance is
		// populated) a matching instance fingerprint - proceed directly.
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

// recordFingerprint stamps e.Instance onto rec's validation block after a
// successful revalidation, so a recipe that was reused across instances
// (deliberately re-pointed at a new Jira site and re-verified) is not
// re-flagged as mismatched on every subsequent call - only the *first*
// reuse against a new instance forces the revalidate path; once
// revalidated there, it is the new instance's fingerprint of record. A
// zero-value e.Instance (String() == "") is a no-op: nothing to stamp.
func (e *Executor) recordFingerprint(rec protocol.KnowledgeRecord) error {
	fp := e.Instance.String()
	if fp == "" || rec.RetrievalRecipe == nil {
		return nil
	}
	if rec.RetrievalRecipe.Validation == nil {
		rec.RetrievalRecipe.Validation = &protocol.KnowledgeRecordRetrievalRecipeValidation{}
	}
	rec.RetrievalRecipe.Validation.ProviderInstanceFingerprint = &fp
	return e.Repo.Store.Put(rec)
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
		// A live provider rejection on an until-now-verified recipe is
		// task q9r.7 #3's schema-change signal this package can actually
		// observe without a dedicated field-metadata integration: Jira
		// rejecting the compiled query (e.g. a referenced field/component
		// was removed or retyped server-side) means the recipe can no
		// longer be trusted as-is. Mark it stale so the *next* reuse goes
		// through Executor.revalidate instead of repeating the same
		// failing query - §12's "Jira rejects the compiled query" trigger,
		// which until now had a StalenessReason constant but no caller
		// that ever invoked it on this path.
		if markErr := e.Repo.MarkStale(rec.Id, StalenessProviderRejected); markErr != nil {
			return nil, fmt.Errorf("recipe: execute: provider search failed: %w (additionally failed to mark recipe stale: %v)", searchErr, markErr)
		}
		return nil, fmt.Errorf("recipe: execute: provider search failed, recipe marked stale for revalidation on next use: %w", searchErr)
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
	// last_execution.bindings is the schema's only fully free-form object
	// on a stored execution (protocol/knowledge.schema.json has no
	// additionalProperties:false on it) and bindings itself is whatever a
	// caller passed to ResolveAndExecute - nothing upstream of this point
	// inspects its contents. This is the actual persistence boundary, so
	// this is where task q9r.7 #4's "recipes must never store
	// credentials" gets enforced, not just tested around: a secret-shaped
	// binding fails the whole execution rather than being silently
	// written to durable knowledge.
	if err := CheckNoSecrets("bindings", bindings); err != nil {
		return err
	}
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
	// Concurrent executions of the same recipe (task q9r.7 #5) each write
	// this same row; Dolt's serializable-ish transaction isolation can
	// reject one of them with a transient 1213 conflict rather than
	// silently corrupting anything (confirmed by
	// TestConcurrentResolveAndExecuteAgainstVerifiedRecipeDoesNotRace).
	// Retry that specific, well-understood transient case rather than
	// failing an otherwise-successful execution.
	return putWithConflictRetry(e.Repo.Store.Put, rec)
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
