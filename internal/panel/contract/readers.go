// Package contract defines the read-only reader interfaces the Punakawan
// Panel's HTTP handlers use to reach existing Punakawan data, per
// punakawan-panel-implementation-plan.md §8: "The panel backend must
// consume existing Punakawan service interfaces. It should not scatter
// format-specific parsing throughout HTTP handlers." Implementations live
// in internal/panel/sources, each wrapping an already-existing store
// (workflow, knowledge, evidence, approvals) rather than duplicating
// its state.
package contract

import (
	"context"
	"errors"
	"time"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrWorkspaceUnavailable is returned by the non-workspace sources
// (session, knowledge, evidence, approval) when asked for a
// workspace other than the single one the panel's *app.App was loaded
// for. Those sources only serve that one workspace, so a request for any
// other is the caller's mistake (a 4xx), not a server fault: HTTP handlers
// detect this with errors.Is and answer 404 rather than 500.
var ErrWorkspaceUnavailable = errors.New("workspace is not available on this panel instance")

// ErrPrimaryProject is returned when a caller tries to deregister the
// primary workspace - the one this panel instance's *app.App was loaded
// for. Removing its registry row would not actually remove it: every id
// resolution falls back to the primary root, so the panel would keep
// listing and serving it while the registry claimed otherwise. Refusing is
// the honest answer; the panel is stopped or pointed elsewhere instead.
var ErrPrimaryProject = errors.New("the primary workspace cannot be removed from this panel instance")

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
	KnowledgeCount     int                                    `json:"knowledge_count"`
	LastActivityAt     time.Time                              `json:"last_activity_at"`
	Pinned             bool                                   `json:"pinned"`
	// Primary is true for the one workspace this panel instance's *app.App
	// was loaded for - the only workspace whose sessions, knowledge, and
	// approvals this instance can actually serve. The frontend uses it
	// to disable drill-down navigation for every other (non-primary)
	// workspace, whose sub-resources would otherwise 404.
	Primary bool `json:"primary"`
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
	// SkipCounts omits each session's EvidenceCount/WarningCount/ErrorCount,
	// which otherwise cost one evidence-ledger and one event-journal file
	// scan per returned run. Set this when the caller does not render those
	// fields (e.g. the Overview page, which needs every run's status for
	// its active/stale/failed detection but never displays per-run counts) -
	// paying for a per-run file scan that nothing shows on screen was the
	// dominant cost of a slow overview load.
	SkipCounts bool
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
	WorkspaceID string        `json:"workspace_id"`
	Result      search.Result `json:"result"`
	RRFScore    float64       `json:"rrf_score"`
}

// GlobalSearchReader searches every registered workspace at once, per
// §10.1. Unlike KnowledgeReader, it takes no workspaceID: that is the
// entire point of "global."
type GlobalSearchReader interface {
	Search(ctx context.Context, req search.Request) ([]GlobalSearchResult, error)
}

// ProjectSummary is one project's panel-facing overview, per the project
// performance plan §3/§14. A project shares its id with the workspace it is
// rooted in (project id == registry workspace id). The count fields are
// sourced from the existing WorkspaceReader rather than recomputed here
// (§8's "one snapshot, reused everywhere"): the project source composes a
// WorkspaceReader instead of duplicating its deep bd/dolt/git inspection.
// JSON tags are load-bearing: handlers marshal this type directly.
type ProjectSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Path               string `json:"path"`
	Pinned             bool   `json:"pinned"`
	Primary            bool   `json:"primary"`
	Availability       string `json:"availability"`
	RepositoryCount    int    `json:"repository_count"`
	KnowledgeCount     int    `json:"knowledge_count"`
	ActiveSessionCount int    `json:"active_session_count"`
	MetadataCount      int    `json:"metadata_count"`
}

