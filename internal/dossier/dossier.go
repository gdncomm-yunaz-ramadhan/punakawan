// Package dossier assembles a protocol.KnowledgeRecord of type
// context-dossier from real workspace, git, and knowledge-store state, plus
// caller-supplied intent text, per
// punakawan-go-typescript-detailed-plan.md §9.1 and §28.4's
// build_context_dossier ("assemble §9.1's dossier fields from workspace,
// git inspection, and durable knowledge... no reasoning").
//
// This package only assembles data; it does not decide what the user wants,
// judge feasibility, or produce a plan. Everything Punakawan's own code
// cannot know on its own (user goal, desired behavior, assumptions, etc.)
// must be supplied by the caller in BuildInput and is copied through
// unchanged.
package dossier

import (
	"context"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// BuildInput carries everything Build needs beyond what it can observe
// itself from the workspace, git, and knowledge store.
type BuildInput struct {
	// WorkspaceID and RunID identify this dossier and are combined into its
	// id: pkw:contextdossier/<WorkspaceID>/<RunID>. The id's <kind> segment
	// is "contextdossier" (no hyphen) rather than the record's own
	// "context-dossier" type value, because internal/knowledge.Validate and
	// protocol/knowledge.schema.json's "id" pattern
	// (^pkw:[a-z]+/[a-z0-9-]+/.+$) only allow letters in that segment — the
	// same reason internal/convention.Extract uses the single word
	// "convention" (not e.g. "convention-profile") for its own ids. See §6.2.
	WorkspaceID string
	RunID       string

	// AffectedRepositories names the repository ids (as known to the
	// *workspace.Workspace) the caller believes are relevant. If empty,
	// Build defaults to every repository declared in the workspace.
	AffectedRepositories []string

	// The remaining fields are caller-supplied intent: Punakawan's own code
	// has no way to derive these on its own, so they are copied straight
	// through onto the built ContextDossier's matching fields.
	UserGoal            string
	BusinessOrUserValue string
	CurrentBehavior     string
	DesiredBehavior     string
	ExplicitNonGoals    []string
	Assumptions         []string
	MissingInformation  []string
	Contradictions      []string
	ConfidenceLevel     string
}

// Build assembles a context-dossier protocol.KnowledgeRecord for run
// in.RunID in workspace ws, inspecting each affected repository's git state
// via gitops (through sup) and querying store for existing knowledge (API
// and data contracts, and prior decisions) relevant to the dossier.
//
// Build does not itself call knowledge.Validate or store.Put; callers are
// expected to do so, matching internal/convention.Extract's contract.
func Build(ctx context.Context, ws *workspace.Workspace, sup *tools.Supervisor, store *knowledge.Store, in BuildInput) (protocol.KnowledgeRecord, error) {
	repoIDs := in.AffectedRepositories
	if len(repoIDs) == 0 {
		repoIDs = make([]string, 0, len(ws.Repositories))
		for _, r := range ws.Repositories {
			repoIDs = append(repoIDs, r.ID)
		}
	}

	inspector := gitops.NewInspector(sup)

	var sourceInventory []string
	var existingImplementationPaths []string
	for _, repoID := range repoIDs {
		repoPath, err := ws.RepositoryPath(repoID)
		if err != nil {
			return protocol.KnowledgeRecord{}, fmt.Errorf("dossier: resolve repository %q: %w", repoID, err)
		}

		sourceInventory = append(sourceInventory, fmt.Sprintf("repository %s (%s)", repoID, repoPath))

		status, err := inspector.Status(ctx, repoPath)
		if err != nil {
			return protocol.KnowledgeRecord{}, fmt.Errorf("dossier: inspect status of %q: %w", repoID, err)
		}
		for _, f := range status.ChangedFiles {
			existingImplementationPaths = append(existingImplementationPaths, repoID+":"+f)
		}
	}

	// These lists were previously every api/data-contract and every decision
	// in the whole store, unscoped and unbounded (punokawan-nmw). Scope each
	// to the dossier's affected repositories (records that name a different
	// repository are dropped; unscoped, cross-cutting records are kept) and
	// cap the totals, so the dossier stays proportional to its subject rather
	// than growing with the entire knowledge store.
	repoSet := make(map[string]bool, len(repoIDs))
	for _, id := range repoIDs {
		repoSet[id] = true
	}

	var apiAndDataContracts []string
	for _, t := range []protocol.KnowledgeRecordType{
		protocol.KnowledgeRecordTypeApiContract,
		protocol.KnowledgeRecordTypeDataContract,
	} {
		recs, err := store.ListByType(t)
		if err != nil {
			return protocol.KnowledgeRecord{}, fmt.Errorf("dossier: list knowledge records of type %s: %w", t, err)
		}
		apiAndDataContracts = appendScopedSummaries(apiAndDataContracts, recs, repoSet, maxApiAndDataContracts)
	}

	var relevantPreviousDecisions []string
	decisions, err := store.ListByType(protocol.KnowledgeRecordTypeDecision)
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("dossier: list decision records: %w", err)
	}
	relevantPreviousDecisions = appendScopedSummaries(relevantPreviousDecisions, decisions, repoSet, maxRelevantPreviousDecisions)

	title := in.UserGoal
	if title == "" {
		title = "context dossier for run " + in.RunID
	}

	cd := &protocol.KnowledgeRecordContextDossier{
		AffectedRepositories:        repoIDs,
		SourceInventory:             sourceInventory,
		ExistingImplementationPaths: existingImplementationPaths,
		ApiAndDataContracts:         apiAndDataContracts,
		RelevantPreviousDecisions:   relevantPreviousDecisions,
		ExplicitNonGoals:            in.ExplicitNonGoals,
		Assumptions:                 in.Assumptions,
		MissingInformation:          in.MissingInformation,
		Contradictions:              in.Contradictions,
	}
	if in.UserGoal != "" {
		cd.UserGoal = strPtr(in.UserGoal)
	}
	if in.BusinessOrUserValue != "" {
		cd.BusinessOrUserValue = strPtr(in.BusinessOrUserValue)
	}
	if in.CurrentBehavior != "" {
		cd.CurrentBehavior = strPtr(in.CurrentBehavior)
	}
	if in.DesiredBehavior != "" {
		cd.DesiredBehavior = strPtr(in.DesiredBehavior)
	}
	if in.ConfidenceLevel != "" {
		cd.ConfidenceLevel = strPtr(in.ConfidenceLevel)
	}
	// ExistingTests and DeploymentPath are intentionally left unset: there
	// is no clean, non-invented signal in the codebase today to populate
	// either (see this package's doc comment and the task report for
	// details), so both stay at their zero value rather than a fabricated
	// guess.

	rec := protocol.KnowledgeRecord{
		Id:     "pkw:contextdossier/" + in.WorkspaceID + "/" + in.RunID,
		Type:   protocol.KnowledgeRecordTypeContextDossier,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "punakawan",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
		ContextDossier: cd,
	}
	return rec, nil
}

