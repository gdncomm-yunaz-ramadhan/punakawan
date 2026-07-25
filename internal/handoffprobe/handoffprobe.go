// Package handoffprobe supplies concrete, dependency-light implementations of
// the three handoff.ValidationDeps probe funcs that the panel
// (internal/panel/deps_subsystems.go buildValidationDeps) and the MCP tools
// currently pass as nil: RepositoryStateMatches, EvidenceExists, and
// TaskIsCurrent.
//
// It deliberately imports nothing from internal/handoff, internal/panel, or
// internal/mcpserver, and depends only on the standard library (plus the `git`
// binary via os/exec). Each exported func is a constructor that takes the
// project/workspace root and returns a closure matching the corresponding
// handoff.ValidationDeps field signature, so the integrator can drop them
// straight into buildValidationDeps and handoffValidationDeps:
//
//	deps := handoff.ValidationDeps{
//		RepositoryStateMatches: handoffprobe.RepositoryStateMatches(root),
//		EvidenceExists:         handoffprobe.EvidenceExists(root),
//		TaskIsCurrent:          handoffprobe.TaskIsCurrent(root),
//		// ... other deps wired elsewhere
//	}
//
// Every constructor is nil-safe to build (it never touches the filesystem until
// the returned func is called) and every returned func is safe to call with an
// empty slice: RepositoryStateMatches/EvidenceExists short-circuit and
// TaskIsCurrent never inspects the filesystem at all.
package handoffprobe

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepositoryStateMatches returns a probe matching
// handoff.ValidationDeps.RepositoryStateMatches: given the repositories a
// capsule recorded as changed, it reports whether their working-tree state
// still "matches" what the capsule saw.
//
// Because this package cannot know the exact SHA/state the capsule recorded
// (that lookup is intentionally kept out of the handoff package), it applies a
// pragmatic, documented contract per repo, resolving each entry to a path
// (used as-is when absolute, otherwise joined under root):
//
//   - Missing/unresolvable repo -> NOT matching. If the path does not exist (or
//     is not a directory), or the repo is a git work tree whose HEAD cannot be
//     resolved (no branch ref and no commit — i.e. a corrupt or half-created
//     repo), the capsule's view can no longer be trusted, so the whole set is
//     reported as not matching (returns false, nil).
//   - Resolvable git work tree -> matching. A work tree whose HEAD resolves
//     (either a symbolic ref for an unborn branch, e.g. a fresh `git init`, or
//     an actual commit, including detached HEAD) is treated as matching.
//   - Path that is not a git work tree -> skipped, treated as matching. A plain
//     directory that git does not recognize as a work tree cannot be probed for
//     divergence here, so it is conservatively counted as matching rather than
//     failing validation on something we cannot inspect.
//
// A single exec problem never crashes the caller: a non-zero git exit (e.g.
// "not a git repository") is interpreted as data, not a failure; only a genuine
// inability to run git (binary missing, permission denied) returns (false,
// err). Called with an empty slice it returns (true, nil).
func RepositoryStateMatches(root string) func(repos []string) (bool, error) {
	return func(repos []string) (bool, error) {
		for _, repo := range repos {
			path := repo
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, repo)
			}

			info, statErr := os.Stat(path)
			if statErr != nil || !info.IsDir() {
				// Missing/unresolvable repo: NOT matching.
				return false, nil
			}

			inside, _, err := gitRun(path, "rev-parse", "--is-inside-work-tree")
			if err != nil {
				// Genuine failure to run git.
				return false, err
			}
			if inside != "true" {
				// Not a git work tree (dir exists but git does not track it):
				// skip, treated as matching.
				continue
			}

			resolvable, err := headResolvable(path)
			if err != nil {
				return false, err
			}
			if !resolvable {
				// Work tree whose HEAD cannot be resolved: NOT matching.
				return false, nil
			}
		}
		return true, nil
	}
}

