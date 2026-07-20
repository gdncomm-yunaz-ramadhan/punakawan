// Package beads wraps the subset of the `bd` (Beads) CLI needed to create
// tasks and dependencies, per punakawan-go-typescript-detailed-plan.md §10
// ("Beads Task Generation"): Jira remains the human-facing tracker, and
// Beads becomes the detailed local execution graph.
//
// This is deliberately thin, mirroring internal/tools/rtk.go: it shells out
// to `bd` via a *tools.Supervisor rather than reimplementing any of bd's own
// logic (dependency-cycle checking, ID assignment, Dolt persistence, ...).
//
// Exact invocation shapes below (flag names, JSON output fields) were
// verified empirically against `bd --help`, `bd create --help`, and
// `bd dep add --help` (bd version 1.0.4) rather than assumed, per the task
// instructions. The plan's §10 does not prescribe a CLI invocation shape
// for bd itself, so the specific flag choices below (e.g. --json over
// --silent, --acceptance receiving newline-joined criteria) are this
// package's judgment call, called out in the doc comments below.
package beads

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// createResult is the subset of `bd create --json`'s output this package
// needs. bd emits additional fields (schema_version, owner, timestamps,
// ...) that are not needed here and are left for the JSON decoder to
// ignore.
type createResult struct {
	Id string `json:"id"`
}

// depResult is the subset of `bd dep add --json`'s output this package
// needs.
type depResult struct {
	IssueId     string `json:"issue_id"`
	DependsOnId string `json:"depends_on_id"`
	Status      string `json:"status"`
	Type        string `json:"type"`
}

// CreateTaskOptions holds the optional `bd create` fields this package
// exposes. Fields left empty are simply omitted from the argv.
type CreateTaskOptions struct {
	// Type is the bd issue type (task|epic|bug|feature|chore|decision).
	// Empty defers to bd's own default ("task").
	Type string
	// Parent is a bd issue ID to create this issue as a hierarchical child
	// of (bd's --parent), e.g. an epic. Empty creates a top-level issue.
	Parent string
	// Labels are applied via bd's --labels (comma-separated on the CLI).
	Labels []string
	// AcceptanceCriteria lines are newline-joined and passed as bd's single
	// --acceptance string flag: bd has no notion of a criteria list, so
	// joining with "\n" is this package's judgment call to preserve one
	// criterion per line in bd's stored text.
	AcceptanceCriteria []string
	// ExternalRef is passed as bd's --external-ref (e.g. "jira-PAY-1842"),
	// bd's own field for linking an issue to an external tracker item.
	ExternalRef string
}

// CreateTask runs `bd create <title> --description <description> ...` in
// dir and returns the created issue's bd ID.
//
// dir must be a directory Supervisor sup is permitted to run commands in
// (see tools.Supervisor.AllowedRoots) and must contain (or be within) an
// initialized bd project (`bd init`).
func CreateTask(ctx context.Context, sup *tools.Supervisor, dir, title, description string, opts CreateTaskOptions) (string, error) {
	if title == "" {
		return "", fmt.Errorf("beads: create task: title is required")
	}

	args := []string{"create", title, "--json"}
	if description != "" {
		args = append(args, "--description", description)
	}
	if opts.Type != "" {
		args = append(args, "--type", opts.Type)
	}
	if opts.Parent != "" {
		args = append(args, "--parent", opts.Parent)
	}
	if len(opts.Labels) > 0 {
		args = append(args, "--labels", strings.Join(opts.Labels, ","))
	}
	if len(opts.AcceptanceCriteria) > 0 {
		args = append(args, "--acceptance", strings.Join(opts.AcceptanceCriteria, "\n"))
	}
	if opts.ExternalRef != "" {
		args = append(args, "--external-ref", opts.ExternalRef)
	}

	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: args, Dir: dir, Timeout: 30 * time.Second})
	if err != nil {
		return "", fmt.Errorf("beads: bd create: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("beads: bd create failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	var out createResult
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return "", fmt.Errorf("beads: decode bd create output: %w", err)
	}
	if out.Id == "" {
		return "", fmt.Errorf("beads: bd create returned no issue id (stdout: %s)", strings.TrimSpace(string(res.Stdout)))
	}
	return out.Id, nil
}

// AddDependency runs `bd dep add <fromID> <toID> --type <depType>` in dir,
// recording that fromID depends on (per bd's terminology, "is blocked by")
// toID. depType must be one of the types bd dep add --help enumerates
// (blocks|tracks|related|parent-child|discovered-from|until|caused-by|
// validates|relates-to|supersedes); an empty depType defers to bd's own
// default ("blocks").
//
// Verified empirically: bd dep add does not validate that toID refers to an
// existing issue and returns exit 0 / status "added" even for an unknown
// ID. This is intentional on bd's side (per bd dep add --help, unrecognized
// IDs are treated like its external:<project>:<capability> cross-project
// references and resolved lazily), not a gap in this wrapper — callers that
// need existence checking must do it themselves (e.g. via bd show) before
// calling AddDependency.
func AddDependency(ctx context.Context, sup *tools.Supervisor, dir, fromID, toID, depType string) error {
	if fromID == "" || toID == "" {
		return fmt.Errorf("beads: add dependency: fromID and toID are required")
	}

	args := []string{"dep", "add", fromID, toID, "--json"}
	if depType != "" {
		args = append(args, "--type", depType)
	}

	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: args, Dir: dir, Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("beads: bd dep add: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("beads: bd dep add failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	var out depResult
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return fmt.Errorf("beads: decode bd dep add output: %w", err)
	}
	if out.Status != "added" {
		return fmt.Errorf("beads: bd dep add: unexpected status %q (stdout: %s)", out.Status, strings.TrimSpace(string(res.Stdout)))
	}
	return nil
}

// Available reports whether the bd binary is installed and responsive,
// mirroring tools.Supervisor.RTKAvailable's pattern for rtk.
func Available(ctx context.Context, sup *tools.Supervisor, dir string) bool {
	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: []string{"--version"}, Dir: dir, Timeout: 5 * time.Second})
	return err == nil && res.ExitCode == 0
}
