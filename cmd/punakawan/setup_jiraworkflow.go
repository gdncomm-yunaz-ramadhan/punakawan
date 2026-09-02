package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/workspace"
)

// jiraWorkflowTemplate is the config a workspace gets when it has none.
//
// Comments and worklogs are switched on because a delivery that records
// its work only inside punakawan is invisible to everyone reading Jira,
// which is where the rest of the team looks. Transitions are switched off:
// a workflow status name is org-specific and unguessable, and attempting
// an unconfigured one fails the transition rather than doing nothing, so
// it stays off until someone fills in real status names below.
const jiraWorkflowTemplate = `# Master switch for writing anything back to Jira. With it false, every
# setting below is inert.
auto_log: true

# Delivery events that post a comment on the linked issue.
comment_events:
  - delivery.started
  - delivery.completed
  - review.changes_required

# Project time tracking: measured intervals recorded with log_delivery_work
# are pushed to the issue as Jira worklog entries.
log_work: true

# Moving an issue's status is a stronger action than commenting, so it is
# opt-in and needs the real status names for each Jira project below.
transition_on_complete: false

# transitions:
#   TRF:
#     start_status: In Progress
#     complete_status: Ready for QA

# Status names this workspace treats as "not real work", excluded from the
# task graph.
skip_statuses: []

# The status meaning "blocked pending clarification", if this workspace has
# one, e.g. "Sent Back to Product Review".
clarification_status: ""
`

// reportJiraWorkflowSetup writes the workspace's Jira workflow config when
// it has none. Without this file every automatic Jira update stays off,
// which is a reasonable default for a library but a silent one for a tool
// whose whole point is that the delivery record reaches Jira: the file
// simply never existed, so nothing was ever written back and no error
// explained why.
//
// It never overwrites an existing file - a workspace that has tuned its
// event list or filled in transition status names must not lose that to a
// re-run - and never fails setup, for the same reason hook configuration
// does not.
func reportJiraWorkflowSetup(cmd *cobra.Command) {
	errOut := cmd.ErrOrStderr()
	ws, err := workspace.DiscoverOrEphemeral(currentDirOrEmpty())
	if err != nil {
		fmt.Fprintf(errOut, "setup: skip jira workflow config: %v\n", err)
		return
	}
	if ws.Ephemeral {
		return
	}

	path := ws.JiraWorkflowPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(errOut, "setup: jira workflow config already present at %s\n", path)
		return
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(errOut, "setup: could not read %s: %v\n", path, err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(errOut, "setup: could not create %s: %v\n", filepath.Dir(path), err)
		return
	}
	if err := os.WriteFile(path, []byte(jiraWorkflowTemplate), 0o644); err != nil {
		fmt.Fprintf(errOut, "setup: could not write %s: %v\n", path, err)
		return
	}
	fmt.Fprintf(errOut, "setup: wrote %s - comments and worklogs on, status transitions off until you fill in transitions\n", path)
}
