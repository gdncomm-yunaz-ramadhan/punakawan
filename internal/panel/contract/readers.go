// Package contract defines the read-only reader interfaces the Punakawan
// Panel's HTTP handlers use to reach existing Punakawan data, per
// punakawan-panel-implementation-plan.md §8: "The panel backend must
// consume existing Punakawan service interfaces. It should not scatter
// format-specific parsing throughout HTTP handlers." Implementations live
// in internal/panel/sources, each wrapping an already-existing store
// (workflow, knowledge, beads, evidence, approvals) rather than duplicating
// its state.
package contract

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// WorkspaceSummary is one workspace's panel-facing overview, per
// punakawan-panel-implementation-plan.md §14.2's workspace card. JSON tags
// are load-bearing: HTTP handlers marshal this type directly as the
// GET /api/v1/workspaces response shape.
type WorkspaceSummary struct {
	ID                 string                                 `json:"id"`
	Path               string                                 `json:"path"`
	DisplayName        string                                 `json:"display_name"`
	Availability       protocol.PanelSourceHealthAvailability `json:"availability"`
	RepositoryCount    int                                    `json:"repository_count"`
	ActiveSessionCount int                                    `json:"active_session_count"`
	OpenTaskCount      int                                    `json:"open_task_count"`
	BlockedTaskCount   int                                    `json:"blocked_task_count"`
	KnowledgeCount     int                                    `json:"knowledge_count"`
	LastActivityAt     time.Time                              `json:"last_activity_at"`
	Pinned             bool                                   `json:"pinned"`
}

// WorkspaceDetail extends WorkspaceSummary with per-source health, per
// §14.3.
type WorkspaceDetail struct {
	WorkspaceSummary
	Health []protocol.PanelSourceHealth `json:"health"`
}

// WorkspaceReader lists and describes registered workspaces. Until the
// workspace registry lands (Phase 1), an implementation may legitimately
// describe only the single workspace it is currently running in.
type WorkspaceReader interface {
	List(ctx context.Context) ([]WorkspaceSummary, error)
	Get(ctx context.Context, workspaceID string) (WorkspaceDetail, error)
}

// SessionFilter narrows SessionReader.List, per §11.3's documented query
// filters.
type SessionFilter struct {
	Status     string
	Workflow   string
	Role       string
	TaskID     string
	Repository string
	Limit      int
}

// SessionDetail extends the compact PanelSessionSummary with its raw event
// timeline, per §14.4.
type SessionDetail struct {
	protocol.PanelSessionSummary
	Timeline []protocol.Event
}

// SessionReader lists and describes Punakawan runs ("sessions" in the
// panel's vocabulary), per §8.3.
type SessionReader interface {
	List(ctx context.Context, workspaceID string, filter SessionFilter) ([]protocol.PanelSessionSummary, error)
	Get(ctx context.Context, workspaceID, sessionID string) (SessionDetail, error)
}

// TaskFilter narrows TaskReader.List, per §11.4's documented query filters.
// role and repository_id are accepted by the HTTP handler but not
// represented here: bd's issue schema has no per-task role or repository
// assignment, so there is nothing to filter on - an honest gap, not an
// oversight.
type TaskFilter struct {
	Status        string
	Priority      string
	Type          string
	Assignee      string
	ExternalIssue string
	// Query matches case-insensitively against title and description, bd
	// having no server-side full-text search this package wraps.
	Query string
	Limit int
}

// TaskSummary wraps a bd issue with panel-computed fields bd's own CLI
// output does not include. BoardStatus is derived from bd's real
// readiness computation (bd's GetReadyWork/`bd ready`, the same one `bd
// ready`/`bd list --ready` use), not just the issue's stored Status field:
// an "open" issue with an unmet "blocks" dependency stays stored as
// status=open in bd (verified empirically against bd 1.0.4), so trusting
// Status alone would misreport most real blocked tasks as ready.
// BoardStatus never yields "review" or "failed": bd has no issue status
// for either, an honest gap in the underlying data model rather than an
// oversight here.
type TaskSummary struct {
	beads.ReadyIssue
	BoardStatus     string   `json:"board_status"`
	BlockingReasons []string `json:"blocking_reasons,omitempty"`
	Stale           bool     `json:"stale"`
}

// TaskEdge is one dependency edge in a TaskGraph.
type TaskEdge struct {
	From string
	To   string
	Type string
}

// TaskGraph is the BD work graph's dependency view, per §14.5. Cycles lists
// every dependency cycle found among Edges (each entry is the ordered list
// of issue IDs forming one cycle), per the phase's exit criterion that
// cycles be detected and displayed rather than silently mis-rendered as a
// tree.
type TaskGraph struct {
	Nodes  []TaskSummary
	Edges  []TaskEdge
	Cycles [][]string
}

