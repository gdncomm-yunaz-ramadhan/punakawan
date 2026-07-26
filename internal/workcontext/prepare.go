package workcontext

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Selection reasons recorded on every context item, so nothing is presented
// without an explanation of why it was selected (plan §4.4/§4.5 exit
// criterion: every returned item includes a selection reason).
const (
	ReasonRequiredByWorkflow = "required_by_workflow"
	ReasonRequested          = "requested"
	ReasonPrioritySelected   = "priority_selected"
	ReasonRetrievalRecipe    = "verified_retrieval_recipe"
	ReasonScopedSearch       = "scoped_search_match"
)

// SearchFunc runs a scoped knowledge search. It is injected (rather than
// importing internal/app) so the caller supplies the index-rebuilding,
// lock-holding search path and this service stays a pure, testable composer.
type SearchFunc func(req search.Request) ([]search.Result, error)

// Request is the fully-resolved input to a context preparation. The caller has
// already loaded the candidate workflow definitions and chosen the scope; this
// service does the deterministic composition.
type Request struct {
	// WorkspaceRoot is where the project's metadata lives.
	WorkspaceRoot string

	// Definitions are the candidate workflow definitions to resolve against.
	Definitions []workflowdef.Definition
	// WorkflowID / Capability / Intent select a workflow per workflowdef.Query.
	// All empty means an ad hoc run (no definition).
	WorkflowID string
	Capability string
	Intent     string
	// Inputs are the caller-supplied workflow inputs (validated/defaulted).
	Inputs map[string]any

	// RetrievalQuery drives the scoped knowledge search; empty skips it.
	RetrievalQuery        string
	RequestedMetadataKeys []string
	KnowledgeTypes        []string
	KnowledgeLimit        int
	IncludeAssumed        bool
	Scope                 search.Scope

	// RoleConfigRevision, when set, is folded into the digest so a run's
	// context is reproducible against the role config in effect.
	RoleConfigRevision *int

	Now time.Time
}

// Item reasons and typed views returned to the caller for display; the
// canonical form for persistence is Snapshot.
type MetadataItem struct {
	Key    string
	Value  any
	Reason string
}

type KnowledgeItem struct {
	Id          string
	ContentHash string
	Validity    string
	Reason      string
	Summary     string
}

type MissingItem struct {
	Kind string
	Key  string
}

// Result is the composed, deterministic context. Snapshot is the immutable
// form stamped onto the run; the typed slices are the same data for display.
type Result struct {
	Definition     *workflowdef.Definition
	Candidates     []workflowdef.Definition // populated only on ambiguous selector
	ResolvedInputs map[string]any

	// StepProgress is the initial per-step state for a definition-backed run:
	// a step with no unmet input_from dependency starts "ready", one with
	// dependencies starts "pending" (plan §5.3). Empty for ad hoc runs.
	StepProgress []protocol.WorkflowRunStepProgressElem

	MetadataRevision int
	Metadata         []MetadataItem
	Knowledge        []KnowledgeItem // accepted guidance (verified/observed, +assumed on request)
	Caution          []KnowledgeItem // inferred: clearly-marked caution channel
	Missing          []MissingItem
	ResolvedRecipeID string

	Digest   string
	Snapshot protocol.WorkflowRunContextSnapshot
}

