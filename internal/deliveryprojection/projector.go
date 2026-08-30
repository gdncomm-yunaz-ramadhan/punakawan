package deliveryprojection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/telemetry"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// reader is the minimal read surface Projector's own batch queries need -
// satisfied by *sql.DB directly, and by a call-counting wrapper in tests
// that must bound how many round trips ListSummaries makes regardless of
// how many deliveries exist.
type reader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Projector serves the panel's one delivery list and detail projection.
// It composes the already-existing delivery/plan/telemetry stores for
// business logic it must not duplicate, plus its own batch queries
// (through read) for the tables none of those stores expose a
// batch-shaped read for.
type Projector struct {
	deliveries *delivery.Store
	plans      *plan.Store
	telemetry  *telemetry.Store
	read       reader
}

// NewProjector builds a Projector over deliveries' own shared storage
// kernel, so it never opens a second connection pool to the same
// database.
func NewProjector(deliveries *delivery.Store) *Projector {
	db := deliveries.DB()
	return &Projector{
		deliveries: deliveries,
		plans:      plan.NewStore(db),
		telemetry:  telemetry.NewStore(db),
		read:       db.Reader(),
	}
}

// ListFilter narrows ListSummaries. It is empty today - every delivery is
// listed - and exists so a caller does not have to change its call shape
// when a real filter (status, project, search) is added.
type ListFilter struct{}

// ListSummaries returns one DeliverySummary per known delivery, oldest
// first, using a fixed, small number of batch queries regardless of how
// many deliveries exist: every query below selects across every
// orchestration at once and groups the rows in memory by orchestration
// id, rather than looping a per-delivery lookup.
func (p *Projector) ListSummaries(ctx context.Context, _ ListFilter) ([]DeliverySummary, error) {
	// 1: every orchestration's event log, reduced to its current record
	// and derived title.
	states, ids, err := loadOrchestrationStates(ctx, p.read)
	if err != nil {
		return nil, err
	}

	// 2: lifetimes/executions/projection versions, joined in one query.
	lifetimes, err := p.batchLifetimes(ctx)
	if err != nil {
		return nil, err
	}

	// 3: project slugs.
	slugs, err := p.batchProjectSlugs(ctx)
	if err != nil {
		return nil, err
	}

	// 4: exact high-level (delivery-scoped) plan revisions.
	plans, err := p.batchDeliveryPlans(ctx)
	if err != nil {
		return nil, err
	}

	// 5: latest progress report per execution.
	progress, err := p.batchLatestProgress(ctx)
	if err != nil {
		return nil, err
	}

	// 6: latest session per execution.
	sessions, err := p.batchLatestSession(ctx)
	if err != nil {
		return nil, err
	}

	// 7: aggregated telemetry per orchestration.
	usage, err := p.batchUsage(ctx)
	if err != nil {
		return nil, err
	}

	// 8: last locally known Jira status per orchestration.
	jiraStatus, err := p.batchLatestJiraTransition(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]DeliverySummary, 0, len(ids))
	for _, id := range ids {
		orch := states[id].orchestration
		title := states[id].title

		summary := DeliverySummary{
			ID:          id,
			Title:       title,
			Status:      Status(orch.Status),
			UpdatedAt:   orch.UpdatedAt,
			Cancellable: isCancellable(orch.Status),
			Usage:       usageOrEmpty(usage[id]),
		}
		for _, projectID := range orch.ProjectIds {
			summary.Projects = append(summary.Projects, ProjectRef{ID: projectID, Slug: slugs[projectID]})
		}
		if lt, ok := lifetimes[id]; ok {
			summary.ProjectionRevision = lt.ProjectionRevision
			if lt.SourceKind == string(delivery.SourceKindJira) {
				summary.Source = &Source{Kind: SourceKindJira, Key: lt.JiraIssueKey, Title: title, Status: jiraStatus[id]}
			} else {
				summary.Source = &Source{Kind: SourceKindAdhoc}
			}
		}
		if ref, ok := plans[id]; ok {
			summary.Plan = &ref
		}
		if orch.WorkflowDefinitionId != nil {
			summary.Workflow = &WorkflowRef{ID: *orch.WorkflowDefinitionId}
		}
		if execID, ok := executionIDFor(lifetimes, id); ok {
			if pr, ok := progress[execID]; ok {
				summary.Progress = &pr
			}
			if sess, ok := sessions[execID]; ok {
				summary.Session = &sess
			}
		}
		out = append(out, summary)
	}
	return out, nil
}

