// hooks.go holds the shared plumbing every Store call site uses to
// translate its own state transition into internal/deliveryhooks' coarser
// event vocabulary and dispatch it. See workflowdefinition.go's WithHooks
// for how a caller configures hooks, and deliveryhooks.Dispatcher.Dispatch
// for why a Store with none configured pays nothing for any of this.
package delivery

import (
	"context"
	"log/slog"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
)

// dispatchOrchestrationEvent loads orchestrationID's current derived state
// and hands it, translated into a deliveryhooks.Event, to whatever hooks
// are configured. Every lane-scoped call site (a lease granted or
// completed, a review conclusion recorded) uses this rather than building
// its own Event, because a hook resolves its external target (e.g. a
// linked Jira issue) per delivery, not per lane - the individual lane that
// triggered the transition has no separate identity a hook downstream of
// this call could use. Loading the orchestration is a best-effort step
// here: a failure is logged and swallowed rather than propagated, since by
// the time this runs the actual delivery state change it rides along with
// has already committed and must not be affected by a hook-plumbing
// problem.
func (s *Store) dispatchOrchestrationEvent(ctx context.Context, orchestrationID string, eventType deliveryhooks.EventType, summary string, pullRequests []string) {
	if s.hooks == nil {
		return
	}
	orch, err := s.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		slog.Warn("delivery: dispatch hook event: load orchestration",
			"orchestration_id", orchestrationID, "event_type", eventType, "error", err)
		return
	}
	s.hooks.Dispatch(ctx, deliveryhooks.Event{
		Type: eventType, DeliveryID: orchestrationID, Revision: orch.Revision,
		Title: derefOrEmpty(orch.Title), Projects: orch.ProjectIds,
		PlanID: derefOrEmpty(orch.PlanId), PlanRevision: derefOrZero(orch.PlanRevision),
		PullRequests: pullRequests, Summary: summary,
	})
}

// pullRequestURLs collects every lane's published pull request URL across
// orchestrationID, in lane iteration order, for review/completion hook
// events where a linked Jira issue's comment is more useful with the
// delivery's PR links than without them. Lanes with no pull request
// published yet are simply skipped, not reported as a gap - a delivery in
// progress legitimately has fewer PRs than lanes.
func (s *Store) pullRequestURLs(ctx context.Context, orchestrationID string) []string {
	lanes, err := s.ListLanes(ctx, orchestrationID)
	if err != nil {
		slog.Warn("delivery: collect pull request urls for hook event: list lanes",
			"orchestration_id", orchestrationID, "error", err)
		return nil
	}
	var urls []string
	for _, lane := range lanes {
		if lane.PrUrl != nil && *lane.PrUrl != "" {
			urls = append(urls, *lane.PrUrl)
		}
	}
	return urls
}

// derefOrEmpty returns "" for a nil string pointer and *s otherwise, for
// rendering an orchestration's optional Title/Description/PlanId fields
// into a deliveryhooks.Event, which carries plain strings rather than
// pointers since a hook has no use for the nil/empty distinction those
// fields preserve for replay purposes.
func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefOrZero returns 0 for a nil *int and *i otherwise, mirroring
// derefOrEmpty for the one optional int field (PlanRevision) a
// deliveryhooks.Event carries.
func derefOrZero(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
