package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitMissingContextRequestInput is submit_missing_context_request's
// input, per punakawan-architecture-enhancement-plan.md §6.4: a subagent
// requesting context its capsule didn't include. Subagents may request
// additional context, but may not search broadly themselves - this only
// records the request; search_knowledge, request_capsule, and
// resolve_missing_context_request are Semar's (the calling agent's) own
// next moves, not something this tool decides.
type SubmitMissingContextRequestInput struct {
	CapsuleId      string   `json:"capsule_id"`
	Query          string   `json:"query"`
	Reason         string   `json:"reason"`
	PreferredTypes []string `json:"preferred_types,omitempty"`
	Blocking       bool     `json:"blocking"`
}

func submitMissingContextRequestHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitMissingContextRequestInput) (*mcp.CallToolResult, protocol.MissingContextRequest, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitMissingContextRequestInput) (*mcp.CallToolResult, protocol.MissingContextRequest, error) {
		if _, err := a.Capsules.Get(in.CapsuleId); err != nil {
			return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: capsule %q: %w; a missing-context request must cite the capsule it was raised from", in.CapsuleId, err)
		}

		rec := protocol.MissingContextRequest{
			Id:             fmt.Sprintf("mcr-%s-%d", in.CapsuleId, time.Now().UnixNano()),
			CapsuleId:      in.CapsuleId,
			Query:          in.Query,
			Reason:         in.Reason,
			PreferredTypes: in.PreferredTypes,
			Blocking:       in.Blocking,
			Status:         protocol.MissingContextRequestStatusPending,
			CreatedAt:      time.Now().UTC(),
		}
		if err := a.ContextRequests.Append(rec); err != nil {
			return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: persist missing context request: %w", err)
		}
		return nil, rec, nil
	}
}

// ListMissingContextRequestsInput is list_missing_context_requests's input.
type ListMissingContextRequestsInput struct {
	Status    string `json:"status,omitempty" jsonschema:"one of pending|added_to_revision|rejected|asked_user; defaults to pending"`
	CapsuleId string `json:"capsule_id,omitempty"`
}

// ListMissingContextRequestsOutput is list_missing_context_requests's output.
type ListMissingContextRequestsOutput struct {
	Requests []protocol.MissingContextRequest `json:"requests"`
}

func listMissingContextRequestsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListMissingContextRequestsInput) (*mcp.CallToolResult, ListMissingContextRequestsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListMissingContextRequestsInput) (*mcp.CallToolResult, ListMissingContextRequestsOutput, error) {
		current, err := a.ContextRequests.Current()
		if err != nil {
			return nil, ListMissingContextRequestsOutput{}, fmt.Errorf("mcpserver: list missing context requests: %w", err)
		}

		status := in.Status
		if status == "" {
			status = string(protocol.MissingContextRequestStatusPending)
		}

		out := ListMissingContextRequestsOutput{Requests: []protocol.MissingContextRequest{}}
		for _, rec := range current {
			if string(rec.Status) != status {
				continue
			}
			if in.CapsuleId != "" && rec.CapsuleId != in.CapsuleId {
				continue
			}
			out.Requests = append(out.Requests, rec)
		}
		return nil, out, nil
	}
}

// ResolveMissingContextRequestInput is resolve_missing_context_request's
// input: Semar's decision (§6.4) on a previously-submitted request.
// Punakawan enforces none of these choices - it only records which one the
// calling agent made.
type ResolveMissingContextRequestInput struct {
	Id string `json:"id"`

	Resolution string `json:"resolution" jsonschema:"one of added_to_revision|rejected|asked_user"`
	Note       string `json:"note,omitempty"`

	// RevisedCapsuleId is required when resolution is added_to_revision: the
	// id of a new capsule (built via request_capsule, typically with
	// retrieval_query set to search for the missing context) that
	// supersedes this request's capsule with the missing context included.
	RevisedCapsuleId string `json:"revised_capsule_id,omitempty"`
}

func resolveMissingContextRequestHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ResolveMissingContextRequestInput) (*mcp.CallToolResult, protocol.MissingContextRequest, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ResolveMissingContextRequestInput) (*mcp.CallToolResult, protocol.MissingContextRequest, error) {
		rec, err := a.ContextRequests.Get(in.Id)
		if err != nil {
			return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: missing context request %q: %w", in.Id, err)
		}

		status := protocol.MissingContextRequestStatus(in.Resolution)
		switch status {
		case protocol.MissingContextRequestStatusAddedToRevision:
			if in.RevisedCapsuleId == "" {
				return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: resolution added_to_revision requires revised_capsule_id (build it with request_capsule first)")
			}
			if _, err := a.Capsules.Get(in.RevisedCapsuleId); err != nil {
				return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: revised capsule %q: %w", in.RevisedCapsuleId, err)
			}
			rec.RevisedCapsuleId = &in.RevisedCapsuleId
		case protocol.MissingContextRequestStatusRejected, protocol.MissingContextRequestStatusAskedUser:
			// no additional fields required
		default:
			return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: unknown resolution %q (want added_to_revision, rejected, or asked_user)", in.Resolution)
		}

		rec.Status = status
		if in.Note != "" {
			rec.ResolutionNote = &in.Note
		}
		now := time.Now().UTC()
		rec.ResolvedAt = &now

		if err := a.ContextRequests.Append(rec); err != nil {
			return nil, protocol.MissingContextRequest{}, fmt.Errorf("mcpserver: persist missing context request resolution: %w", err)
		}
		return nil, rec, nil
	}
}
