package mcpserver

import "testing"

// TestPlanSaveAndGet drives plan_save/plan_get over the wire: saving a
// plan twice under the same id appends a revision rather than replacing
// it, and plan_get can fetch either the current revision or an exact
// past one.
func TestPlanSaveAndGet(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	id := "plan-smoke-1"
	var saved PlanSaveOutput
	callTool(t, cs, "plan_save", map[string]any{
		"plan": map[string]any{
			"id":                  id,
			"objective":           "migrate checkout to payments v2",
			"acceptance_criteria": []string{"checkout tests pass"},
		},
	}, &saved)
	if saved.Plan.Revision != 1 {
		t.Fatalf("saved.Plan.Revision = %d, want 1", saved.Plan.Revision)
	}
	if saved.Plan.PreviousRevision != nil {
		t.Fatalf("saved.Plan.PreviousRevision = %v, want nil for a first revision", saved.Plan.PreviousRevision)
	}

	var revised PlanSaveOutput
	callTool(t, cs, "plan_save", map[string]any{
		"plan": map[string]any{
			"id":                id,
			"objective":         "migrate checkout to payments v2, clarified",
			"reason_for_change": "clarified target repo",
		},
	}, &revised)
	if revised.Plan.Revision != 2 {
		t.Fatalf("revised.Plan.Revision = %d, want 2", revised.Plan.Revision)
	}
	if revised.Plan.PreviousRevision == nil || *revised.Plan.PreviousRevision != 1 {
		t.Fatalf("revised.Plan.PreviousRevision = %v, want pointer to 1", revised.Plan.PreviousRevision)
	}

	var current PlanGetOutput
	callTool(t, cs, "plan_get", map[string]any{"id": id}, &current)
	if current.Plan.Revision != 2 || current.Plan.Objective != "migrate checkout to payments v2, clarified" {
		t.Fatalf("plan_get (current) = %+v, want revision 2", current.Plan)
	}

	var first PlanGetOutput
	callTool(t, cs, "plan_get", map[string]any{"id": id, "revision": 1}, &first)
	if first.Plan.Revision != 1 || first.Plan.Objective != "migrate checkout to payments v2" {
		t.Fatalf("plan_get (revision 1) = %+v, want the original, unmutated revision", first.Plan)
	}
}

// TestPlanGetUnknownIsRefused covers looking up a plan id nobody ever
// saved.
func TestPlanGetUnknownIsRefused(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	callToolExpectingError(t, cs, "plan_get", map[string]any{"id": "no-such-plan"})
}