// Prepare composes the three pillars into a bounded, deterministic snapshot
// (plan §4.4). Steps: resolve/validate the workflow, validate+default inputs,
// resolve required metadata (missing → awaiting-clarification signal), select
// optional metadata via the project priority selector, retrieve scoped
// knowledge filtered by lifecycle eligibility, resolve (never execute) a
// verified retrieval recipe, then build the digest and snapshot.
//
// recipes may be nil to skip recipe resolution. searchFn may be nil to skip
// knowledge retrieval (e.g. when no knowledge store is open).
func Prepare(req Request, searchFn SearchFunc, recipes *recipe.Repository) (Result, error) {
	var res Result

	// 1. Resolve the workflow definition (explicit id or exact selector).
	if req.WorkflowID != "" || req.Capability != "" {
		def, candidates, err := workflowdef.Resolve(req.Definitions, workflowdef.Query{
			ID:         req.WorkflowID,
			Capability: req.Capability,
			Intent:     req.Intent,
		})
		if err != nil {
			// Ambiguity is returned to the caller with candidates, not guessed.
			res.Candidates = candidates
			return res, err
		}
		res.Definition = &def
	}

	// 2. Validate and default declared inputs; initialize step progress.
	if res.Definition != nil {
		resolved, err := workflowdef.ResolveInputs(*res.Definition, req.Inputs)
		if err != nil {
			return res, err
		}
		res.ResolvedInputs = resolved
		res.StepProgress = initialStepProgress(*res.Definition)
	} else {
		res.ResolvedInputs = req.Inputs
	}

	// Load project metadata once for both required-metadata resolution and
	// optional priority selection.
	proj, err := project.Load(req.WorkspaceRoot)
	if err != nil {
		return res, err
	}
	res.MetadataRevision = proj.Revision

	added := make(map[string]bool)

	// 3. Resolve required metadata; anything absent becomes a missing entry.
	if res.Definition != nil {
		for _, key := range res.Definition.RequiredMetadata {
			if entry, ok := proj.MetadataFor(key); ok {
				res.Metadata = append(res.Metadata, MetadataItem{Key: entry.Key, Value: entry.Value, Reason: ReasonRequiredByWorkflow})
				added[entry.Key] = true
			} else {
				res.Missing = append(res.Missing, MissingItem{Kind: "metadata", Key: key})
			}
		}
	}

	// 4. Select optional metadata through the existing priority selector.
	capability, intent := req.Capability, req.Intent
	if res.Definition != nil && len(res.Definition.Selectors) > 0 {
		// Prefer the definition's own first selector for metadata namespacing
		// when the caller did not pass explicit capability/intent.
		if capability == "" {
			capability = res.Definition.Selectors[0].Capability
			intent = res.Definition.Selectors[0].Intent
		}
	}
	selector := project.PrioritySelector{}
	for _, entry := range selector.Select(*proj, capability, intent, req.RequestedMetadataKeys) {
		if added[entry.Key] {
			continue
		}
		reason := ReasonPrioritySelected
		for _, rk := range req.RequestedMetadataKeys {
			if rk == entry.Key {
				reason = ReasonRequested
				break
			}
		}
		res.Metadata = append(res.Metadata, MetadataItem{Key: entry.Key, Value: entry.Value, Reason: reason})
		added[entry.Key] = true
	}

	// 5. Retrieve scoped knowledge, filtered by lifecycle eligibility.
	if searchFn != nil && req.RetrievalQuery != "" {
		limit := req.KnowledgeLimit
		if limit <= 0 {
			limit = 10
		}
		results, err := searchFn(search.Request{
			Query: req.RetrievalQuery,
			Scope: req.Scope,
			Types: req.KnowledgeTypes,
			Limit: limit,
		})
		if err != nil {
			return res, err
		}
		seenKnowledge := make(map[string]bool)
		for _, r := range results {
			item := toKnowledgeItem(r)
			seenKnowledge[r.Id] = true
			switch ClassifyValidity(r.Record.Validity.State) {
			case Eligible:
				res.Knowledge = append(res.Knowledge, item)
			case OnRequest:
				if req.IncludeAssumed {
					res.Knowledge = append(res.Knowledge, item)
				}
			case Caution:
				res.Caution = append(res.Caution, item)
			default:
				// Excluded: disputed/stale/superseded/invalid/draft/validating
				// never appear as accepted guidance.
			}
		}

		// 6. Resolve (do not execute) a verified retrieval recipe for an exact
		// capability/intent match. Executing a recipe is a live fetch that would
		// break digest determinism, so preparation only surfaces the recipe
		// reference; the agent executes it during its actual work.
		if recipes != nil && capability != "" {
			resolver := recipe.Resolver{Repo: recipes}
			resolution, rerr := resolver.Resolve(recipe.OperationRequest{
				Capability:  capability,
				Intent:      intent,
				WorkspaceID: proj.ID,
			})
			if rerr == nil && resolution.Outcome == recipe.OutcomeResolved && resolution.Selected != nil {
				id := resolution.Selected.Record.Id
				res.ResolvedRecipeID = id
				if !seenKnowledge[id] {
					item := KnowledgeItem{
						Id:          id,
						ContentHash: contentHash(resolution.Selected.Record),
						Validity:    string(resolution.Selected.Record.Validity.State),
						Reason:      ReasonRetrievalRecipe,
						Summary:     deref(knowledge.BoundedSummary(resolution.Selected.Record)),
					}
					res.Knowledge = append(res.Knowledge, item)
				}
			}
		}
	}

	// 7. Build the deterministic digest and the immutable snapshot.
	res.Digest = digest(res, req.RoleConfigRevision)
	res.Snapshot = buildSnapshot(res, req.Now)
	return res, nil
}

// initialStepProgress derives each step's starting state from its input_from
// dependencies: a step with no dependency is immediately "ready"; one that
// depends on an earlier step starts "pending" until that step completes (plan
// §5.3). Nothing is completed at initialization.
func initialStepProgress(def workflowdef.Definition) []protocol.WorkflowRunStepProgressElem {
	if len(def.Steps) == 0 {
		return nil
	}
	out := make([]protocol.WorkflowRunStepProgressElem, 0, len(def.Steps))
	for _, st := range def.Steps {
		state := protocol.WorkflowRunStepProgressElemStateReady
		if len(st.InputFrom) > 0 {
			state = protocol.WorkflowRunStepProgressElemStatePending
		}
		out = append(out, protocol.WorkflowRunStepProgressElem{StepId: st.ID, State: state})
	}
	return out
}

