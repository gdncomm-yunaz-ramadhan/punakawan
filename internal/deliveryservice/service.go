package deliveryservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/telemetry"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Service is the one composition entry point every delivery-starting
// surface (MCP, workflow invocation, daemon) routes through: it resolves
// identity before any later reconciliation, session, or telemetry step.
type Service struct {
	deliveries *delivery.Store
	plans      *plan.Store
	// hydrator is nil unless WithJiraHydrator is passed to New, in which
	// case StartOrResolve's reconciliation step never hydrates a Jira
	// source's parent/subtasks into requirement sources - a caller with no
	// hydrator configured is expected to supply RequirementDrafts itself.
	hydrator JiraHydrator
	// telemetry is nil unless WithTelemetryStore is passed to New, in
	// which case StartOrResolve never opens a durable agent_sessions row
	// for the delivery session it starts - a caller with no telemetry
	// store configured keeps behaving exactly as it did before telemetry
	// existed.
	telemetry *telemetry.Store
	// agents is nil unless WithAgentRegistry is passed to New, in which
	// case a begun telemetry session's RoleVersion stays empty - a
	// caller with no agent registry configured keeps behaving exactly as
	// it did before RoleVersion existed.
	agents agent.AgentRegistry
	// orgs is nil unless WithJiraOrgResolver is passed to New, in which
	// case a Jira source's tenant is taken at face value - which is what
	// a host that distinguishes no organisations wants.
	orgs JiraOrgResolver
}

// JiraOrgResolver turns whatever a caller wrote in source.tenant into the
// organisation this host actually knows by that name: blank resolves to
// the default, a name resolves to its canonical spelling, and a name this
// host has no credentials for is an error naming the ones it does.
//
// Putting the tenant through this before it reaches delivery identity is
// what keeps "gdn" and "gdncomm" from becoming two deliveries for one
// issue, while still letting two genuinely different sites hold the same
// issue key.
//
// It is given the issue key as well, so a host with several organisations
// can answer "the default one, which can actually see this issue" rather
// than "the default one" - and return a decision to put to a human when
// it cannot. A resolver that only canonicalizes names ignores the key.
type JiraOrgResolver func(ctx context.Context, org, issueKey string) (string, *protocol.NeedUserInput, error)

// WithJiraOrgResolver wires the organisation resolution StartOrResolve
// applies to a Jira source before anything is written.
func WithJiraOrgResolver(resolve JiraOrgResolver) Option {
	return func(s *Service) { s.orgs = resolve }
}

// Option configures optional Service dependencies. Every existing New
// call site (no options) keeps compiling and behaving exactly as before.
type Option func(*Service)

// WithJiraHydrator wires the Jira parent/subtask hydration reconcile.go
// uses to turn a Jira source into its own durable requirement sources.
func WithJiraHydrator(h JiraHydrator) Option {
	return func(s *Service) { s.hydrator = h }
}

// WithTelemetryStore wires the additive, cumulative session-usage store
// StartOrResolve begins a durable agent session against whenever it opens
// a delivery session.
func WithTelemetryStore(store *telemetry.Store) Option {
	return func(s *Service) { s.telemetry = store }
}

// WithAgentRegistry wires the internal/agent role registry a begun
// telemetry session resolves its Participant against, so
// AgentSession.RoleVersion is populated whenever the participant names one
// of the four known roles.
func WithAgentRegistry(reg agent.AgentRegistry) Option {
	return func(s *Service) { s.agents = reg }
}

