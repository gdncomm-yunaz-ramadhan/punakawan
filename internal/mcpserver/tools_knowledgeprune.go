package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultPruneCandidateLimit mirrors ListRecords' own "no limit means
// unbounded" only at the KnowledgeListQuery layer; a sweep tool an agent
// calls repeatedly must default to a bounded page instead, so an unset Limit
// here does not accidentally walk the entire corpus in one call.
const defaultPruneCandidateLimit = 50

// staleAgeThresholdDays is the heuristic age (based on source.retrieved_at)
// past which a record's age becomes a prune signal on its own. It is a
// heuristic annotation only - it never excludes a record from the results or
// forces a verdict; the calling agent decides what to do with it.
const staleAgeThresholdDays = 180

// FindPruneCandidatesInput filters/paginates exactly like
// knowledge.KnowledgeListQuery (no new storage or index is added), plus an
// optional min_age_days applied in Go after the page is fetched. No
// validity_state is required or defaulted: per design, prune candidates are
// not restricted to stale/superseded/invalid - the agent's judgment decides,
// this tool only surfaces signal.
type FindPruneCandidatesInput struct {
	Type          string `json:"type,omitempty" jsonschema:"filter to one knowledge record type"`
	Status        string `json:"status,omitempty" jsonschema:"filter to one record status"`
	ValidityState string `json:"validity_state,omitempty" jsonschema:"filter to one validity state (e.g. stale, superseded, invalid, disputed, observed, verified); omit to include every state"`
	Repository    string `json:"repository,omitempty" jsonschema:"filter to records scoped to one repository"`
	Source        string `json:"source,omitempty" jsonschema:"filter to records from one source.provider"`
	MinAgeDays    int    `json:"min_age_days,omitempty" jsonschema:"only include records whose source.retrieved_at is at least this many days old; applied within the fetched page only - use next_cursor to keep scanning for older records rather than assuming this is a global filter"`
	Limit         int    `json:"limit,omitempty" jsonschema:"max candidates to return, default 50"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous call's next_cursor"`
}

// PruneCandidate carries every signal available from the existing store (no
// fabricated fields): validity/supersession state, structural in-degree
// (Related: records that reference this one - the closest real proxy for
// "still relied on", since no access-time/usage telemetry exists anywhere in
// the store), and source age. Id feeds directly into delete_knowledge's ids.
type PruneCandidate struct {
	Id             string    `json:"id"`
	Type           string    `json:"type"`
	Title          string    `json:"title,omitempty"`
	ValidityState  string    `json:"validity_state"`
	SupersededBy   *string   `json:"superseded_by,omitempty"`
	SourceProvider string    `json:"source_provider,omitempty"`
	RetrievedAt    time.Time `json:"retrieved_at"`
	AgeDays        int       `json:"age_days"`
	RelationCount  int       `json:"relation_count"`
	Signals        []string  `json:"signals,omitempty"`
}

type FindPruneCandidatesOutput struct {
	Candidates []PruneCandidate `json:"candidates"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Scanned    int              `json:"scanned"`
}

func findPruneCandidatesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, FindPruneCandidatesInput) (*mcp.CallToolResult, FindPruneCandidatesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FindPruneCandidatesInput) (*mcp.CallToolResult, FindPruneCandidatesOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, FindPruneCandidatesOutput{}, fmt.Errorf("mcpserver: find_prune_candidates: open knowledge store: %w", err)
		}

		limit := in.Limit
		if limit <= 0 {
			limit = defaultPruneCandidateLimit
		}

		records, nextCursor, err := store.ListRecords(ctx, knowledge.KnowledgeListQuery{
			Type:          in.Type,
			Status:        in.Status,
			ValidityState: in.ValidityState,
			Repository:    in.Repository,
			Source:        in.Source,
			Limit:         limit,
			Cursor:        in.Cursor,
		})
		if err != nil {
			return nil, FindPruneCandidatesOutput{}, fmt.Errorf("mcpserver: find_prune_candidates: list records: %w", err)
		}

		now := time.Now().UTC()
		out := FindPruneCandidatesOutput{NextCursor: nextCursor, Scanned: len(records)}
		for _, rec := range records {
			ageDays := int(now.Sub(rec.Source.RetrievedAt).Hours() / 24)
			if in.MinAgeDays > 0 && ageDays < in.MinAgeDays {
				continue
			}

			related, err := store.Related(rec.Id)
			if err != nil {
				return nil, FindPruneCandidatesOutput{}, fmt.Errorf("mcpserver: find_prune_candidates: related(%s): %w", rec.Id, err)
			}
			relationCount := len(related)

			out.Candidates = append(out.Candidates, PruneCandidate{
				Id:             rec.Id,
				Type:           string(rec.Type),
				Title:          rec.Title,
				ValidityState:  string(rec.Validity.State),
				SupersededBy:   rec.SupersededBy,
				SourceProvider: rec.Source.Provider,
				RetrievedAt:    rec.Source.RetrievedAt,
				AgeDays:        ageDays,
				RelationCount:  relationCount,
				Signals:        pruneSignals(rec, ageDays, relationCount),
			})
		}
		return nil, out, nil
	}
}

// pruneSignals composes human-readable, advisory-only reasons a record
// showed up in a prune sweep. These never gate or filter the result set -
// they exist so an agent reasoning over candidates does not have to
// re-derive them from raw fields.
func pruneSignals(rec protocol.KnowledgeRecord, ageDays, relationCount int) []string {
	var signals []string
	switch rec.Validity.State {
	case protocol.KnowledgeRecordValidityStateStale,
		protocol.KnowledgeRecordValidityStateSuperseded,
		protocol.KnowledgeRecordValidityStateInvalid,
		protocol.KnowledgeRecordValidityStateDisputed:
		signals = append(signals, "validity_state:"+string(rec.Validity.State))
	}
	if relationCount == 0 {
		signals = append(signals, "no incoming references")
	}
	if ageDays > staleAgeThresholdDays {
		signals = append(signals, fmt.Sprintf("retrieved %dd ago (older than the %dd heuristic)", ageDays, staleAgeThresholdDays))
	}
	return signals
}
