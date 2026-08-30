//go:build e2e

// Package e2e proves the delivery reliability rebuild's complete workflows
// end to end: a Jira-sourced delivery through hydration, planning, work,
// and provider synchronization; an ad-hoc delivery's isolation; crash/retry
// recovery; and the panel's live projection. Every test in this package
// runs against a temporary SQLite database and in-process fake HTTP
// servers standing in for Atlassian and GitHub - nothing here spawns a
// real adapter subprocess, writes to a shared data directory, or touches
// this machine's real home directory or git configuration.
package e2e

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryprojection"
	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/jiraintegration"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
)

// stack bundles every store a delivery-workflow test needs, all sharing
// one temporary SQLite kernel that is closed automatically at test end.
type stack struct {
	db         *storage.DB
	deliveries *delivery.Store
	plans      *plan.Store
	outbox     *outbox.Store
	telemetry  *telemetry.Store
	projector  *deliveryprojection.Projector
	registry   *fakeRegistry
}

// newStack opens a throwaway storage kernel under t.TempDir() and wires
// every domain store over it, plus a fakeRegistry that resolves the
// "atlassian" and "github" adapter ids to Gates backed by httptest
// servers rather than real subprocesses.
func newStack(t *testing.T) *stack {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	deliveries := delivery.NewStore(db)
	reg := newFakeRegistry(t)
	return &stack{
		db:         db,
		deliveries: deliveries,
		plans:      plan.NewStore(db),
		outbox:     outbox.New(db),
		telemetry:  telemetry.NewStore(db),
		projector:  deliveryprojection.NewProjector(deliveries),
		registry:   reg,
	}
}

// deliveryService builds a deliveryservice.Service over s, hydrating a
// Jira source through the real internal/jirahooks.Lifecycle (which itself
// talks to s.registry's fake atlassian Gate over real HTTP) and recording
// additive telemetry through s.telemetry.
func (s *stack) deliveryService() *deliveryservice.Service {
	hydrator := jirahooks.NewLifecycle(s.deliveries, s.registry, s.outbox)
	return deliveryservice.New(s.deliveries, s.plans, deliveryservice.WithJiraHydrator(hydrator), deliveryservice.WithTelemetryStore(s.telemetry))
}

// jiraService builds a jiraintegration.Service over s with cfg, the same
// composition internal/jirahooks.JiraHook would build per delivery-lifecycle
// event - called directly here instead of through the event dispatcher, so
// the test drives exactly the effects the plan's scenario names without
// also having to fake deliveryhooks' own dispatch wiring.
func (s *stack) jiraService(cfg *jiraworkflow.Config) *jiraintegration.Service {
	return jiraintegration.NewService(s.deliveries, s.registry, s.outbox, cfg)
}

// githubService builds a githubintegration.Service over s.
func (s *stack) githubService() *githubintegration.Service {
	return githubintegration.NewService(s.registry, s.outbox)
}

// drainOutbox resolves every currently pending/retrying/reconciling intent
// by repeatedly calling a synchronous Worker's RunOnce, up to maxAttempts
// times - enough for this package's scripted scenarios (a handful of
// intents, at most one ambiguous attempt each) to reach a terminal or
// reconciling-then-resolved state without an unbounded loop hanging a
// failing test.
func (s *stack) drainOutbox(t *testing.T, maxAttempts int) {
	t.Helper()
	w := &providerwrite.Worker{ID: "e2e-worker", Store: s.outbox, Adapters: s.registry}
	ctx := context.Background()
	idle := 0
	for i := 0; i < maxAttempts; i++ {
		did, err := w.RunOnce(ctx)
		if err != nil {
			t.Fatalf("drainOutbox: RunOnce: %v", err)
		}
		if !did {
			idle++
			if idle >= 2 {
				return
			}
			continue
		}
		idle = 0
	}
}

// newTempGitRepo creates a real, empty git repository under t.TempDir()
// with repo-local identity only (never --global, and HOME is redirected
// to a throwaway directory first) so a delivery's project can name a real
// RepositoryURL without this suite ever touching the operator's actual
// git configuration.
func newTempGitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "e2e-test")
	runGit(t, dir, "config", "user.email", "e2e-test@example.invalid")
	return dir
}

// runGit runs a git subcommand inside dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// defaultJiraWorkflowConfig enables every lifecycle effect the plan's Jira
// scenario exercises: a start/complete comment, a start-of-work and
// completion transition for Jira project ABC, and worklog sync.
func defaultJiraWorkflowConfig() *jiraworkflow.Config {
	return &jiraworkflow.Config{
		AutoLog:              true,
		CommentEvents:        []string{"delivery.started", "delivery.completed"},
		TransitionOnComplete: true,
		LogWork:              true,
		Transitions: map[string]jiraworkflow.TransitionPolicy{
			"ABC": {StartStatus: "In Progress", CompleteStatus: "Done"},
		},
	}
}