// GetDetail returns orchestrationID's full DeliveryDetail, derived from
// one BuildDeliveryView call (the same event-log replay and lifecycle
// assembly every other delivery caller already shares - GetDetail must
// not re-implement that reduction) plus the handful of small,
// orchestration-scoped lookups BuildDeliveryView does not itself carry
// (exact plan content, provider writes, GitHub review detail).
func (p *Projector) GetDetail(ctx context.Context, orchestrationID string) (*DeliveryDetail, error) {
	view, err := p.deliveries.BuildDeliveryView(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	revision, err := p.projectionRevision(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}

	detail := &DeliveryDetail{
		DeliverySummary: DeliverySummary{
			ID:                 view.Orchestration.Id,
			Title:              view.Title,
			Status:             Status(view.Orchestration.Status),
			UpdatedAt:          view.Orchestration.UpdatedAt,
			Cancellable:        isCancellable(view.Orchestration.Status),
			Usage:              usageFromProjection(view.Telemetry),
			ProjectionRevision: revision,
		},
		Description:           view.Description,
		OrchestrationRevision: view.Orchestration.Revision,
		Activity:              []ActivityEntry{},
	}
	for _, proj := range view.Projects {
		detail.Projects = append(detail.Projects, ProjectRef{ID: proj.ProjectID, Slug: proj.ProjectSlug})
	}
	if view.Orchestration.WorkflowDefinitionId != nil {
		detail.Workflow = &WorkflowRef{ID: *view.Orchestration.WorkflowDefinitionId}
	}

	if view.PlanID != "" {
		planRevision, err := p.plans.GetRevision(ctx, view.PlanID, view.PlanRevision)
		if err != nil && !errors.Is(err, plan.ErrNotFound) {
			return nil, fmt.Errorf("deliveryprojection: load plan %s@%d: %w", view.PlanID, view.PlanRevision, err)
		}
		if err == nil {
			detail.Plan = &PlanRef{ID: view.PlanID, Revision: view.PlanRevision, Objective: planRevision.Objective, Status: planRevision.Status}
			detail.PlanDetail = &planRevision
		}
	}

	for _, link := range view.ProjectPlans {
		revision, err := p.plans.GetRevision(ctx, link.PlanID, link.PlanRevision)
		if err != nil {
			if errors.Is(err, plan.ErrNotFound) {
				continue
			}
			return nil, fmt.Errorf("deliveryprojection: load project plan %s@%d: %w", link.PlanID, link.PlanRevision, err)
		}
		head, err := p.plans.Get(ctx, link.PlanID)
		if err != nil {
			return nil, fmt.Errorf("deliveryprojection: load plan head %s: %w", link.PlanID, err)
		}
		slug := ""
		for _, ref := range detail.Projects {
			if ref.ID == link.ProjectID {
				slug = ref.Slug
			}
		}
		detail.ProjectPlans = append(detail.ProjectPlans, ProjectPlanDetail{
			ProjectID: link.ProjectID, ProjectSlug: slug, Plan: revision, HeadRevision: head.Revision,
		})
	}

	sources, err := p.deliveries.ListRequirementSources(ctx, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: list requirement sources: %w", err)
	}
	for _, s := range sources {
		detail.RequirementSources = append(detail.RequirementSources, *s)
	}

	writes, err := p.batchProviderWrites(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	detail.ProviderWrites = writes

	if view.Lifecycle != nil && view.Lifecycle.Case.SourceKind == string(delivery.SourceKindJira) {
		jira := &JiraDetail{IssueKey: view.Lifecycle.Case.JiraIssueKey, WorkLogs: view.WorkLogs}
		for _, item := range view.Lifecycle.WorkItems {
			jira.TouchedItems = append(jira.TouchedItems, JiraTouchedItem{
				ParentTaskID: item.ParentTaskID, JiraIssueKey: item.JiraIssueKey, TouchCount: item.TouchCount,
				FirstTouchedAt: item.FirstTouchedAt, LastTouchedAt: item.LastTouchedAt,
			})
		}
		for _, w := range writes {
			if w.Operation != "atlassian.transitionJiraIssue" {
				continue
			}
			from, to := transitionStatuses(w)
			jira.Transitions = append(jira.Transitions, JiraTransition{FromStatus: from, ToStatus: to, Status: w.Status, OccurredAt: w.UpdatedAt})
			jira.ParentStatus = to
		}
		jira.WriteHealth = writeHealth(writes, "atlassian")
		detail.Jira = jira
		if detail.Source == nil {
			detail.Source = &Source{Kind: SourceKindJira, Key: jira.IssueKey, Title: detail.Title, Status: jira.ParentStatus}
		}
	} else {
		detail.Source = &Source{Kind: SourceKindAdhoc}
	}

	if repo, prNumber, headSHA, ok := githubPRFromLanes(view.Lanes); ok {
		reviews, err := p.batchGitHubReviews(ctx, view.Lifecycle)
		if err != nil {
			return nil, err
		}
		detail.GitHub = &GitHubDetail{
			Repository: repo, PullRequestNumber: prNumber, HeadSHA: headSHA,
			Reviews: reviews, WriteHealth: writeHealth(writes, "github"),
		}
	}

	if view.Lifecycle != nil {
		checkpointsBySession := map[string][]delivery.SessionCheckpoint{}
		for _, c := range view.Lifecycle.Checkpoints {
			checkpointsBySession[c.SessionID] = append(checkpointsBySession[c.SessionID], c)
		}
		for _, sess := range view.Lifecycle.Sessions {
			detail.Sessions = append(detail.Sessions, SessionDetail{DeliverySession: sess, Checkpoints: checkpointsBySession[sess.ID]})
		}
		if len(view.Lifecycle.Progress) > 0 {
			latest := view.Lifecycle.Progress[len(view.Lifecycle.Progress)-1]
			detail.Progress = &Progress{Percent: latest.ProgressPercent, Summary: latest.Summary, ReportedAt: latest.ReportedAt}
		}
		if len(view.Lifecycle.Sessions) > 0 {
			latest := view.Lifecycle.Sessions[len(view.Lifecycle.Sessions)-1]
			detail.Session = &Session{Participant: latest.Participant, Provider: latest.Provider, Status: latest.Status, StartedAt: latest.StartedAt, StoppedAt: latest.EndedAt}
		}
		for _, sess := range view.Lifecycle.Sessions {
			detail.Activity = append(detail.Activity, ActivityEntry{Kind: "session_started", Summary: "session started by " + sess.Participant, OccurredAt: sess.StartedAt})
		}
		for _, prog := range view.Lifecycle.Progress {
			detail.Activity = append(detail.Activity, ActivityEntry{Kind: "progress_reported", Summary: prog.Summary, OccurredAt: prog.ReportedAt})
		}
	}
	for _, ev := range view.Timeline {
		detail.Activity = append(detail.Activity, ActivityEntry{Kind: string(ev.Type), Summary: string(ev.Type), OccurredAt: ev.OccurredAt})
	}
	for _, ja := range view.JiraActivity {
		detail.Activity = append(detail.Activity, ActivityEntry{Kind: ja.EventType, Summary: ja.IssueKey + ": " + ja.EventType, OccurredAt: ja.FiredAt})
	}
	for _, w := range writes {
		detail.Activity = append(detail.Activity, ActivityEntry{Kind: "provider_write_" + w.Status, Summary: w.Adapter + " " + w.Operation, OccurredAt: w.UpdatedAt})
	}
	sort.SliceStable(detail.Activity, func(i, j int) bool { return detail.Activity[i].OccurredAt.Before(detail.Activity[j].OccurredAt) })

	return detail, nil
}

func (p *Projector) projectionRevision(ctx context.Context, orchestrationID string) (int, error) {
	revision, err := p.deliveries.GetProjectionRevision(ctx, orchestrationID)
	if errors.Is(err, delivery.ErrNotFound) {
		return 0, nil
	}
	return revision, err
}

func isCancellable(status protocol.DeliveryOrchestrationStatus) bool {
	return status == protocol.DeliveryOrchestrationStatusPending || status == protocol.DeliveryOrchestrationStatusActive
}

func usageOrEmpty(u Usage) Usage {
	if u.EstimatedCosts == nil {
		u.EstimatedCosts = map[string]float64{}
	}
	return u
}

func usageFromProjection(u telemetry.UsageProjection) Usage {
	out := Usage{
		InputTokens:     u.Counters.InputTokens,
		OutputTokens:    u.Counters.OutputTokens,
		CacheTokens:     u.Counters.CacheWriteTokens + u.Counters.CacheReadTokens,
		ToolCalls:       u.Counters.ToolCalls,
		ElapsedMS:       u.Counters.ElapsedMS,
		EstimatedCosts:  map[string]float64{},
		PricingComplete: u.TelemetryStatus == "complete",
	}
	if u.EstimatedCost != nil {
		out.EstimatedCosts[u.EstimatedCost.Currency] = u.EstimatedCost.Amount
	}
	return out
}

// --- batch queries -----------------------------------------------------

type lifetimeRow struct {
	ExecutionID        string
	SourceKind         string
	JiraIssueKey       string
	ProjectionRevision int
}

func (p *Projector) batchLifetimes(ctx context.Context) (map[string]lifetimeRow, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT e.orchestration_id, e.id, c.source_kind, c.jira_issue_key, COALESCE(pv.revision, 0)
FROM delivery_executions e
JOIN delivery_cases c ON c.id = e.case_id
LEFT JOIN delivery_projection_versions pv ON pv.orchestration_id = e.orchestration_id`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch lifetimes: %w", err)
	}
	defer rows.Close()
	out := map[string]lifetimeRow{}
	for rows.Next() {
		var orchestrationID string
		var row lifetimeRow
		if err := rows.Scan(&orchestrationID, &row.ExecutionID, &row.SourceKind, &row.JiraIssueKey, &row.ProjectionRevision); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan lifetime: %w", err)
		}
		out[orchestrationID] = row
	}
	return out, rows.Err()
}

func executionIDFor(lifetimes map[string]lifetimeRow, orchestrationID string) (string, bool) {
	lt, ok := lifetimes[orchestrationID]
	if !ok {
		return "", false
	}
	return lt.ExecutionID, true
}

func (p *Projector) batchProjectSlugs(ctx context.Context) (map[string]string, error) {
	rows, err := p.read.QueryContext(ctx, `SELECT id, slug FROM delivery_projects`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch project slugs: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan project slug: %w", err)
		}
		out[id] = slug
	}
	return out, rows.Err()
}

func (p *Projector) batchDeliveryPlans(ctx context.Context) (map[string]PlanRef, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT link.orchestration_id, link.plan_id, link.plan_revision, pr.data
FROM delivery_plan_links link
JOIN plan_revisions pr ON pr.plan_id = link.plan_id AND pr.revision = link.plan_revision
WHERE link.scope = 'delivery'`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch delivery plans: %w", err)
	}
	defer rows.Close()
	out := map[string]PlanRef{}
	for rows.Next() {
		var orchestrationID, planID, data string
		var revision int
		if err := rows.Scan(&orchestrationID, &planID, &revision, &data); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan delivery plan: %w", err)
		}
		var decoded plan.Plan
		if err := json.Unmarshal([]byte(data), &decoded); err != nil {
			return nil, fmt.Errorf("deliveryprojection: decode plan %s: %w", planID, err)
		}
		out[orchestrationID] = PlanRef{ID: planID, Revision: revision, Objective: decoded.Objective, Status: decoded.Status}
	}
	return out, rows.Err()
}

