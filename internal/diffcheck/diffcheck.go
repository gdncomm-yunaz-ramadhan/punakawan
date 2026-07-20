// Package diffcheck validates a task worktree's pending changes before they
// are committed, per punakawan-go-typescript-detailed-plan.md §15.4 ("Block
// commits containing detected secrets") and §17.2 (the evidence bundle's
// diff.patch file).
package diffcheck

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
)

// secretPatterns is a deliberately small, documented heuristic set, not a
// full secret-scanning product: §15.4 only requires that commits containing
// "detected" secrets are blocked, and building or vendoring a complete
// scanner is out of scope for this milestone.
var secretPatterns = []*regexp.Regexp{
	// AWS access key id.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// PEM private key header.
	regexp.MustCompile(`(?i)-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)(secret|api)[_-]?key\s*[:=]\s*['"][^'"\s]{8,}['"]`),
	regexp.MustCompile(`(?i)password\s*[:=]\s*['"][^'"\s]{4,}['"]`),
}

// Report is the outcome of checking a worktree's pending changes.
type Report struct {
	Allowed      bool
	ChangedFiles []string
	Violations   []string
	Diff         string
}

// Check stages every pending change in worktreePath (`git add -A`, so newly
// created and deleted files are included, not just modified tracked files),
// then diffs the index against HEAD, checking each changed file against pol
// and scanning the diff text for likely secrets. It writes the diff into
// bundle's diff.patch (§17.2) regardless of outcome, since a rejected diff is
// still evidence.
//
// Staging is a mutation of the worktree's index, not its working tree
// content; internal/gitops.Inspector is deliberately read-only (see its
// package doc), so that mutation happens here rather than being added to
// Inspector.
func Check(ctx context.Context, sup *tools.Supervisor, worktreePath string, pol *policy.Policy, bundle *evidence.Bundle) (Report, error) {
	res, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"add", "-A"}, Dir: worktreePath})
	if err != nil {
		return Report{}, fmt.Errorf("diffcheck: git add -A: %w", err)
	}
	if res.ExitCode != 0 {
		return Report{}, fmt.Errorf("diffcheck: git add -A failed: %s", res.Stderr)
	}

	inspector := gitops.NewInspector(sup)

	status, err := inspector.Status(ctx, worktreePath)
	if err != nil {
		return Report{}, fmt.Errorf("diffcheck: git status: %w", err)
	}

	diffText, err := inspector.Diff(ctx, worktreePath, "--cached")
	if err != nil {
		return Report{}, fmt.Errorf("diffcheck: git diff --cached: %w", err)
	}

	report := Report{
		Allowed:      true,
		ChangedFiles: status.ChangedFiles,
		Diff:         diffText,
	}

	for _, path := range status.ChangedFiles {
		allowed, err := pol.AllowsFilesystemWrite(path)
		if err != nil {
			return Report{}, fmt.Errorf("diffcheck: evaluate policy for %q: %w", path, err)
		}
		if !allowed {
			report.Allowed = false
			report.Violations = append(report.Violations, fmt.Sprintf("policy denies write to %q", path))
		}
	}

	for _, pattern := range secretPatterns {
		if pattern.MatchString(diffText) {
			report.Allowed = false
			report.Violations = append(report.Violations, fmt.Sprintf("diff matches secret-like pattern %q", pattern.String()))
		}
	}

	if bundle != nil {
		if err := writeDiffPatch(bundle, diffText); err != nil {
			return Report{}, err
		}
	}

	return report, nil
}

func writeDiffPatch(bundle *evidence.Bundle, diffText string) error {
	if err := os.WriteFile(bundle.Path("diff.patch"), []byte(diffText), 0o644); err != nil {
		return fmt.Errorf("diffcheck: write diff.patch: %w", err)
	}
	return nil
}