// New wires a Service over the already-open delivery and plan stores.
func New(deliveries *delivery.Store, plans *plan.Store, opts ...Option) *Service {
	s := &Service{deliveries: deliveries, plans: plans}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// StartOrResolve resolves req.Source to one delivery lifetime and
// execution, validating every required piece of context before writing
// anything. It returns a structured NeedUserInput - never a partially
// written delivery - when required context is missing or contradictory.
func (s *Service) StartOrResolve(ctx context.Context, req StartRequest) (StartResult, *protocol.NeedUserInput, error) {
	if req.Source == nil {
		return StartResult{}, &protocol.NeedUserInput{
			Kind:          protocol.NeedUserInputKindMissingContext,
			Question:      "start_delivery requires a source (jira or adhoc).",
			MissingFields: []string{"source"},
		}, nil
	}
	source := *req.Source
	if source.Kind == SourceJira && s.orgs != nil {
		if strings.TrimSpace(source.Tenant) == "" {
			// An issue that already has a lifetime already answered this
			// question; asking a second time would ask the same person
			// twice and risk a second delivery for one issue.
			if remembered, ok, err := s.deliveries.ActiveJiraOrgForKey(ctx, source.Key); err != nil {
				return StartResult{}, nil, err
			} else if ok {
				source.Tenant = remembered
			}
		}
		org, needsInput, err := s.orgs(ctx, source.Tenant, source.Key)
		if err != nil {
			// A tenant this host holds no credentials for is the caller's
			// to correct, not a server fault: answering with the question
			// lets the agent retry with a name that exists instead of
			// opening a delivery that could never write anything back.
			return StartResult{}, &protocol.NeedUserInput{
				Kind:          protocol.NeedUserInputKindMissingContext,
				Question:      err.Error(),
				MissingFields: []string{"source.tenant"},
			}, nil
		}
		if needsInput != nil {
			return StartResult{}, needsInput, nil
		}
		source.Tenant = org
	}
	normalized, needsInput, err := NormalizeSource(source)
	if err != nil {
		return StartResult{}, nil, err
	}
	if needsInput != nil {
		return StartResult{}, needsInput, nil
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return StartResult{}, nil, fmt.Errorf("deliveryservice: idempotency_key is required")
	}

	resolved, err := s.deliveries.StartOrResolveExecution(ctx, req.IdempotencyKey, normalized.toDeliverySource(), delivery.OrchestrationOptions{
		Title:                strings.TrimSpace(req.Title),
		Description:          strings.TrimSpace(req.Description),
		WorkflowDefinitionID: req.WorkflowDefinitionID,
		PlanID:               strings.TrimSpace(req.PlanID),
		PlanRevision:         req.PlanRevision,
	})
	if err != nil {
		return StartResult{}, nil, err
	}
	// Reconciliation always runs after resolving identity, whether this
	// call just created the lifetime/execution or reused an already-active
	// one: a second start_delivery call against the same Jira issue with a
	// newly discovered subtask or project must reconcile that new work onto
	// the reused execution rather than short-circuiting because nothing was
	// freshly created.
	report, err := s.reconcile(ctx, req, resolved)
	if err != nil {
		return StartResult{}, nil, err
	}
	result := StartResult{
		Lifetime:         *resolved.Lifetime,
		Execution:        *resolved.Execution,
		CreatedLifetime:  resolved.CreatedLifetime,
		CreatedExecution: resolved.CreatedExecution,
		Reconciliation:   report,
	}
	if strings.TrimSpace(req.Session.Participant) != "" {
		session, err := s.deliveries.StartSession(ctx, req.IdempotencyKey+":session", resolved.Execution.ID, "", req.Session.Participant, req.Session.ResumedFromID, req.Session.WorktreePath, req.Session.Provider)
		if err != nil {
			return StartResult{}, nil, err
		}
		result.Session = session
		if s.telemetry != nil {
			result.TelemetrySession = s.beginTelemetrySession(ctx, resolved, req.Session, session)
		}
	}
	return result, nil, nil
}

// beginTelemetrySession opens (or resumes) a durable agent_sessions row
// for the delivery session StartOrResolve just opened. It never fails
// StartOrResolve's own result over a telemetry hiccup - tracking is
// additive and secondary to actually starting the delivery - it only logs
// and returns nil.
func (s *Service) beginTelemetrySession(ctx context.Context, resolved *delivery.ResolvedExecution, sessionStart SessionStart, opened *delivery.DeliverySession) *telemetry.AgentSession {
	clientKind := strings.TrimSpace(sessionStart.Provider)
	if clientKind == "" {
		clientKind = "unspecified"
	}
	externalID := strings.TrimSpace(sessionStart.ExternalSessionID)
	if externalID == "" {
		// No client-native session id was supplied; the delivery session's
		// own id is already unique, so it stands in as the external
		// identity rather than leaving telemetry unbegun entirely. A later
		// lifecycle hook call that does know the real external session id
		// begins a second, additive agent_sessions row instead of
		// resuming this one - see TotalsByDelivery's additive-across-
		// sessions semantics.
		externalID = opened.ID
	}
	roleVersion := ""
	if s.agents != nil {
		if spec, err := s.agents.Get(sessionStart.Participant); err == nil {
			roleVersion = spec.Version
		}
	}
	session, err := s.telemetry.Begin(ctx, telemetry.BeginRequest{
		DeliveryID: resolved.Execution.OrchestrationID, ExecutionID: resolved.Execution.ID,
		ClientKind: clientKind, ExternalSessionID: externalID,
		Participant: sessionStart.Participant, RoleVersion: roleVersion, Provider: sessionStart.Provider, WorktreePath: sessionStart.WorktreePath,
	})
	if err != nil {
		slog.Warn("deliveryservice: begin telemetry session", "orchestration_id", resolved.Execution.OrchestrationID, "error", err)
		return nil
	}
	return &session
}

// Complete atomically completes orchestrationID's execution and returns
// its refreshed view.
//
// Deviation from the plan text: the plan's signature names a return type
// delivery.DeliveryDetail, which does not exist yet - the improvement
// plan's "one compact list, one exact detail" panel projection is a later
// task's package (internal/deliveryprojection, per the plan's own file
// structure table). This returns the real, already-existing
// *delivery.DeliveryView instead of fabricating that later type early.
func (s *Service) Complete(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*delivery.DeliveryView, error) {
	if _, err := s.deliveries.CompleteOrchestration(ctx, idempotencyKey, orchestrationID, expectedRevision); err != nil {
		return nil, err
	}
	s.finalizeStrayTelemetrySessions(ctx, idempotencyKey, orchestrationID, "delivery_completed")
	return s.deliveries.BuildDeliveryView(ctx, orchestrationID)
}

// finalizeStrayTelemetrySessions best-effort closes any agent_sessions
// row a client's own SessionEnd hook never finalized before this delivery
// itself completed - otherwise such a session stays "active" forever,
// even though the delivery it was tracking is done. It never fails the
// caller (Complete has already durably completed the delivery by the time
// this runs) - only logs.
func (s *Service) finalizeStrayTelemetrySessions(ctx context.Context, idempotencyKey, orchestrationID, stopReason string) {
	if s.telemetry == nil {
		return
	}
	active, err := s.telemetry.ListActiveByOrchestration(ctx, orchestrationID)
	if err != nil {
		slog.Warn("deliveryservice: list active telemetry sessions", "orchestration_id", orchestrationID, "error", err)
		return
	}
	for _, session := range active {
		if _, _, err := s.telemetry.Finalize(ctx, telemetry.FinalizeRequest{
			SessionID: session.ID, StopID: idempotencyKey + ":" + session.ID, StopReason: stopReason,
		}); err != nil {
			slog.Warn("deliveryservice: finalize stray telemetry session", "session_id", session.ID, "error", err)
		}
	}
}

// Cancel cancels orchestrationID and returns its refreshed view. See
// Complete's doc comment for the same delivery.DeliveryView deviation.
func (s *Service) Cancel(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*delivery.DeliveryView, error) {
	if _, err := s.deliveries.CancelOrchestration(ctx, idempotencyKey, orchestrationID, expectedRevision); err != nil {
		return nil, err
	}
	s.finalizeStrayTelemetrySessions(ctx, idempotencyKey, orchestrationID, "delivery_cancelled")
	return s.deliveries.BuildDeliveryView(ctx, orchestrationID)
}
