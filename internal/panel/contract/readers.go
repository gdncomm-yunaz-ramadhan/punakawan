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
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// WorkspaceSummary is one workspace's panel-facing overview, per
// punakawan-panel-implementation-plan.md §14.2's workspace card.
type WorkspaceSummary struct {
	ID                 string
	Path               string
	DisplayName        string
	Availability       protocol.PanelSourceHealthAvailability
	RepositoryCount    int
	ActiveSessionCount int
	OpenTaskCount      int
	BlockedTaskCount   int
	KnowledgeCount     int
	LastActivityAt     time.Time
	Pinned             bool
}

// WorkspaceDetail extends WorkspaceSummary with per-source health, per
// §14.3.
type WorkspaceDetail struct {
	WorkspaceSummary
	Health []protocol.PanelSourceHealth
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
type TaskFilter struct {
	Status        string
	Priority      string
	Type          string
	Assignee      string
	ExternalIssue string
	Limit         int
}

// TaskEdge is one dependency edge in a TaskGraph.
type TaskEdge struct {
	From string
	To   string
	Type string
}

// TaskGraph is the BD work graph's dependency view, per §14.5.
type TaskGraph struct {
	Nodes []beads.ReadyIssue
	Edges []TaskEdge
}

// TaskReader lists and describes BD issues without mutating them, per
// §8.2.
type TaskReader interface {
	List(ctx context.Context, workspaceID string, filter TaskFilter) ([]beads.ReadyIssue, error)
	Get(ctx context.Context, workspaceID, taskID string) (beads.Issue, error)
	Dependencies(ctx context.Context, workspaceID string) (TaskGraph, error)
}

// KnowledgeReader searches and describes durable knowledge, per §8.1 and
// §10. Search reuses internal/search's existing BM25F+relation-expansion
// pipeline (AEP-M6) directly - the panel does not reimplement ranking.
type KnowledgeReader interface {
	Search(ctx context.Context, workspaceID string, req search.Request) ([]search.Result, error)
	Get(ctx context.Context, workspaceID, knowledgeID string) (protocol.KnowledgeRecord, error)
	Relations(ctx context.Context, workspaceID, knowledgeID string) ([]protocol.KnowledgeRecord, error)
}

// EvidenceReader lists and describes evidence records, per §8.4. Large
// artifacts are not loaded by these calls: Get returns the
// protocol.EvidenceRecord's metadata (path, hash, type), and reading the
// artifact itself is left to a later phase's dedicated preview endpoints.
type EvidenceReader interface {
	List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error)
	Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error)
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
