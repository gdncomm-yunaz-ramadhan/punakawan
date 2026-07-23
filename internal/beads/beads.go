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

// ReopenIssue runs `bd reopen <issueID> --reason <reason>` in dir, per
// Bagong's reopen-task workflow (M8): a blocking finding against
// already-completed work reopens the original issue rather than creating a
// duplicate. Unlike CreateTask/AddDependency, this does not decode a
// specific JSON field from bd's output - bd reopen's --json output carries
// nothing this caller needs beyond success/failure, so checking ExitCode is
// sufficient.
func ReopenIssue(ctx context.Context, sup *tools.Supervisor, dir, issueID, reason string) error {
	if issueID == "" {
		return fmt.Errorf("beads: reopen issue: issueID is required")
	}

	args := []string{"reopen", issueID, "--json"}
	if reason != "" {
		args = append(args, "--reason", reason)
	}

	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: args, Dir: dir, Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("beads: bd reopen: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("beads: bd reopen failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// Available reports whether the bd binary is installed and responsive,
// mirroring tools.Supervisor.RTKAvailable's pattern for rtk.
func Available(ctx context.Context, sup *tools.Supervisor, dir string) bool {
	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: []string{"--version"}, Dir: dir, Timeout: 5 * time.Second})
	return err == nil && res.ExitCode == 0
}

// ReadyIssue is the subset of `bd ready --json`'s (and `bd ready --claim
// --json`'s) per-issue output this package needs. Verified empirically (bd
// version 1.0.4): both invocations emit the same JSON object shape, one
// difference being that a successful --claim additionally populates
// Assignee and StartedAt and sets Status to "in_progress" (plain `bd ready`
// only lists issues that are still "open"). bd omits empty fields (e.g.
// Description, Labels, Dependencies, Parent) rather than emitting zero
// values, which is why every field below is tagged omitempty-tolerant via
// pointer-free zero values decoding cleanly from an absent key.
//
// bd emits additional fields this package does not need (schema_version,
// comment_count, dependency_count, dependent_count, ...); the JSON decoder
// simply ignores them.
type ReadyIssue struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Status       string            `json:"status"`
	Priority     int               `json:"priority"`
	IssueType    string            `json:"issue_type"`
	Owner        string            `json:"owner"`
	Assignee     string            `json:"assignee"`
	Labels       []string          `json:"labels,omitempty"`
	Parent       string            `json:"parent"`
	Dependencies []ReadyDependency `json:"dependencies,omitempty"`
	CreatedAt    string            `json:"created_at"`
	CreatedBy    string            `json:"created_by"`
	UpdatedAt    string            `json:"updated_at"`
	StartedAt    string            `json:"started_at"`
}

// RelatedIssue is one entry of Issue.Dependents (bd show --json's
// "dependents" and "dependencies" arrays, when they nest a full issue
// summary rather than a bare edge - verified empirically against bd
// version 1.0.4: bd list/ready --json emit dependencies as flat
// {issue_id,depends_on_id,type} edges (ReadyDependency), but bd show --json
// nests the related issue's own summary fields plus dependency_type).
type RelatedIssue struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	Priority       int    `json:"priority"`
	IssueType      string `json:"issue_type"`
	DependencyType string `json:"dependency_type"`
}

// Issue is the subset of `bd show --json`'s and `bd list --json`'s
// per-issue output this package needs beyond ReadyIssue: the full (not
// ready-filtered) issue detail, including Parent, Dependents, and
// AcceptanceCriteria, which bd ready --json does not emit.
type Issue struct {
	ID                 string         `json:"id"`
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	AcceptanceCriteria string         `json:"acceptance_criteria"`
	Status             string         `json:"status"`
	Priority           int            `json:"priority"`
	IssueType          string         `json:"issue_type"`
	Owner              string         `json:"owner"`
	Assignee           string         `json:"assignee"`
	Labels             []string       `json:"labels,omitempty"`
	Parent             string         `json:"parent"`
	Dependencies       []RelatedIssue `json:"dependencies,omitempty"`
	Dependents         []RelatedIssue `json:"dependents,omitempty"`
	CreatedAt          string         `json:"created_at"`
	CreatedBy          string         `json:"created_by"`
	UpdatedAt          string         `json:"updated_at"`
	ClosedAt           string         `json:"closed_at"`
}