func (p *Projector) batchLatestProgress(ctx context.Context) (map[string]Progress, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT execution_id, progress_percent, summary, reported_at FROM delivery_progress_reports
ORDER BY execution_id, reported_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch progress: %w", err)
	}
	defer rows.Close()
	out := map[string]Progress{}
	for rows.Next() {
		var executionID, summary, reportedAt string
		var percent sql.NullFloat64
		if err := rows.Scan(&executionID, &percent, &summary, &reportedAt); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan progress: %w", err)
		}
		if _, seen := out[executionID]; seen {
			continue
		}
		reported, err := parseTimestamp(reportedAt)
		if err != nil {
			return nil, err
		}
		prog := Progress{Summary: summary, ReportedAt: reported}
		if percent.Valid {
			prog.Percent = &percent.Float64
		}
		out[executionID] = prog
	}
	return out, rows.Err()
}

func (p *Projector) batchLatestSession(ctx context.Context) (map[string]Session, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT execution_id, participant, provider, status, started_at, ended_at FROM delivery_sessions
ORDER BY execution_id, started_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch sessions: %w", err)
	}
	defer rows.Close()
	out := map[string]Session{}
	for rows.Next() {
		var executionID, participant, provider, status, startedAt string
		var endedAt sql.NullString
		if err := rows.Scan(&executionID, &participant, &provider, &status, &startedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan session: %w", err)
		}
		if _, seen := out[executionID]; seen {
			continue
		}
		started, err := parseTimestamp(startedAt)
		if err != nil {
			return nil, err
		}
		sess := Session{Participant: participant, Provider: provider, Status: status, StartedAt: started}
		if endedAt.Valid && endedAt.String != "" {
			ended, err := parseTimestamp(endedAt.String)
			if err != nil {
				return nil, err
			}
			sess.StoppedAt = &ended
		}
		out[executionID] = sess
	}
	return out, rows.Err()
}