func toKnowledgeItem(r search.Result) KnowledgeItem {
	reason := ReasonScopedSearch
	if len(r.Explanation) > 0 {
		reason = "matched: " + joinReasons(r.Explanation)
	}
	return KnowledgeItem{
		Id:          r.Id,
		ContentHash: contentHash(r.Record),
		Validity:    string(r.Record.Validity.State),
		Reason:      reason,
		Summary:     deref(knowledge.BoundedSummary(r.Record)),
	}
}

func joinReasons(exp []string) string {
	out := exp[0]
	for _, e := range exp[1:] {
		out += "; " + e
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// contentHash prefers the record's own recorded source content hash and falls
// back to hashing the record's canonical JSON, so the digest is stable and
// every knowledge reference carries a hash.
func contentHash(r protocol.KnowledgeRecord) string {
	if r.Source.ContentHash != nil && *r.Source.ContentHash != "" {
		return *r.Source.ContentHash
	}
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return knowledge.ContentHash(b)
}

func buildSnapshot(res Result, now time.Time) protocol.WorkflowRunContextSnapshot {
	snap := protocol.WorkflowRunContextSnapshot{}
	if !now.IsZero() {
		t := now
		snap.PreparedAt = &t
	}
	if res.Digest != "" {
		d := res.Digest
		snap.Digest = &d
	}
	rev := res.MetadataRevision
	snap.ProjectMetadataRevision = &rev

	for _, m := range res.Metadata {
		snap.Metadata = append(snap.Metadata, protocol.WorkflowRunContextSnapshotMetadataElem{
			Key:    m.Key,
			Value:  m.Value,
			Reason: m.Reason,
		})
	}
	// Accepted knowledge only in the snapshot's knowledge list; caution items
	// are display-only and must not be persisted as accepted context.
	for _, k := range res.Knowledge {
		elem := protocol.WorkflowRunContextSnapshotKnowledgeElem{Id: k.Id, Reason: k.Reason}
		if k.ContentHash != "" {
			ch := k.ContentHash
			elem.ContentHash = &ch
		}
		if k.Validity != "" {
			v := k.Validity
			elem.Validity = &v
		}
		snap.Knowledge = append(snap.Knowledge, elem)
	}
	for _, m := range res.Missing {
		elem := protocol.WorkflowRunContextSnapshotMissingElem{Kind: m.Kind}
		if m.Key != "" {
			key := m.Key
			elem.Key = &key
		}
		snap.Missing = append(snap.Missing, elem)
	}
	return snap
}

// digest hashes the substantive, order-normalized context so identical inputs
// and store revisions always produce the same value (plan §4.4 exit
// criterion). PreparedAt is deliberately excluded — a timestamp would make an
// otherwise-identical context digest differently.
func digest(res Result, roleConfigRevision *int) string {
	type kv struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	type kh struct {
		Id          string `json:"id"`
		ContentHash string `json:"content_hash"`
		Validity    string `json:"validity"`
	}
	var defRef *protocol.WorkflowRunDefinitionRef
	if res.Definition != nil {
		defRef = &protocol.WorkflowRunDefinitionRef{
			Id:          res.Definition.ID,
			Revision:    res.Definition.Revision,
			ContentHash: res.Definition.ContentHash(),
		}
	}
	meta := make([]kv, 0, len(res.Metadata))
	for _, m := range res.Metadata {
		meta = append(meta, kv{Key: m.Key, Value: m.Value})
	}
	sort.Slice(meta, func(i, j int) bool { return meta[i].Key < meta[j].Key })

	know := make([]kh, 0, len(res.Knowledge))
	for _, k := range res.Knowledge {
		know = append(know, kh{Id: k.Id, ContentHash: k.ContentHash, Validity: k.Validity})
	}
	sort.Slice(know, func(i, j int) bool { return know[i].Id < know[j].Id })

	miss := make([]MissingItem, len(res.Missing))
	copy(miss, res.Missing)
	sort.Slice(miss, func(i, j int) bool {
		if miss[i].Kind != miss[j].Kind {
			return miss[i].Kind < miss[j].Kind
		}
		return miss[i].Key < miss[j].Key
	})

	payload := struct {
		Definition         *protocol.WorkflowRunDefinitionRef `json:"definition"`
		Inputs             map[string]any                     `json:"inputs"`
		MetadataRevision   int                                `json:"metadata_revision"`
		Metadata           []kv                               `json:"metadata"`
		Knowledge          []kh                               `json:"knowledge"`
		Missing            []MissingItem                      `json:"missing"`
		RoleConfigRevision *int                               `json:"role_config_revision"`
	}{
		Definition:         defRef,
		Inputs:             res.ResolvedInputs,
		MetadataRevision:   res.MetadataRevision,
		Metadata:           meta,
		Knowledge:          know,
		Missing:            miss,
		RoleConfigRevision: roleConfigRevision,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return knowledge.ContentHash(b)
}
