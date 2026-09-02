// Package deliveryservice puts one provider-neutral Service in front of
// every delivery-starting surface (MCP, workflow invocation, daemon):
// resolving identity (Jira reuse-or-continue, ad-hoc always-fresh) before
// any later reconciliation, session, or telemetry step. See the
// improvement plan's Task 3 for the exact identity semantics this package
// implements against internal/delivery's storage.
package deliveryservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/telemetry"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// JiraHydrator is the exact shape jirahooks.Lifecycle.Hydrate already has:
// reconcile.go depends on this interface, not the concrete type, so a test
// can substitute a fake that never talks to a real Jira adapter. Hydrate
// captures each source as its own durable requirement entry before
// returning, so callers here can rely on that having already happened by
// the time they see the returned sources.
type JiraHydrator interface {
	Hydrate(ctx context.Context, executionID, sessionID, idempotencyKey string) ([]jirahooks.HydratedJiraSource, error)
}

// SourceKind distinguishes a Jira-sourced delivery (reused by canonical
// key until cancelled) from an ad-hoc one (always a new lifetime).
type SourceKind string

const (
	SourceJira  SourceKind = "jira"
	SourceAdhoc SourceKind = "adhoc"
)

// SourceIdentity is the transport-neutral source a caller (MCP tool,
// workflow invocation, daemon route) supplies to StartOrResolve. Provider
// and Tenant are only meaningful for SourceJira; Key must be empty for
// SourceAdhoc.
type SourceIdentity struct {
	Kind     SourceKind
	Provider string
	Tenant   string
	Key      string
}

// NormalizeSource validates and canonicalizes a caller-supplied source:
// a Jira key is uppercased, and both tenant and key must already be
// present (the caller - typically an adapter-aware MCP layer - is
// responsible for supplying a stable adapter-instance tenant fingerprint;
// this only validates it is present, it does not compute one). An ad-hoc
// source must not carry a key. A missing Jira tenant/key is reported as
// NeedUserInput{Kind: missing_context} rather than an error, since the
// caller can resolve it by simply retrying start with the same
// idempotency key once it has the missing field.
func NormalizeSource(in SourceIdentity) (SourceIdentity, *protocol.NeedUserInput, error) {
	switch in.Kind {
	case SourceAdhoc:
		if strings.TrimSpace(in.Key) != "" {
			return SourceIdentity{}, nil, fmt.Errorf("deliveryservice: ad-hoc source must not carry a key")
		}
		return SourceIdentity{Kind: SourceAdhoc}, nil, nil
	case SourceJira:
		tenant := strings.TrimSpace(in.Tenant)
		key := strings.ToUpper(strings.TrimSpace(in.Key))
		var missing []string
		if tenant == "" {
			missing = append(missing, "source.tenant")
		}
		if key == "" {
			missing = append(missing, "source.key")
		}
		if len(missing) > 0 {
			return SourceIdentity{}, &protocol.NeedUserInput{
				Kind:          protocol.NeedUserInputKindMissingContext,
				Question:      "Starting a Jira delivery requires the adapter tenant and the issue key.",
				MissingFields: missing,
			}, nil
		}
		return SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: tenant, Key: key}, nil, nil
	default:
		return SourceIdentity{}, &protocol.NeedUserInput{
			Kind:          protocol.NeedUserInputKindMissingContext,
			Question:      "source.kind must be jira or adhoc.",
			MissingFields: []string{"source.kind"},
		}, nil
	}
}

// toDeliverySource maps the transport-neutral SourceIdentity onto
// internal/delivery's own persistence-level SourceIdentity/SourceKind.
func (in SourceIdentity) toDeliverySource() delivery.SourceIdentity {
	kind := delivery.SourceKindAdhoc
	if in.Kind == SourceJira {
		kind = delivery.SourceKindJira
	}
	return delivery.SourceIdentity{Kind: kind, Provider: in.Provider, Tenant: in.Tenant, Key: in.Key}
}

// RequirementDraft is one requirement source a caller already has content
// for - the same shape delivery.SourceInput already normalizes, named
// here so StartRequest does not depend on a caller having pre-classified
// bare reference strings.
type RequirementDraft struct {
	Provider   string
	ExternalID string
	URL        string
	Title      string
	Summary    string
}