// ProjectReader lists and describes projects and applies the plan's metadata
// mutations. Reads (List/Summary/Get) never mutate; the three metadata
// methods each load the project fresh, apply an optimistically-locked change
// through internal/project, and persist a new immutable revision, returning
// the updated project so the handler can render the changed entry and new
// revision (§4.3/§15).
//
// The metadata mutations live on the reader (not directly in the HTTP
// handler) so the handler stays transport-only: it need not know how a
// project id maps to a workspace root, only the internal/project error kinds
// it maps to status codes. Errors returned wrap internal/project's exported
// error vars (ErrRevisionConflict, ErrDuplicateKey, ErrSecretRejected,
// ErrInvalidValue, ErrMissingField, ErrKeyNotFound) and
// ErrWorkspaceUnavailable for an unknown project id, all matchable with
// errors.Is.
type ProjectReader interface {
	List(ctx context.Context) ([]ProjectSummary, error)
	Summary(ctx context.Context, projectID string) (ProjectSummary, error)
	Get(ctx context.Context, projectID string) (*project.Project, error)
	// Deregister drops the project's row from the panel's workspace
	// registry so the panel stops listing and serving it. It is a registry
	// operation only: the workspace directory, its .punakawan tree,
	// knowledge database, tasks, and repositories are all left untouched on
	// disk, and re-registering the same path restores it. Returns
	// ErrWorkspaceUnavailable for an unknown id and ErrPrimaryProject for
	// the primary workspace.
	Deregister(ctx context.Context, projectID string) error
	AddMetadata(ctx context.Context, projectID string, entry project.MetadataEntry, baseRevision int) (*project.Project, error)
	UpdateMetadata(ctx context.Context, projectID, key string, newDescription *string, newValue any, baseRevision int) (*project.Project, error)
	DeleteMetadata(ctx context.Context, projectID, key string, baseRevision int) (*project.Project, error)
}

// RoleCapabilityInfo is one role's owned-capability catalog: the fixed set of
// capability keys that role may carry (internal/roleconfig.OwnedCapabilities),
// in Panel/prompt order. GetRoles returns one per role so the Panel knows which
// toggles to render for each role without hard-coding the catalog client-side.
type RoleCapabilityInfo struct {
	Role         string   `json:"role"`
	Capabilities []string `json:"capabilities"`
}

// RolesReader reads and mutates a project's four-role configuration, per the
// role-config distinguished-improvements plan Part I. It mirrors the metadata
// mutations on ProjectReader: reads (GetRoles) never mutate; UpdateRole/ResetRole
// each load the config fresh, apply an optimistically-locked change through
// internal/roleconfig, and persist a new immutable revision, returning the
// updated configuration so the handler can render the changed roles and new
// revision.
//
// The contract depends on internal/roleconfig directly (no import cycle:
// roleconfig imports only pkg/protocol, not this package), so the patch shape
// is roleconfig.Patch verbatim rather than a translated local copy. Errors
// returned wrap roleconfig's exported error vars (ErrRevisionConflict,
// ErrUnknownRole, ErrInvalidStyle, ErrInvalidMode, ErrUnownedCapability) and
// ErrWorkspaceUnavailable for an unknown project id, all matchable with
// errors.Is.
type RolesReader interface {
	// GetRoles returns the current configuration, the owned-capability catalog
	// for all four roles, and an error. The catalog is static per role but is
	// returned alongside the config so the Panel renders in one round-trip.
	GetRoles(ctx context.Context, projectID string) (*protocol.RoleConfiguration, []RoleCapabilityInfo, error)
	UpdateRole(ctx context.Context, projectID, role string, patch roleconfig.Patch, baseRevision int) (*protocol.RoleConfiguration, error)
	ResetRole(ctx context.Context, projectID, role string, baseRevision int) (*protocol.RoleConfiguration, error)
}

// ContradictionReader reads and mutates a project's Contradiction Ledger, per
// the role-config distinguished-improvements plan Part II §16-22. It mirrors
// the metadata/role mutations on the other readers: reads never mutate; the
// mutators load the ledger fresh, apply the stateless internal/contradiction
// change, and persist, returning the resulting record so the handler can render
// it. Errors returned wrap contradiction's exported vars (ErrNotFound,
// ErrIllegalTransition) and ErrWorkspaceUnavailable for an unknown project id,
// all matchable with errors.Is.
type ContradictionReader interface {
	ListContradictions(ctx context.Context, projectID string) ([]protocol.Contradiction, error)
	GetContradiction(ctx context.Context, projectID, id string) (*protocol.Contradiction, error)
	// CreateContradiction persists c through contradiction.Put; the handler
	// assigns an id when the caller left it empty.
	CreateContradiction(ctx context.Context, projectID string, c protocol.Contradiction) (*protocol.Contradiction, error)
	ProposeContradictionResolution(ctx context.Context, projectID, id, proposedStatement, rationale string, requiresHumanConfirmation bool) (*protocol.Contradiction, error)
	ResolveContradiction(ctx context.Context, projectID, id, statement, by string) (*protocol.Contradiction, error)
	AcceptContradictionDivergence(ctx context.Context, projectID, id, by string) (*protocol.Contradiction, error)
}