// EvidenceExists returns a probe matching
// handoff.ValidationDeps.EvidenceExists: given a set of evidence ids it returns
// the subset that are MISSING (have no backing record).
//
// Evidence records live under the project's `.punakawan` tree in per-run
// append-only ledgers at `<root>/.punakawan/evidence/<runID>/records.jsonl`
// (see internal/evidence.OpenLedger), each line a JSON EvidenceRecord carrying
// an "id". Because a capsule references evidence purely by id (no runID), this
// probe scans every run's ledger under `.punakawan/evidence/*/records.jsonl`,
// builds the set of all recorded ids, and reports which of the requested ids
// are absent from it. If the evidence tree does not exist yet, the set is empty
// and every requested id is reported missing. Called with an empty slice it
// returns (nil, nil) without touching the filesystem.
func EvidenceExists(root string) func(ids []string) ([]string, error) {
	return func(ids []string) ([]string, error) {
		if len(ids) == 0 {
			return nil, nil
		}
		present, err := recordedEvidenceIDs(root)
		if err != nil {
			return nil, err
		}
		var missing []string
		for _, id := range ids {
			if _, ok := present[id]; !ok {
				missing = append(missing, id)
			}
		}
		return missing, nil
	}
}

// TaskIsCurrent returns a probe matching
// handoff.ValidationDeps.TaskIsCurrent: whether the capsule's current task is
// still the current one.
//
// Task currency is authoritative only in the beads/Dolt issue database, which
// is not a filesystem artifact under a workspace root and cannot be queried
// cheaply here without pulling in that dependency. There is likewise no durable
// task-state file under `.punakawan` to consult. So this probe is deliberately
// conservative: it cannot disprove that the task is current, and reporting a
// spurious "no longer current" would push otherwise-resumable capsules into
// refresh_required, so it always returns (true, nil). The root and taskID are
// accepted for signature symmetry and to leave room for a future cheap check
// (e.g. a task-state file) without changing the constructor's shape.
func TaskIsCurrent(root string) func(taskID string) (bool, error) {
	return func(taskID string) (bool, error) {
		return true, nil
	}
}

// headResolvable reports whether the git work tree at path has a resolvable
// HEAD: either a symbolic ref (covers an unborn branch, e.g. a fresh
// `git init` with no commits) or an actual commit object (covers normal and
// detached-HEAD states). A work tree that has neither is treated as
// unresolvable.
func headResolvable(path string) (bool, error) {
	if _, ok, err := gitRun(path, "symbolic-ref", "-q", "HEAD"); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	_, ok, err := gitRun(path, "rev-parse", "-q", "--verify", "HEAD")
	if err != nil {
		return false, err
	}
	return ok, nil
}

// gitRun runs `git -C dir <args...>` and separates the two failure modes that
// matter here: a non-zero exit (git ran and answered "no" — reported as
// ok=false, err=nil, so callers treat it as data) versus a genuine inability to
// execute git at all (binary missing, permission denied — reported as a real
// error). stderr is discarded.
func gitRun(dir string, args ...string) (out string, ok bool, err error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return "", false, nil
		}
		return "", false, runErr
	}
	return strings.TrimSpace(stdout.String()), true, nil
}

// recordedEvidenceIDs collects every evidence record id present in the
// project's evidence ledgers under `<root>/.punakawan/evidence/*/records.jsonl`.
// A missing evidence tree yields an empty set (not an error). A malformed line
// is skipped rather than failing the whole scan, so one bad record can never
// mask the existence of every other id.
func recordedEvidenceIDs(root string) (map[string]struct{}, error) {
	pattern := filepath.Join(root, ".punakawan", "evidence", "*", "records.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{})
	for _, file := range files {
		if err := scanLedgerIDs(file, ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func scanLedgerIDs(file string, into map[string]struct{}) error {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Id string `json:"id"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			// Skip a malformed record rather than failing the whole scan.
			continue
		}
		if rec.Id != "" {
			into[rec.Id] = struct{}{}
		}
	}
	return scanner.Err()
}
