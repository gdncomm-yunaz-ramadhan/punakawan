// Package reconcile implements source version reconciliation (§13.2
// "Reconcile changed versions", "Detect stale cached content"): re-fetching
// a knowledge record's external source through an adapter and comparing it
// against the record's stored provenance via internal/knowledge.CheckStale
// (§7.3/§7.4, built in M2).
package reconcile

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// gateCaller is the subset of *adapters.Gate's behavior reconciliation
// needs. Defined locally (rather than depending on adapters.Gate's
// concrete type) so tests can substitute a fake instead of spawning a real
// adapter subprocess; *adapters.Gate satisfies this interface as-is.
type gateCaller interface {
	Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error)
}

// CheckSourceStale re-fetches rec's external source through gate,
// hashes the raw response, and delegates to store.CheckStale to decide
// whether rec's validity.state should transition to stale.
//
// The raw adapter response bytes are hashed directly rather than parsing
// out a specific "version" field: any change to the underlying Jira issue
// or Confluence page changes its JSON representation, so this detects
// staleness without depending on exactly which fields a given adapter
// normalizes or names, which is a smaller, more robust surface to couple
// to than a specific response schema.
func CheckSourceStale(ctx context.Context, store *knowledge.Store, gate gateCaller, runID string, rec protocol.KnowledgeRecord) (bool, error) {
	op, params, err := fetchOpForSource(rec.Source)
	if err != nil {
		return false, err
	}

	raw, err := gate.Call(ctx, runID, op, params)
	if err != nil {
		return false, fmt.Errorf("reconcile: %s: fetch current source: %w", rec.Id, err)
	}

	stale, err := store.CheckStale(rec.Id, knowledge.ContentHash(raw))
	if err != nil {
		return false, fmt.Errorf("reconcile: %s: check stale: %w", rec.Id, err)
	}
	return stale, nil
}

// fetchOpForSource maps a knowledge record's source provenance onto the
// adapter operation (and parameters) that re-fetches it, per the Atlassian
// adapter's manifest (packages/adapter-atlassian): "atlassian.getJiraIssue"
// takes issueIdOrKey, "atlassian.getConfluencePage" takes pageId — both
// mirroring the Atlassian adapter's stable parameter names
// (getJiraIssue(cloudId, issueIdOrKey), getConfluencePage(cloudId, pageId)),
// so rec.Source.ExternalId is expected to hold the issue key / page id.
func fetchOpForSource(src protocol.KnowledgeRecordSource) (string, map[string]any, error) {
	if src.ExternalId == nil || *src.ExternalId == "" {
		return "", nil, fmt.Errorf("reconcile: source.external_id is required to re-fetch a %s source", src.Provider)
	}

	switch src.Provider {
	case "jira":
		return "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": *src.ExternalId}, nil
	case "confluence":
		return "atlassian.getConfluencePage", map[string]any{"pageId": *src.ExternalId}, nil
	default:
		return "", nil, fmt.Errorf("reconcile: no reconciliation adapter operation for source.provider %q", src.Provider)
	}
}
