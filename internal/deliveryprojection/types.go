// Package deliveryprojection is the one panel-facing read model over a
// delivery: a compact DeliverySummary for the list page and a
// DeliveryDetail for the detail page, both carrying the same
// ProjectionRevision so a caller can tell a stale list entry apart from a
// freshly loaded detail. Neither type exposes scheduler-internal concepts
// (lanes, blocked counts, pending questions, a lane-derived next action) -
// those stay in internal/delivery.DeliveryView, which this package does
// not replace.
package deliveryprojection

import (
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Status mirrors protocol.DeliveryOrchestrationStatus's four values.
type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

// SourceKind distinguishes a Jira-sourced delivery from an ad-hoc one,
// mirroring internal/delivery.SourceKind.
type SourceKind string

const (
	SourceKindJira  SourceKind = "jira"
	SourceKindAdhoc SourceKind = "adhoc"
)

// Source is a delivery's originating Jira issue, or absent for an ad-hoc
// delivery with no single originating issue.
type Source struct {
	Kind SourceKind `json:"kind"`
	// Key is the Jira issue key; empty for an ad-hoc source.
	Key string `json:"key,omitempty"`
	// Title is the most recently captured requirement title for Key.
	Title string `json:"title,omitempty"`
	// Status is the last locally observed Jira status - the target status
	// of the most recent transition this delivery itself requested. Empty
	// when no transition has ever been enqueued, rather than guessed from a
	// live Jira call this projection never makes.
	Status string `json:"status,omitempty"`
}

// ProjectRef is one project a delivery touches.
type ProjectRef struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

// PlanRef is the delivery's own cross-project high-level plan link, kept
// deliberately lightweight (DeliveryDetail.Plan carries the full content).
type PlanRef struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	Objective string `json:"objective"`
	Status    string `json:"status,omitempty"`
}

// WorkflowRef names the workflow definition a delivery was configured
// from, if any.
type WorkflowRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Progress is a delivery's most recently reported progress.
type Progress struct {
	Percent    *float64  `json:"percent,omitempty"`
	Summary    string    `json:"summary"`
	ReportedAt time.Time `json:"reported_at"`
}

// Session is a delivery's most recently started session.
type Session struct {
	Participant string     `json:"participant,omitempty"`
	Provider    string     `json:"provider,omitempty"`
	Model       string     `json:"model,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	StoppedAt   *time.Time `json:"stopped_at,omitempty"`
}

// Usage is a delivery's cumulative, additive-across-sessions agent usage.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheTokens  int64 `json:"cache_tokens"`
	ToolCalls    int64 `json:"tool_calls"`
	ElapsedMS    int64 `json:"elapsed_ms"`
	// EstimatedCosts maps currency to amount; more than one entry means the
	// delivery's contributing sessions priced in more than one currency.
	// Empty when no contributing snapshot ever named a cost at all.
	EstimatedCosts map[string]float64 `json:"estimated_costs"`
	// PricingComplete is false whenever any contributing usage was priced
	// against an unknown model rate - the totals above are then a partial,
	// never a fabricated, sum.
	PricingComplete bool `json:"pricing_complete"`
}

// DeliverySummary is the panel list page's one row: enough to render,
// search, and sort every delivery without a per-card follow-up fetch.
type DeliverySummary struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Status   Status       `json:"status"`
	Source   *Source      `json:"source,omitempty"`
	Projects []ProjectRef `json:"projects"`
	Plan     *PlanRef     `json:"plan,omitempty"`
	Workflow *WorkflowRef `json:"workflow,omitempty"`
	Progress *Progress    `json:"progress,omitempty"`
	Session  *Session     `json:"session,omitempty"`
	Usage    Usage        `json:"usage"`

	UpdatedAt time.Time `json:"updated_at"`
	// Cancellable is true while the delivery can still be cancelled
	// (pending or active).
	Cancellable bool `json:"cancellable"`
	// ProjectionRevision is delivery_projection_versions' current revision
	// for this delivery - the number a caller compares across List/Detail
	// calls, and passes back as since_revision to the watch endpoint. It is
	// unrelated to the underlying orchestration's own event-log revision,
	// which cancel/complete's own optimistic-concurrency check still uses
	// (see DeliveryDetail.OrchestrationRevision).
	ProjectionRevision int `json:"projection_revision"`
}

// LaneRef is one unit of work opened in a project: the executable thing a
// delivery is made of. The projection carries it because a reader
// otherwise has no way to tell a delivery that decomposed correctly from
// one that produced nothing - both look identical from projects alone.
type LaneRef struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	ProjectSlug  string   `json:"project_slug,omitempty"`
	ParentTaskID string   `json:"parent_task_id,omitempty"`
	Title        string   `json:"title,omitempty"`
	Status       string   `json:"status"`
	BlockedBy    []string `json:"blocked_by,omitempty"`
	PullRequest  string   `json:"pull_request,omitempty"`
}

// ProjectPlanDetail is one project's exact detailed plan link, plus the
// plan lineage's current head revision so a caller can tell "this
// delivery still points at the head" from "the plan moved on since".
type ProjectPlanDetail struct {
	ProjectID    string    `json:"project_id"`
	ProjectSlug  string    `json:"project_slug"`
	Plan         plan.Plan `json:"plan"`
	HeadRevision int       `json:"head_revision"`
}

// JiraTouchedItem is one parent task Jira work is mapped to.
type JiraTouchedItem struct {
	ParentTaskID   string     `json:"parent_task_id"`
	JiraIssueKey   string     `json:"jira_issue_key"`
	TouchCount     int        `json:"touch_count"`
	FirstTouchedAt *time.Time `json:"first_touched_at,omitempty"`
	LastTouchedAt  *time.Time `json:"last_touched_at,omitempty"`
}

// JiraTransition is one requested Jira status transition, recorded from
// the durable provider write this delivery enqueued for it - never a live
// Jira read.
type JiraTransition struct {
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status"`
	// Status is the write's own outbox status (pending, succeeded, ...),
	// not the Jira issue's status.
	Status     string    `json:"status"`
	OccurredAt time.Time `json:"occurred_at"`
}