// Caps bounding the store-derived dossier lists (punokawan-nmw): finite so a
// large knowledge store cannot balloon a single dossier, but generous enough
// to carry a run's genuinely relevant contracts and decisions.
const (
	maxApiAndDataContracts       = 25
	maxRelevantPreviousDecisions = 25
)

// appendScopedSummaries appends summaries of recs to dst, skipping records
// scoped to a repository outside repoSet, until dst reaches cap.
func appendScopedSummaries(dst []string, recs []protocol.KnowledgeRecord, repoSet map[string]bool, cap int) []string {
	for _, r := range recs {
		if len(dst) >= cap {
			break
		}
		if !inAffectedRepositories(r.Scope, repoSet) {
			continue
		}
		dst = append(dst, summarize(r))
	}
	return dst
}

// inAffectedRepositories reports whether a record with recScope belongs to one
// of the dossier's affected repositories. A record that declares no repository
// (or none at all) is cross-cutting and kept; one that names a repository
// outside repoSet is dropped.
func inAffectedRepositories(recScope *protocol.KnowledgeRecordScope, repoSet map[string]bool) bool {
	if recScope == nil || recScope.Repository == nil || *recScope.Repository == "" {
		return true
	}
	return repoSet[*recScope.Repository]
}

// summarize produces a short human-readable reference to a knowledge record
// for inclusion in a dossier's summary lists, preferring the record's title
// and falling back to its id if the title is empty.
func summarize(rec protocol.KnowledgeRecord) string {
	if rec.Title != "" {
		return rec.Title + " (" + rec.Id + ")"
	}
	return rec.Id
}

func strPtr(s string) *string { return &s }