func (p *Projector) batchUsage(ctx context.Context) (map[string]Usage, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT sess.orchestration_id, sess.telemetry_status,
       COALESCE(snap.input_tokens, 0), COALESCE(snap.output_tokens, 0),
       COALESCE(snap.cache_write_tokens, 0), COALESCE(snap.cache_read_tokens, 0),
       COALESCE(snap.tool_calls, 0), COALESCE(snap.elapsed_ms, 0),
       snap.estimated_cost_json
FROM agent_sessions sess
LEFT JOIN agent_usage_snapshots snap ON snap.session_id = sess.id`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch usage: %w", err)
	}
	defer rows.Close()

	type costJSON struct {
		Amount   float64 `json:"amount,omitempty"`
		Currency string  `json:"currency,omitempty"`
		Known    bool    `json:"known"`
	}
	out := map[string]Usage{}
	incomplete := map[string]bool{}
	for rows.Next() {
		var orchestrationID, telemetryStatus string
		var input, output, cacheWrite, cacheRead, toolCalls, elapsed int64
		var costRaw sql.NullString
		if err := rows.Scan(&orchestrationID, &telemetryStatus, &input, &output, &cacheWrite, &cacheRead, &toolCalls, &elapsed, &costRaw); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan usage: %w", err)
		}
		u, ok := out[orchestrationID]
		if !ok {
			u = Usage{EstimatedCosts: map[string]float64{}, PricingComplete: true}
		}
		u.InputTokens += input
		u.OutputTokens += output
		u.CacheTokens += cacheWrite + cacheRead
		u.ToolCalls += toolCalls
		u.ElapsedMS += elapsed
		if telemetryStatus != "complete" {
			incomplete[orchestrationID] = true
		}
		if costRaw.Valid {
			var cost costJSON
			if err := json.Unmarshal([]byte(costRaw.String), &cost); err != nil {
				return nil, fmt.Errorf("deliveryprojection: decode usage cost: %w", err)
			}
			if cost.Known {
				u.EstimatedCosts[cost.Currency] += cost.Amount
			}
		}
		out[orchestrationID] = u
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id := range incomplete {
		u := out[id]
		u.PricingComplete = false
		out[id] = u
	}
	return out, nil
}

func (p *Projector) batchLatestJiraTransition(ctx context.Context) (map[string]string, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT orchestration_id, payload_json FROM provider_write_intents
WHERE operation = 'atlassian.transitionJiraIssue'
ORDER BY orchestration_id, updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: batch jira transitions: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var orchestrationID, payloadJSON string
		if err := rows.Scan(&orchestrationID, &payloadJSON); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan jira transition: %w", err)
		}
		if _, seen := out[orchestrationID]; seen {
			continue
		}
		var payload struct {
			TargetStatus string `json:"target_status"`
		}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("deliveryprojection: decode jira transition payload: %w", err)
		}
		out[orchestrationID] = payload.TargetStatus
	}
	return out, rows.Err()
}

func (p *Projector) batchProviderWrites(ctx context.Context, orchestrationID string) ([]ProviderWrite, error) {
	rows, err := p.read.QueryContext(ctx, `
SELECT id, adapter_id, operation, target_key, payload_json, status, attempt_count, last_error_redacted, created_at, updated_at
FROM provider_write_intents WHERE orchestration_id = ? ORDER BY created_at, id`, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: list provider writes: %w", err)
	}
	defer rows.Close()
	var out []ProviderWrite
	for rows.Next() {
		var w ProviderWrite
		var createdAt, updatedAt, payload string
		if err := rows.Scan(&w.ID, &w.Adapter, &w.Operation, &w.TargetKey, &payload, &w.Status, &w.AttemptCount, &w.LastError, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan provider write: %w", err)
		}
		w.payloadJSON = payload
		created, err := parseTimestamp(createdAt)
		if err != nil {
			return nil, err
		}
		updated, err := parseTimestamp(updatedAt)
		if err != nil {
			return nil, err
		}
		w.CreatedAt, w.UpdatedAt = created, updated
		out = append(out, w)
	}
	return out, rows.Err()
}

func (p *Projector) batchGitHubReviews(ctx context.Context, lifecycle *delivery.DeliveryLifecycle) ([]delivery.GitHubPRReview, error) {
	if lifecycle == nil {
		return nil, nil
	}
	rows, err := p.read.QueryContext(ctx, `
SELECT id, repository, pull_request_number, head_sha, findings_json, body, verdict, status,
       delivery_execution_id, external_review_id, failure, created_at, updated_at
FROM github_pr_reviews WHERE delivery_execution_id = ? ORDER BY created_at, id`, lifecycle.Execution.ID)
	if err != nil {
		return nil, fmt.Errorf("deliveryprojection: list github reviews: %w", err)
	}
	defer rows.Close()
	var out []delivery.GitHubPRReview
	for rows.Next() {
		var r delivery.GitHubPRReview
		var findingsJSON, createdAt, updatedAt string
		if err := rows.Scan(&r.ID, &r.Repository, &r.PullRequestNumber, &r.HeadSHA, &findingsJSON, &r.Body, &r.Verdict, &r.Status,
			&r.DeliveryExecutionID, &r.ExternalReviewID, &r.Failure, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("deliveryprojection: scan github review: %w", err)
		}
		if err := json.Unmarshal([]byte(findingsJSON), &r.Findings); err != nil {
			return nil, fmt.Errorf("deliveryprojection: decode github review findings: %w", err)
		}
		created, err := parseTimestamp(createdAt)
		if err != nil {
			return nil, err
		}
		updated, err := parseTimestamp(updatedAt)
		if err != nil {
			return nil, err
		}
		r.CreatedAt, r.UpdatedAt = created, updated
		out = append(out, r)
	}
	return out, rows.Err()
}

func githubPRFromLanes(lanes []delivery.LaneSummary) (repository string, number int, headSHA string, ok bool) {
	for _, l := range lanes {
		if l.PRURL == "" && l.Repository == "" {
			continue
		}
		return l.Repository, l.PRNumber, l.BaseSha, true
	}
	return "", 0, "", false
}

func transitionStatuses(w ProviderWrite) (from, to string) {
	var payload struct {
		FromStatus string `json:"from_status"`
		ToStatus   string `json:"target_status"`
	}
	if err := json.Unmarshal([]byte(w.payloadJSON), &payload); err != nil {
		return "", ""
	}
	return payload.FromStatus, payload.ToStatus
}

func writeHealth(writes []ProviderWrite, adapter string) WriteHealth {
	var h WriteHealth
	for _, w := range writes {
		if w.Adapter != adapter {
			continue
		}
		switch w.Status {
		case "pending", "claimed":
			h.Pending++
		case "retrying":
			h.Retrying++
		case "reconciling":
			h.Reconciling++
		case "failed":
			h.Failed++
		case "succeeded":
			h.Succeeded++
		case "cancelled":
			h.Cancelled++
		}
	}
	return h
}

// storedTimeLayout mirrors internal/delivery's own timeLayout - both
// packages persist timestamps the same way (time.RFC3339Nano) into the
// one shared storage kernel.
const storedTimeLayout = time.RFC3339Nano

func parseTimestamp(v string) (time.Time, error) {
	t, err := time.Parse(storedTimeLayout, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("deliveryprojection: parse timestamp %q: %w", v, err)
	}
	return t, nil
}