// ImpactReader reads and queries a project's Cross-Repository Impact Graph, per
// the plan Part III §23-31. Nodes/QueryImpact never mutate; RefreshImpact
// re-runs the stateless internal/impact builders to reconcile the persisted
// graph with the current workspace. QueryImpact returns impact.ImpactResult
// verbatim (a derived query view, not a stored entity).
type ImpactReader interface {
	ImpactNodes(ctx context.Context, projectID string) ([]protocol.ImpactNode, error)
	ImpactNode(ctx context.Context, projectID, nodeID string) (protocol.ImpactNode, bool, error)
	QueryImpact(ctx context.Context, projectID, subjectID string, depth int, include []string) (impact.ImpactResult, error)
	RefreshImpact(ctx context.Context, projectID string) error
}

// DossierReader reads and mutates a project's durable Change Dossiers, per the
// plan Part IV §32-39. ListDossiers returns the current dossier per id (a
// lightweight summary carrying id/title/status/... without claims or evidence);
// GetDossier returns the full dossier.Loaded (dossier plus its claims and
// evidence). FinalizeDossier surfaces *dossier.BlockingError verbatim so the
// handler can 409 with the blocker list; the claim mutators surface
// ErrSelfVerification/ErrClaimNotFound.
type DossierReader interface {
	ListDossiers(ctx context.Context, projectID string) ([]protocol.ChangeDossier, error)
	CreateDossier(ctx context.Context, projectID string, d protocol.ChangeDossier) (protocol.ChangeDossier, error)
	GetDossier(ctx context.Context, projectID, id string) (dossier.Loaded, error)
	AddDossierClaim(ctx context.Context, projectID, id string, claim protocol.DossierClaim) (protocol.DossierClaim, error)
	VerifyDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error)
	DisputeDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error)
	AddDossierEvidence(ctx context.Context, projectID, id string, ev protocol.DossierEvidence) (protocol.DossierEvidence, error)
	FinalizeDossier(ctx context.Context, projectID, id string) error
	ExportDossierMarkdown(ctx context.Context, projectID, id string) (string, error)
	ExportDossierJSON(ctx context.Context, projectID, id string) ([]byte, error)
}

// DeliveryReader reads and mutates delivery orchestrations by proxying to
// the daemon's own delivery.Store (internal/daemon/delivery.go) over its
// authenticated loopback transport - the daemon, not this panel instance,
// is the only process allowed to open delivery.Store's storage directly
// (see internal/daemon.Daemon's own doc comment). The three mutators and
// GetDeliveryView/WatchDeliveryView mirror daemon.Client's own methods
// exactly; this interface exists only so HTTP handlers and the events
// watcher depend on a narrow contract instead of the concrete *daemon.Client,
// matching every other reader in this package.
type DeliveryReader interface {
	ListDeliveries(ctx context.Context) ([]*protocol.DeliveryOrchestration, error)
	// GetDeliveryView returns orchestrationID's current view immediately.
	// sinceSeq mirrors delivery.BuildDeliveryViewSince: pass a prior
	// response's LatestSeq to populate NewlyRunnableLaneIDs.
	GetDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*delivery.DeliveryView, error)
	// WatchDeliveryView is GetDeliveryView, except the daemon blocks
	// server-side for up to waitSeconds waiting for LatestSeq to advance
	// past sinceSeq - the events package's DeliveryWatcher is this
	// method's only caller, long-polling it in a loop per orchestration.
	WatchDeliveryView(ctx context.Context, orchestrationID string, sinceSeq, waitSeconds int) (*delivery.DeliveryView, error)
	AnswerDeliveryQuestion(ctx context.Context, orchestrationID string, in daemon.AnswerDeliveryQuestionRequest) (*delivery.DeliveryView, error)
	ApproveProjectDelivery(ctx context.Context, orchestrationID string, in daemon.ApproveProjectDeliveryRequest) (*delivery.DeliveryView, error)
	CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*delivery.DeliveryView, error)
	// GetDeliveryEvidence fetches one lane-scoped evidence artifact's raw
	// bytes and media type by id, scoped to orchestrationID.
	GetDeliveryEvidence(ctx context.Context, orchestrationID, evidenceID string) ([]byte, string, error)
}
