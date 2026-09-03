package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newRemoteAndCheckout builds a real bare repository and a clone of it
// with one commit pushed, which is the least a lane worktree needs: a
// base branch that exists on a remote it can fetch.
func newRemoteAndCheckout(t *testing.T) (remote, checkout string) {
	t.Helper()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	remote = t.TempDir()
	run(remote, "init", "--bare", "-b", "main")
	checkout = t.TempDir()
	run("", "clone", remote, checkout)
	run(checkout, "config", "user.email", "test@example.com")
	run(checkout, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run(checkout, "add", "README.md")
	run(checkout, "commit", "-m", "initial commit")
	run(checkout, "push", "-u", "origin", "main")
	return remote, checkout
}

// TestStartDeliveryAsksWhereToWorkThenCutsAWorktreePerLane: a delivery
// used to leave every lane pointing at nothing, so an agent worked in
// whatever directory it already stood in - somebody's own checkout, and
// for a multi-project delivery not even the right repository. The
// worktree machinery to do this properly existed with no caller at all.
//
// Starting asks where the work belongs and creates nothing; answering
// cuts one worktree per lane; and the answer is remembered, so the next
// delivery in that project is never asked again.
func TestStartDeliveryAsksWhereToWorkThenCutsAWorktreePerLane(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	remote, checkout := newRemoteAndCheckout(t)

	project := map[string]any{
		"slug":           "worktree-project",
		"repository_url": remote,
		"default_branch": "main",
		"local_path":     checkout,
		"tasks":          []map[string]any{{"title": "do the work"}},
	}

	var started StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"plan":     testPlan(),
		"source":   jiraSource("PAY-7001"),
		"projects": []map[string]any{project},
	}, &started)

	if started.NeedsInput == nil || started.NeedsInput.Kind != protocol.NeedUserInputKindDecisionRequired {
		t.Fatalf("NeedsInput = %+v, want a decision about where the work happens", started.NeedsInput)
	}
	if len(started.Reconciliation.Worktrees) != 0 {
		t.Fatalf("Worktrees = %v, want none before anybody answered", started.Reconciliation.Worktrees)
	}

	var view DeliveryViewOutput
	callTool(t, cs, "get_delivery", map[string]any{"orchestration_id": started.OrchestrationId}, &view)
	var reference string
	for _, question := range view.View.PendingQuestions {
		if delivery.IsWorktreeQuestion(question) {
			reference = question
		}
	}
	if reference == "" {
		t.Fatalf("PendingQuestions = %v, want the worktree question", view.View.PendingQuestions)
	}

	var answered DeliveryViewOutput
	callTool(t, cs, "answer_delivery_question", map[string]any{
		"orchestration_id": started.OrchestrationId,
		"reference":        reference,
		"worktree_mode":    "worktree",
	}, &answered)
	if len(answered.Worktrees) != 1 {
		t.Fatalf("Worktrees = %v, want exactly one per lane", answered.Worktrees)
	}
	if info, err := os.Stat(answered.Worktrees[0]); err != nil || !info.IsDir() {
		t.Fatalf("expected a real worktree directory at %s: %v", answered.Worktrees[0], err)
	}
	if strings.HasPrefix(answered.Worktrees[0], checkout) {
		t.Fatalf("worktree %s was cut inside the checkout itself", answered.Worktrees[0])
	}
	for _, question := range answered.View.PendingQuestions {
		if delivery.IsWorktreeQuestion(question) {
			t.Fatalf("PendingQuestions = %v, want the answered question cleared", answered.View.PendingQuestions)
		}
	}

	// The same project again: answered once, never asked twice.
	var second StartDeliveryOutput
	callTool(t, cs, "start_delivery", map[string]any{
		"plan":     testPlan(),
		"source":   jiraSource("PAY-7002"),
		"projects": []map[string]any{project},
	}, &second)
	if second.NeedsInput != nil {
		t.Fatalf("NeedsInput = %+v, want none - this project already said where its work happens", second.NeedsInput)
	}
	if len(second.Reconciliation.Worktrees) != 1 {
		t.Fatalf("Worktrees = %v, want one cut without asking", second.Reconciliation.Worktrees)
	}
}
