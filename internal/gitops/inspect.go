// Package gitops wraps read-only Git operations through the tools.Supervisor,
// per punakawan-go-typescript-detailed-plan.md §3.1 ("Git worktree lifecycle")
// and §22 Milestone 1 ("Git repository inspection"). Every Git invocation in
// this package is supervised (working-directory allowlist, env allowlist,
// timeout, output cap) exactly like any other external tool, and none of it
// writes to the repository.
package gitops

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// StatusResult is the parsed result of `git status --porcelain=v2 --branch`.
type StatusResult struct {
	// Branch is the current branch name, or "" if HEAD is detached.
	Branch string
	// Clean is true when there are no changed, added, deleted, renamed, or
	// untracked files.
	Clean bool
	// ChangedFiles lists the paths of modified/added/deleted/renamed/
	// untracked files, in the order git reported them.
	ChangedFiles []string
}

// Commit is a single entry from `git log`.
type Commit struct {
	SHA     string
	Author  string
	Date    time.Time
	Subject string
}

// Inspector runs read-only Git operations through a tools.Supervisor.
type Inspector struct {
	sup *tools.Supervisor
}

// NewInspector returns an Inspector that runs Git commands through sup.
// Every repository path passed to the Inspector's methods must lie within
// one of sup's AllowedRoots.
func NewInspector(sup *tools.Supervisor) *Inspector {
	return &Inspector{sup: sup}
}

// Status runs `git status --porcelain=v2 --branch` in repoPath and parses
// the result.
func (i *Inspector) Status(ctx context.Context, repoPath string) (*StatusResult, error) {
	res, err := i.run(ctx, repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("gitops: git status: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parseStatus(string(res.Stdout)), nil
}

// parseStatus parses `git status --porcelain=v2 --branch` output.
//
// Porcelain v2 emits one line per entry, space-separated fields:
//   - "# branch.head <name>"       -- header line, name is "(detached)" if
//     HEAD is not on a branch.
//   - "1 <xy> ... <path>"          -- ordinary changed tracked file.
//   - "2 <xy> ... <path>\t<orig>"  -- renamed/copied tracked file.
//   - "u <xy> ..."                 -- unmerged (conflicted) file.
//   - "? <path>"                   -- untracked file.
//   - "! <path>"                   -- ignored file (not requested, but
//     tolerated if present).
//
// For entry lines the path is always the last whitespace-separated field
// (renames carry the origin path after a tab, which we ignore for
// ChangedFiles purposes -- we only need the current path).
func parseStatus(out string) *StatusResult {
	result := &StatusResult{}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			const prefix = "# branch.head "
			if strings.HasPrefix(line, prefix) {
				head := strings.TrimPrefix(line, prefix)
				if head != "(detached)" {
					result.Branch = head
				}
			}
		case '1', '2', 'u':
			path := statusPathField(line)
			if path != "" {
				result.ChangedFiles = append(result.ChangedFiles, path)
			}
		case '?':
			path := strings.TrimSpace(strings.TrimPrefix(line, "?"))
			if path != "" {
				result.ChangedFiles = append(result.ChangedFiles, path)
			}
		}
	}
	result.Clean = len(result.ChangedFiles) == 0
	return result
}

// statusPathField extracts the path from a porcelain v2 "1", "2", or "u"
// entry line. For rename/copy ("2") lines the path is followed by a tab and
// the original path; we split on tab first, then take the last
// space-separated field of the remainder.
func statusPathField(line string) string {
	if tab := strings.IndexByte(line, '\t'); tab != -1 {
		line = line[:tab]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// logFieldSep and logRecordSep are ASCII unit/record separators, chosen
// because they cannot legitimately appear in author names or commit
// subjects, so they safely delimit machine-parseable `git log` output.
const (
	logFieldSep  = "\x1f"
	logRecordSep = "\x1e"
)

// Log runs `git log -n <limit>` in repoPath with a machine-parseable format
// and returns the parsed commits, most recent first.
func (i *Inspector) Log(ctx context.Context, repoPath string, limit int) ([]Commit, error) {
	format := "%H" + logFieldSep + "%an" + logFieldSep + "%aI" + logFieldSep + "%s" + logRecordSep
	res, err := i.run(ctx, repoPath, "log", "-n", strconv.Itoa(limit), "--format="+format)
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("gitops: git log: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parseLog(string(res.Stdout))
}

func parseLog(out string) ([]Commit, error) {
	records := strings.Split(out, logRecordSep)
	var commits []Commit
	for _, rec := range records {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, logFieldSep)
		if len(fields) != 4 {
			return nil, fmt.Errorf("gitops: unexpected git log record shape: %q", rec)
		}
		date, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("gitops: parse commit date %q: %w", fields[2], err)
		}
		commits = append(commits, Commit{
			SHA:     fields[0],
			Author:  fields[1],
			Date:    date,
			Subject: fields[3],
		})
	}
	return commits, nil
}

// Diff runs `git diff <ref>` in repoPath (or `git diff` if ref is empty) and
// returns the raw diff text.
func (i *Inspector) Diff(ctx context.Context, repoPath string, ref string) (string, error) {
	args := []string{"diff"}
	if ref != "" {
		args = append(args, ref)
	}
	res, err := i.run(ctx, repoPath, args...)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("gitops: git diff: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return string(res.Stdout), nil
}

// CurrentBranch runs `git rev-parse --abbrev-ref HEAD` in repoPath and
// returns the current branch name (or "HEAD" if detached, matching Git's
// own convention for --abbrev-ref).
func (i *Inspector) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	res, err := i.run(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("gitops: git rev-parse: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// HeadSHA runs `git rev-parse HEAD` in repoPath and returns the current
// commit SHA, per §15.4 ("Record base commit and resulting commit").
func (i *Inspector) HeadSHA(ctx context.Context, repoPath string) (string, error) {
	res, err := i.run(ctx, repoPath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("gitops: git rev-parse HEAD: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// run executes `git <args...>` in repoPath through the Supervisor.
func (i *Inspector) run(ctx context.Context, repoPath string, args ...string) (*tools.Result, error) {
	return i.sup.Run(ctx, tools.Spec{
		Name: "git",
		Args: args,
		Dir:  repoPath,
	})
}
