package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/syncqueue"
)

// SyncQueueEntry mirrors syncqueue.Entry for MCP output, since that type's
// fields already use the right JSON shape but living in internal/syncqueue
// rather than pkg/protocol - no generated schema exists for it (this is
// tool-output-only data, not a persisted protocol record type).
type SyncQueueEntry struct {
	Id            string `json:"id"`
	RunId         string `json:"run_id"`
	Adapter       string `json:"adapter"`
	Op            string `json:"op"`
	IssueIdOrKey  string `json:"issue_id_or_key,omitempty"`
	Error         string `json:"error"`
	Attempts      int    `json:"attempts"`
	Status        string `json:"status"`
	ConflictsWith string `json:"conflicts_with,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func toSyncQueueEntry(e syncqueue.Entry) SyncQueueEntry {
	return SyncQueueEntry{
		Id:            e.Id,
		RunId:         e.RunId,
		Adapter:       e.Adapter,
		Op:            e.Op,
		IssueIdOrKey:  e.IssueIdOrKey,
		Error:         e.Error,
		Attempts:      e.Attempts,
		Status:        string(e.Status),
		ConflictsWith: e.ConflictsWith,
		CreatedAt:     e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// ListJiraSyncQueueInput is list_jira_sync_queue's input.
type ListJiraSyncQueueInput struct {
	// RunId, if set, filters to entries recorded for that run only.
	RunId string `json:"run_id,omitempty"`
	// IncludeResolved includes resolved/abandoned entries too; by default
	// only pending (unretried) entries are returned.
	IncludeResolved bool `json:"include_resolved,omitempty"`
}

// ListJiraSyncQueueOutput is list_jira_sync_queue's output.
type ListJiraSyncQueueOutput struct {
	Entries []SyncQueueEntry `json:"entries"`
}

// listJiraSyncQueueHandler lists recorded outbound-adapter-write failures
// (punokawan-nbz), so a caller can see what needs retrying without having
// lost the failure the moment the original tool call returned its error.
func listJiraSyncQueueHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListJiraSyncQueueInput) (*mcp.CallToolResult, ListJiraSyncQueueOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListJiraSyncQueueInput) (*mcp.CallToolResult, ListJiraSyncQueueOutput, error) {
		// Current folds the queue's append-only history to one record per
		// id; List would return every historical record (a pending entry
		// and, separately, the resolved record that superseded it), which
		// is not what "list the queue" should mean here.
		current, err := a.SyncQueue.Current()
		if err != nil {
			return nil, ListJiraSyncQueueOutput{}, fmt.Errorf("mcpserver: list sync queue: %w", err)
		}

		out := ListJiraSyncQueueOutput{Entries: []SyncQueueEntry{}}
		for _, e := range current {
			if !in.IncludeResolved && e.Status != syncqueue.StatusPending {
				continue
			}
			if in.RunId != "" && e.RunId != in.RunId {
				continue
			}
			out.Entries = append(out.Entries, toSyncQueueEntry(e))
		}
		return nil, out, nil
	}
}

// RetryJiraSyncEntryInput is retry_jira_sync_entry's input.
type RetryJiraSyncEntryInput struct {
	EntryId string `json:"entry_id"`
}

// RetryJiraSyncEntryOutput is retry_jira_sync_entry's output.
type RetryJiraSyncEntryOutput struct {
	Resolved bool `json:"resolved"`
}

// retryJiraSyncEntryHandler replays a queued entry's failed write through
// its original adapter. On success it marks the entry resolved; on
// failure, Gate.Call's own SetSyncQueue wiring re-enqueues it under the
// same id with an incremented attempt count, so the error this returns
// already reflects that.
func retryJiraSyncEntryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RetryJiraSyncEntryInput) (*mcp.CallToolResult, RetryJiraSyncEntryOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RetryJiraSyncEntryInput) (*mcp.CallToolResult, RetryJiraSyncEntryOutput, error) {
		current, err := a.SyncQueue.Current()
		if err != nil {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: retry sync entry: %w", err)
		}
		entry, ok := current[in.EntryId]
		if !ok {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: no sync queue entry %q", in.EntryId)
		}
		if entry.Status != syncqueue.StatusPending {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: sync queue entry %q is already %s", in.EntryId, entry.Status)
		}

		gate, err := a.AdapterRegistry.Gate(ctx, entry.Adapter)
		if err != nil {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: retry sync entry: %w", err)
		}
		if _, err := gate.Call(ctx, entry.RunId, entry.Op, entry.Params); err != nil {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: retry sync entry %q: %w", in.EntryId, err)
		}

		if err := a.SyncQueue.Resolve(in.EntryId, syncqueue.StatusResolved); err != nil {
			return nil, RetryJiraSyncEntryOutput{}, fmt.Errorf("mcpserver: mark sync entry %q resolved: %w", in.EntryId, err)
		}
		return nil, RetryJiraSyncEntryOutput{Resolved: true}, nil
	}
}