// ReadyDependency is the subset of a ReadyIssue's "dependencies" entries
// this package needs, matching depResult's field naming for consistency.
type ReadyDependency struct {
	IssueId     string `json:"issue_id"`
	DependsOnId string `json:"depends_on_id"`
	Type        string `json:"type"`
}

// ReadyOptions holds the optional `bd ready` filters this package exposes.
// Fields left empty/zero are simply omitted from the argv, deferring to
// bd's own defaults (notably --limit's default of 100; this package does
// not override it, so callers wanting more than 100 results must page via
// repeated calls, and callers wanting bd's "unlimited" behavior have no way
// to request --limit 0 through this struct today).
//
// Flags intentionally not exposed: --claim (mutating; see ClaimReady
// below), --mol/--mol-type/--gated (molecule-specific dispatch, out of
// scope for a thin ready-task-selection wrapper), --explain (debug/human
// output, not additional structured data), --plain/--pretty (display-only,
// irrelevant with --json), --unassigned (redundant with Assignee=="" filtering
// client-side if ever needed), --label/--label-any/--priority/--type/--sort/
// --parent/--has-metadata-key/--metadata-field/--include-deferred/
// --include-ephemeral (not called out by the task's "at minimum" list and
// not needed by the plan's current callers; can be added later if a real
// caller needs them).
type ReadyOptions struct {
	// Assignee filters by bd's -a/--assignee.
	Assignee string
	// ExcludeLabels excludes issues with ANY of these labels, via bd's
	// --exclude-label (repeatable on the CLI).
	ExcludeLabels []string
	// ExcludeTypes excludes these issue types, via bd's --exclude-type
	// (repeatable on the CLI).
	ExcludeTypes []string
}

// readyArgs builds the shared argv tail for `bd ready` and `bd ready
// --claim`, given opts.
func readyArgs(opts ReadyOptions) []string {
	var args []string
	if opts.Assignee != "" {
		args = append(args, "--assignee", opts.Assignee)
	}
	for _, label := range opts.ExcludeLabels {
		args = append(args, "--exclude-label", label)
	}
	for _, issueType := range opts.ExcludeTypes {
		args = append(args, "--exclude-type", issueType)
	}
	return args
}

// decodeReadyIssues runs argv (a `bd ready` invocation already carrying
// --json) in dir and decodes its stdout as a []ReadyIssue. An empty result
// set is not an error: bd exits 0 and prints "[]" when no issues match, per
// empirical verification, so callers get a nil/empty slice rather than an
// error in that case.
func decodeReadyIssues(ctx context.Context, sup *tools.Supervisor, dir string, argv []string) ([]ReadyIssue, error) {
	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: argv, Dir: dir, Timeout: 30 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("beads: bd ready: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("beads: bd ready failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	var out []ReadyIssue
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("beads: decode bd ready output: %w", err)
	}
	return out, nil
}

// Ready runs `bd ready --json` in dir, filtered per opts, and returns the
// issues bd considers claimable right now (open, with no active blockers;
// bd's GetReadyWork semantics exclude in_progress, blocked, deferred, and
// hooked issues on its own).
//
// dir must be a directory Supervisor sup is permitted to run commands in
// (see tools.Supervisor.AllowedRoots) and must contain (or be within) an
// initialized bd project (`bd init`).
//
// This corresponds to punakawan-go-typescript-detailed-plan.md §9's flow
// diagram step "Petruk executes ready task": Ready is how a caller
// discovers which task to execute next, without mutating any issue state
// (see ClaimReady for the mutating variant, §11.3's "claim task" step).
func Ready(ctx context.Context, sup *tools.Supervisor, dir string, opts ReadyOptions) ([]ReadyIssue, error) {
	args := append([]string{"ready", "--json"}, readyArgs(opts)...)
	return decodeReadyIssues(ctx, sup, dir, args)
}

