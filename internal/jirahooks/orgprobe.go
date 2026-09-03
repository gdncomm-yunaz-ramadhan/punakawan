package jirahooks

import (
	"context"
	"strings"
)

// credentialsRejected is the phrase this codebase's Atlassian adapter
// uses when a 404 turned out to be the token rather than the issue
// (packages/adapter-atlassian/src/restClient.ts). Jira Cloud answers 404
// for both, so the adapter probes the authenticated-user read to tell
// them apart, and this is how that answer travels back over the adapter
// protocol - an RPC error carries a message and nothing else.
const credentialsRejected = "credentials rejected"

// IssueVisible reports whether one organisation's Jira site can see an
// issue key.
//
// A credential the site refuses is returned as an error rather than a
// miss: an expired token says nothing about where the issue actually
// lives, and treating it as "not here" would move a delivery to another
// organisation because a password needs rotating. Any other failure is a
// miss, which is the safe direction - it leads to a question, never to a
// silent choice.
func IssueVisible(ctx context.Context, registry gateResolver, org, issueKey string) (bool, error) {
	gate, err := registry.Gate(ctx, jiraAdapterID(org))
	if err != nil {
		return false, err
	}
	if _, err := gate.Call(ctx, "locate:"+issueKey, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": issueKey}); err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		if strings.Contains(strings.ToLower(err.Error()), credentialsRejected) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}
