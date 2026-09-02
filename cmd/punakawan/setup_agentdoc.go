package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// agentDocFiles are the instruction files coding agents read on their own.
// Both are written when both exist; a repo that keeps only one gets only
// that one, and a repo with neither gets AGENTS.md, which every agent this
// project targets reads.
var agentDocFiles = []string{"AGENTS.md", "CLAUDE.md"}

const agentDocBeginMarker = "<!-- punakawan:begin -->"
const agentDocEndMarker = "<!-- punakawan:end -->"

// agentDocSection is what a consuming repo's instruction file gets told.
// Registering the MCP server exposes the tools but says nothing in the
// repo an agent is actually working in, so using punakawan stayed
// accidental: an agent that never looked at the tool list did the work and
// left no delivery record at all.
const agentDocSection = agentDocBeginMarker + `
## Punakawan delivery tracking

Work in this repository is tracked as a Punakawan delivery. Do not track it
by hand in Jira comments.

- ` + "`plan_get`" + ` before planning, ` + "`plan_save`" + ` once a plan exists or changes.
- ` + "`start_delivery`" + ` with the Jira source, a ` + "`projects`" + ` array naming this
  repository and the tasks to open in it, and a ` + "`session`" + `. Without projects
  the delivery has no lanes and cannot run; without a session nothing
  measures its tokens, cost, or tool calls. Call it again for the same
  issue when more work turns up - it adds to that delivery rather than
  starting another.
- ` + "`map_delivery_work_item`" + ` for each lane, then ` + "`log_delivery_work`" + ` with the
  lane id when that task's work is done.
- ` + "`get_delivery`" + ` shows the current state and the ids the calls above need.
` + agentDocEndMarker

// reportAgentDocSetup adds the punakawan section to this repository's agent
// instruction files. It rewrites its own delimited block in place, so a
// re-run updates the guidance without touching anything a human wrote
// around it - the same merge-and-prune discipline the hook config uses.
func reportAgentDocSetup(cmd *cobra.Command) {
	errOut := cmd.ErrOrStderr()
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "setup: skip agent instructions: %v\n", err)
		return
	}

	for _, name := range agentDocTargets(dir) {
		path := filepath.Join(dir, name)
		changed, err := ensureAgentDocSection(path)
		if err != nil {
			fmt.Fprintf(errOut, "setup: could not update %s: %v\n", name, err)
			continue
		}
		if changed {
			fmt.Fprintf(errOut, "setup: added punakawan delivery guidance to %s\n", name)
		}
	}
}

// agentDocTargets picks which instruction files to write: every one that
// already exists, or AGENTS.md when none does. Creating both in a repo
// that has neither would put the same text in two files an agent reads
// together.
func agentDocTargets(dir string) []string {
	var existing []string
	for _, name := range agentDocFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			existing = append(existing, name)
		}
	}
	if len(existing) == 0 {
		return []string{agentDocFiles[0]}
	}
	return existing
}

// ensureAgentDocSection writes the delimited section into path, replacing
// an existing one rather than appending a second copy. It reports whether
// the file changed, so a re-run that had nothing to do stays quiet.
func ensureAgentDocSection(path string) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	body := string(current)
	var next string
	switch begin := strings.Index(body, agentDocBeginMarker); {
	case begin < 0:
		next = body
		if next != "" && !strings.HasSuffix(next, "\n\n") {
			next = strings.TrimRight(next, "\n") + "\n\n"
		}
		next += agentDocSection + "\n"
	default:
		end := strings.Index(body[begin:], agentDocEndMarker)
		if end < 0 {
			// An unterminated marker means someone edited the block by
			// hand. Replacing to the end of the file would eat whatever
			// they wrote after it, so this is left alone and reported.
			return false, fmt.Errorf("%s has an unterminated %s block", filepath.Base(path), agentDocBeginMarker)
		}
		next = body[:begin] + agentDocSection + body[begin+end+len(agentDocEndMarker):]
	}

	if next == body {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(next), 0o644)
}
