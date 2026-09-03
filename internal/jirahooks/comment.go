package jirahooks

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
)

// buildComment renders a compact, useful Jira comment body for event,
// mirroring the plan's own completion-comment format (title, plan
// id/revision, projects, PR links, delivery id) but reused for every
// configured comment event rather than only delivery completion.
func buildComment(event deliveryhooks.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Punakawan %s", eventLabel(event.Type))
	if event.Title != "" {
		fmt.Fprintf(&b, "\n\n%q", event.Title)
	}

	var lines []string
	if event.PlanID != "" {
		lines = append(lines, fmt.Sprintf("Plan: %s r%d", event.PlanID, event.PlanRevision))
	}
	if event.EntityID != "" {
		lines = append(lines, "Lane: "+event.EntityID)
	}
	if len(event.Projects) > 0 {
		lines = append(lines, "Projects: "+strings.Join(event.Projects, ", "))
	}
	if len(event.PullRequests) > 0 {
		lines = append(lines, "PRs: "+strings.Join(event.PullRequests, ", "))
	}
	if len(lines) > 0 {
		b.WriteString("\n\n" + strings.Join(lines, "\n"))
	}

	if event.Summary != "" {
		fmt.Fprintf(&b, "\n\n%s", event.Summary)
	}

	fmt.Fprintf(&b, "\n\nDelivery: %s", event.DeliveryID)
	return b.String()
}

// eventLabel renders eventType as the short human-readable phrase
// buildComment's opening line uses.
func eventLabel(eventType deliveryhooks.EventType) string {
	switch eventType {
	case deliveryhooks.EventDeliveryStarted:
		return "delivery started"
	case deliveryhooks.EventPlanCreated:
		return "plan created"
	case deliveryhooks.EventPlanRevised:
		return "plan revised"
	case deliveryhooks.EventImplementationStarted:
		return "implementation started"
	case deliveryhooks.EventImplementationCompleted:
		return "implementation completed"
	case deliveryhooks.EventReviewChangesRequired:
		return "review requested changes"
	case deliveryhooks.EventReviewAccepted:
		return "review accepted"
	case deliveryhooks.EventDeliveryCompleted:
		return "delivery completed"
	case deliveryhooks.EventDeliveryFailed:
		return "delivery failed"
	case deliveryhooks.EventRequirementUnclear:
		return "needs clarification before work starts"
	default:
		return string(eventType)
	}
}
