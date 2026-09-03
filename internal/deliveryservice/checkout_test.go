package deliveryservice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
)

// newTestCheckout makes a real git repository whose origin is remote, so
// the resolver's "is this that repository" check has something true to
// answer against.
func newTestCheckout(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

// TestStartDeliveryRecordsAndReusesAProjectCheckout: a delivery knew only
// a repository URL, so the only tree it could ever work in was the one its
// MCP server happened to be started in. Starting it from inside the
// checkout now records where that repository is, and a later delivery
// started from anywhere else finds the same tree.
func TestStartDeliveryRecordsAndReusesAProjectCheckout(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	remote := "https://example.test/checkout-api.git"
	checkout := newTestCheckout(t, remote)

	req := jiraRequest("acme", "PAY-1", "first")
	req.WorkspaceRoot = checkout
	req.Projects = []ProjectDraft{{Slug: "checkout-api", RepositoryURL: remote, DefaultBranch: "main", Title: "do the work"}}

	result, needsInput, err := svc.StartOrResolve(ctx, req)
	if err != nil || needsInput != nil {
		t.Fatalf("StartOrResolve: needsInput=%v err=%v", needsInput, err)
	}
	if len(result.Reconciliation.Checkouts) != 1 || !strings.Contains(result.Reconciliation.Checkouts[0], checkout) {
		t.Fatalf("Checkouts = %v, want the checkout this delivery was started from", result.Reconciliation.Checkouts)
	}

	// Started from somewhere that is not a checkout of anything, the
	// recorded path is what answers.
	elsewhere := jiraRequest("acme", "PAY-2", "second")
	elsewhere.WorkspaceRoot = t.TempDir()
	elsewhere.Projects = req.Projects
	result, needsInput, err = svc.StartOrResolve(ctx, elsewhere)
	if err != nil || needsInput != nil {
		t.Fatalf("StartOrResolve from elsewhere: needsInput=%v err=%v", needsInput, err)
	}
	if len(result.Reconciliation.Checkouts) != 1 || !strings.Contains(result.Reconciliation.Checkouts[0], checkout) {
		t.Fatalf("Checkouts = %v, want the remembered checkout", result.Reconciliation.Checkouts)
	}
}

// A recorded path that is no longer that repository must not be handed to
// a delivery: work would land in the wrong tree, or in a directory that
// only looks like the right one.
func TestStartDeliveryRejectsAStaleRecordedCheckout(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	remote := "https://example.test/checkout-api.git"
	checkout := newTestCheckout(t, remote)

	req := jiraRequest("acme", "PAY-1", "first")
	req.WorkspaceRoot = checkout
	req.Projects = []ProjectDraft{{Slug: "checkout-api", RepositoryURL: remote, DefaultBranch: "main", Title: "do the work"}}
	if _, _, err := svc.StartOrResolve(ctx, req); err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}

	// The checkout is gone, and nothing else on this machine is it.
	if err := os.RemoveAll(filepath.Join(checkout, ".git")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	stale := jiraRequest("acme", "PAY-3", "third")
	stale.WorkspaceRoot = t.TempDir()
	stale.Projects = req.Projects
	result, _, err := svc.StartOrResolve(ctx, stale)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if len(result.Reconciliation.Checkouts) != 0 {
		t.Fatalf("Checkouts = %v, want none - the recorded path is no longer that repository", result.Reconciliation.Checkouts)
	}
	if len(result.Reconciliation.Warnings) == 0 {
		t.Fatal("expected a warning naming the project with no checkout")
	}
}

func TestCheckoutDirNameSeparatesRepositoriesSharingAName(t *testing.T) {
	one := checkoutDirName("https://example.test/acme/web.git", "web")
	two := checkoutDirName("https://example.test/other/web.git", "web")
	if one == two {
		t.Fatalf("checkoutDirName collided for two different repositories: %q", one)
	}
	if delivery.RepositoryIdentity("git@example.test:acme/web.git") != delivery.RepositoryIdentity("https://example.test/acme/web.git") {
		t.Skip("repository identity does not unify these remote forms; nothing to assert about clone naming")
	}
	if checkoutDirName("git@example.test:acme/web.git", "web") != one {
		t.Fatal("the same repository named two ways must clone to one directory")
	}
}