// PlanDraft is the not-yet-saved content of one plan.Plan revision, which
// reconcile turns into an actual plan.Plan via SaveWithKey. It is the
// content half of the pair: a caller that has already saved a plan names
// it by id and revision instead (StartRequest.PlanID, ProjectDraft.PlanID).
type PlanDraft struct {
	Title   string
	Content string
}

// ProjectDraft is one project a delivery should be reconciled onto, plus
// the single unit of work opened there: reconcile performs the
// UpsertProject/AttachProject/CreateParentTask/RouteParentTask/CreateLane
// sequence from it. A caller with several units of work in one repository
// passes one draft per unit, all naming the same slug - the project writes
// are keyed by slug and so collapse to one.
//
// Plan and PlanID are alternatives, not a pair: Plan carries content to
// save, PlanID and PlanRevision name a revision that already exists. Both
// end up linked to this project the same way.
type ProjectDraft struct {
	Slug          string
	RepositoryURL string
	DefaultBranch string
	TaskKey       string
	Title         string
	Plan          PlanDraft
	PlanID        string
	PlanRevision  int
}

// SessionStart is the durable agent session StartOrResolve opens once
// identity and reconciliation are settled.
type SessionStart struct {
	Participant   string
	ResumedFromID string
	WorktreePath  string
	Provider      string
	// ExternalSessionID names the coding-agent client's own session/thread
	// id, when the caller already knows it, for telemetry.Store.Begin's
	// (client_kind, external_session_id) identity. Left empty, StartOrResolve
	// falls back to the newly (or already) opened delivery session's own id
	// - still unique, just not the client-native identity a later lifecycle
	// hook for the same external session would resume under.
	ExternalSessionID string
}

// StartRequest is StartOrResolve's input.
type StartRequest struct {
	IdempotencyKey       string
	Source               *SourceIdentity
	Title                string
	Description          string
	WorkflowDefinitionID string
	Requirements         []RequirementDraft
	// PlanID and PlanRevision name an already-saved cross-project plan
	// revision to record on the orchestration. HighLevelPlan is the
	// content-carrying alternative, saved and linked by reconcile.
	PlanID        string
	PlanRevision  int
	HighLevelPlan PlanDraft
	Projects      []ProjectDraft
	Session       SessionStart
	ResumeToken   string
}

// ReconcileReport summarizes what one StartOrResolve call created,
// updated, or left unchanged.
//
// Skipped is the half that matters when something looks wrong: every
// place reconcile declines to create work - a task naming no requirement
// source, a plan draft with no content - says so here instead of
// continuing quietly, so a caller is never handed an empty delivery with
// a success-shaped response and no explanation.
type ReconcileReport struct {
	Projects     []string `json:"projects"`
	Requirements []string `json:"requirements"`
	Plans        []string `json:"plans"`
	RunnableWork []string `json:"runnable_work,omitempty"`
	Skipped      []string `json:"skipped,omitempty"`
}

// StartResult is StartOrResolve's success output.
//
// Deviation from the plan text: the plan's Step 3 snippet for this struct
// lists only Lifetime/Execution/CreatedLifetime/CreatedExecution/
// Reconciliation, while its own StartRequest (same step) carries a
// Session field the service has nowhere else to report opening a session
// through. Session is added here, populated only when req.Session names a
// participant, so a caller does not have to make a second round trip to
// learn the session id the Delivery contract diagram's "begin durable
// agent session" step just opened.
type StartResult struct {
	Lifetime  delivery.DeliveryLifetime
	Execution delivery.DeliveryExecution
	Session   *delivery.DeliverySession
	// TelemetrySession is populated alongside Session whenever a Service
	// configured with WithTelemetryStore opens one (see StartOrResolve);
	// nil when no telemetry store is configured, matching every existing
	// caller's unchanged behavior.
	TelemetrySession *telemetry.AgentSession
	CreatedLifetime  bool
	CreatedExecution bool
	Reconciliation   ReconcileReport
}