// WriteHealth summarizes the durable provider writes a delivery has
// enqueued against one provider, by their current outbox status.
type WriteHealth struct {
	Pending     int `json:"pending"`
	Retrying    int `json:"retrying"`
	Reconciling int `json:"reconciling"`
	Failed      int `json:"failed"`
	Succeeded   int `json:"succeeded"`
	Cancelled   int `json:"cancelled"`
}

// JiraDetail is a Jira-sourced delivery's provider-specific detail. Absent
// entirely for an ad-hoc delivery.
type JiraDetail struct {
	IssueKey     string                  `json:"issue_key"`
	ParentStatus string                  `json:"parent_status,omitempty"`
	TouchedItems []JiraTouchedItem       `json:"touched_items"`
	Transitions  []JiraTransition        `json:"transitions"`
	WorkLogs     []delivery.WorkLogEntry `json:"worklogs"`
	WriteHealth  WriteHealth             `json:"write_health"`
}

// GitHubDetail is a delivery's GitHub pull-request/review detail. Absent
// when the delivery has never proposed a PR review.
type GitHubDetail struct {
	Repository        string                    `json:"repository,omitempty"`
	PullRequestNumber int                       `json:"pull_request_number,omitempty"`
	HeadSHA           string                    `json:"head_sha,omitempty"`
	Reviews           []delivery.GitHubPRReview `json:"reviews"`
	WriteHealth       WriteHealth               `json:"write_health"`
}

// ProviderWrite is one durable outbox intent this delivery has enqueued,
// regardless of provider.
type ProviderWrite struct {
	ID           string    `json:"id"`
	Adapter      string    `json:"adapter"`
	Operation    string    `json:"operation"`
	TargetKey    string    `json:"target_key"`
	Status       string    `json:"status"`
	AttemptCount int       `json:"attempt_count"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// payloadJSON is the raw outbox payload, kept only so a Jira transition
	// write's from/target status can be extracted for JiraDetail.Transitions
	// - never marshaled (unexported), since a provider write's payload shape
	// is operation-specific and not part of this projection's public
	// contract.
	payloadJSON string
}

// SessionDetail is one delivery session plus the checkpoints it recorded.
type SessionDetail struct {
	delivery.DeliverySession
	Checkpoints []delivery.SessionCheckpoint `json:"checkpoints"`
}

// ActivityEntry is one entry in DeliveryDetail's merged domain/provider/
// session timeline, sorted oldest first.
type ActivityEntry struct {
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	OccurredAt time.Time `json:"occurred_at"`
}

// DeliveryDetail is the panel detail page's full read model: every
// DeliverySummary field, plus the full plan, per-project plans,
// requirement sources, provider-specific detail, sessions, provider
// writes, and one merged activity timeline.
type DeliveryDetail struct {
	DeliverySummary

	Description string `json:"description,omitempty"`
	// OrchestrationRevision is the underlying orchestration's own
	// event-log revision - the value cancel/complete's optimistic
	// concurrency check compares an expected_revision against. It is a
	// different counter than ProjectionRevision (delivery_projection_
	// versions), which this projection uses for list/detail consistency
	// and watch polling instead.
	OrchestrationRevision int `json:"orchestration_revision"`

	Lanes              []LaneRef                    `json:"lanes"`
	PlanDetail         *plan.Plan                   `json:"plan_detail,omitempty"`
	ProjectPlans       []ProjectPlanDetail          `json:"project_plans,omitempty"`
	RequirementSources []protocol.RequirementSource `json:"requirement_sources,omitempty"`
	Jira               *JiraDetail                  `json:"jira,omitempty"`
	GitHub             *GitHubDetail                `json:"github,omitempty"`
	Sessions           []SessionDetail              `json:"sessions,omitempty"`
	ProviderWrites     []ProviderWrite              `json:"provider_writes,omitempty"`
	Activity           []ActivityEntry              `json:"activity"`
}