// ClaimReady runs `bd ready --claim --json` in dir, filtered per opts,
// atomically claiming (per bd ready --help) the first ready issue matching
// the filters: bd sets its status to in_progress and its assignee, and
// returns that single issue's data in the same shape as Ready, as a
// one-element slice. If no issue matches, it returns an empty slice and no
// error, exactly like Ready (verified empirically: bd exits 0 and prints
// "[]" in that case too, it does not treat "nothing to claim" as a
// failure).
//
// This is split out from Ready, rather than a Claim bool field on
// ReadyOptions, because it is a mutating action (it changes issue state in
// bd's store) where Ready is read-only; keeping them as separate functions
// makes that distinction visible at call sites, mirroring this package's
// existing CreateTask/AddDependency split between issue-graph construction
// and (here) issue-graph traversal vs. claiming. This corresponds to
// punakawan-go-typescript-detailed-plan.md §11.3's execution loop step 1,
// "claim task".
func ClaimReady(ctx context.Context, sup *tools.Supervisor, dir string, opts ReadyOptions) ([]ReadyIssue, error) {
	args := append([]string{"ready", "--claim", "--json"}, readyArgs(opts)...)
	return decodeReadyIssues(ctx, sup, dir, args)
}

// ListOptions holds the optional `bd list` filters this package exposes.
// Unlike ReadyOptions, List is not restricted to unblocked issues: it is
// the panel's task reader's route to "every task regardless of state"
// (punakawan-panel-implementation-plan.md §8.2).
type ListOptions struct {
	Status   string
	Priority string
	Type     string
	Assignee string
	// Limit overrides bd's own default of 50. 0 defers to that default,
	// matching ReadyOptions' convention; List does not expose bd's
	// --limit=0 ("unlimited") passthrough today.
	Limit int
}

// List runs `bd list --json` in dir, filtered per opts, and returns every
// matching issue regardless of ready/blocked/closed state. dir has the
// same requirements as Ready's.
func List(ctx context.Context, sup *tools.Supervisor, dir string, opts ListOptions) ([]ReadyIssue, error) {
	args := []string{"list", "--json"}
	if opts.Status != "" {
		args = append(args, "--status", opts.Status)
	}
	if opts.Priority != "" {
		args = append(args, "--priority", opts.Priority)
	}
	if opts.Type != "" {
		args = append(args, "--type", opts.Type)
	}
	if opts.Assignee != "" {
		args = append(args, "--assignee", opts.Assignee)
	}
	if opts.Limit > 0 {
		args = append(args, "--limit", fmt.Sprintf("%d", opts.Limit))
	}
	return decodeReadyIssues(ctx, sup, dir, args)
}

// Show runs `bd show --json` in dir for a single issue ID and returns its
// full detail, including Dependents and AcceptanceCriteria, which List and
// Ready do not emit.
func Show(ctx context.Context, sup *tools.Supervisor, dir, id string) (Issue, error) {
	res, err := sup.Run(ctx, tools.Spec{Name: "bd", Args: []string{"show", id, "--json"}, Dir: dir, Timeout: 30 * time.Second})
	if err != nil {
		return Issue{}, fmt.Errorf("beads: bd show %s: %w", id, err)
	}
	if res.ExitCode != 0 {
		return Issue{}, fmt.Errorf("beads: bd show %s failed: %s", id, strings.TrimSpace(string(res.Stderr)))
	}

	var out []Issue
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return Issue{}, fmt.Errorf("beads: decode bd show %s output: %w", id, err)
	}
	if len(out) == 0 {
		return Issue{}, fmt.Errorf("beads: bd show %s: issue not found", id)
	}
	return out[0], nil
}