// TaskReader lists and describes BD issues without mutating them, per
// §8.2.
type TaskReader interface {
	List(ctx context.Context, workspaceID string, filter TaskFilter) ([]TaskSummary, error)
	Get(ctx context.Context, workspaceID, taskID string) (beads.Issue, error)
	Dependencies(ctx context.Context, workspaceID string) (TaskGraph, error)
}

// KnowledgeReader searches and describes durable knowledge, per §8.1 and
// §10. Search reuses internal/search's existing BM25F+relation-expansion
// pipeline (AEP-M6) directly - the panel does not reimplement ranking.
type KnowledgeReader interface {
	// List browses without a query (search.Search returns nothing for an
	// empty query, so this is a separate path per §14.6's filter rail:
	// type, state, repository, source, and staleness, not text relevance).
	List(ctx context.Context, workspaceID string, filter KnowledgeFilter) ([]protocol.KnowledgeRecord, error)
	Search(ctx context.Context, workspaceID string, req search.Request) ([]search.Result, error)
	Get(ctx context.Context, workspaceID, knowledgeID string) (protocol.KnowledgeRecord, error)
	Relations(ctx context.Context, workspaceID, knowledgeID string) ([]protocol.KnowledgeRecord, error)
	// History returns every put/supersede/delete event
	// internal/knowledge.Store has recorded for one record, in append
	// (chronological) order. This is coarser than §14.6's history section
	// ("created/verified/updated/disputed/superseded/invalidated"): bd
	// itself only distinguishes put (create-or-update, not itself
	// distinguishable from a re-verification), supersede, and delete - an
	// honest gap in the underlying event log, not fabricated here.
	History(ctx context.Context, workspaceID, knowledgeID string) ([]knowledge.Event, error)
}

// KnowledgeFilter narrows KnowledgeReader.List, per §14.6's filter rail.
// HasConflict/HasRelation are derived from the record's own embedded
// Relations rather than a separate index: "conflicts-with" is just one of
// the 20 relation types a record can declare on itself.
type KnowledgeFilter struct {
	Type        string
	State       string
	Repository  string
	Source      string
	Stale       bool
	HasConflict bool
	HasRelation bool
	Limit       int
}

// EvidenceReader lists and describes evidence records, per §8.4. Large
// artifacts are not loaded by List/Get: Get returns the
// protocol.EvidenceRecord's metadata (path, hash, type) only. Preview is
// the dedicated, size-bounded path for actually reading an artifact's
// content, per §14.7/Phase 6's ranged log loading, diff summaries, and
// screenshot previews.
type EvidenceReader interface {
	List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error)
	Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error)
	// Preview reads at most limit bytes of the evidence artifact starting
	// at offset (limit<=0 selects a source-defined default), enforcing
	// that the artifact's resolved path lies within the workspace's own
	// evidence directory - the concrete mechanism behind Phase 6's exit
	// criterion "arbitrary workspace paths cannot be served".
	Preview(ctx context.Context, workspaceID, evidenceID string, offset, limit int64) (EvidencePreview, error)
}

// DiffSummary is a cheap, streamed-and-bounded count of a unified diff's
// shape (files touched, lines added/removed), computed without holding
// the full diff in memory at once. Only populated for
// EvidenceRecordTypeGitDiff/ApiDiff previews.
type DiffSummary struct {
	FilesChanged int  `json:"files_changed"`
	Insertions   int  `json:"insertions"`
	Deletions    int  `json:"deletions"`
	Truncated    bool `json:"truncated"`
}

// EvidencePreview is EvidenceReader.Preview's result: either a redacted
// text excerpt (Kind "text") or a size-capped binary blob (Kind "binary",
// e.g. a screenshot), never both.
type EvidencePreview struct {
	Kind        string // "text" or "binary"
	MimeType    string
	Data        []byte
	TotalSize   int64
	Offset      int64
	Truncated   bool
	DiffSummary *DiffSummary
}

// ApprovalFilter narrows ApprovalReader.List.
type ApprovalFilter struct {
	Status string
}

// ApprovalReader lists approval records, per §8.5. The panel's MVP is
// read-only: no Approve/Resolve method exists here on purpose.
type ApprovalReader interface {
	List(ctx context.Context, workspaceID string, filter ApprovalFilter) ([]protocol.ApprovalRecord, error)
}

// GlobalSearchResult pairs one workspace's search.Result with the
// workspace it came from and its fused rank score, per §10.1's global
// search: every registered workspace is queried through the same
// KnowledgeReader.Search path, then merged by rank rather than raw score
// (search.FuseRankedLists) since separate BM25F corpora are not
// comparable on score alone.
type GlobalSearchResult struct {
	WorkspaceID string
	Result      search.Result
	RRFScore    float64
}

// GlobalSearchReader searches every registered workspace at once, per
// §10.1. Unlike KnowledgeReader, it takes no workspaceID: that is the
// entire point of "global."
type GlobalSearchReader interface {
	Search(ctx context.Context, req search.Request) ([]GlobalSearchResult, error)
}
