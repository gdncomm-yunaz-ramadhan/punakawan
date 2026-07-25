package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// randomLocalID returns a random "<prefix>-<16 hex>" id, used when a caller
// leaves a new record's id unset so the server mints a stable one (mirrors
// internal/panel/api.newID).
func randomLocalID(prefix string) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}

// openContradictionStatuses is the set §18 considers still in play - anything
// not yet resolved, accepted, or superseded. list_contradictions returns these
// by default so a caller sees only the ledger that still needs attention.
var openContradictionStatuses = map[protocol.ContradictionStatus]bool{
	protocol.ContradictionStatusDetected:           true,
	protocol.ContradictionStatusTriaged:            true,
	protocol.ContradictionStatusNeedsClarification: true,
	protocol.ContradictionStatusResolutionProposed: true,
}

// SubmitContradictionInput is submit_contradiction's input.
type SubmitContradictionInput struct {
	Id         string                             `json:"id,omitempty" jsonschema:"optional stable id; the server mints one when omitted"`
	Title      string                             `json:"title" jsonschema:"human-readable title of the disagreement"`
	Severity   protocol.ContradictionSeverity     `json:"severity" jsonschema:"one of informational|minor|material|critical (critical blocks by default)"`
	Subject    protocol.ContradictionSubject      `json:"subject" jsonschema:"the thing in disagreement: type plus a normalized key used for deterministic dedup"`
	Claims     []protocol.ContradictionClaimsElem `json:"claims" jsonschema:"the conflicting claims, each a source, a statement, and optional evidence ids"`
	DetectedBy string                             `json:"detected_by,omitempty" jsonschema:"role or subsystem that detected the contradiction"`
}

// SubmitContradictionOutput carries the stored (or matched) record plus whether
// it was a dedup hit rather than a new detection.
type SubmitContradictionOutput struct {
	Contradiction protocol.Contradiction `json:"contradiction"`
	Deduplicated  bool                   `json:"deduplicated"`
}

func submitContradictionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitContradictionInput) (*mcp.CallToolResult, SubmitContradictionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitContradictionInput) (*mcp.CallToolResult, SubmitContradictionOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Gareng, "contradictions"); err != nil {
			return nil, SubmitContradictionOutput{}, err
		}
		root := a.Workspace.Root

		// Deterministic dedup (§20/CONTRA-012): if a contradiction already exists
		// for the same subject key, return it rather than recording a duplicate.
		if in.Subject.Key != nil && *in.Subject.Key != "" {
			candidates, err := contradiction.FindCandidates(root, string(in.Subject.Type), *in.Subject.Key)
			if err != nil {
				return nil, SubmitContradictionOutput{}, fmt.Errorf("mcpserver: find contradiction candidates: %w", err)
			}
			if len(candidates) > 0 {
				return nil, SubmitContradictionOutput{Contradiction: candidates[0], Deduplicated: true}, nil
			}
		}

		id := in.Id
		if id == "" {
			id = randomLocalID("contradiction")
		}
		blocking := contradiction.DefaultBlocking(in.Severity)
		rec := protocol.Contradiction{
			Id:        id,
			ProjectId: a.Workspace.ID,
			Title:     in.Title,
			Severity:  in.Severity,
			Status:    protocol.ContradictionStatusDetected,
			Subject:   in.Subject,
			Claims:    in.Claims,
			Blocking:  &blocking,
		}
		if in.DetectedBy != "" {
			rec.DetectedBy = &in.DetectedBy
		}
		if err := contradiction.Put(root, rec, contradiction.PutOptions{}); err != nil {
			return nil, SubmitContradictionOutput{}, fmt.Errorf("mcpserver: put contradiction: %w", err)
		}
		stored, err := contradiction.Get(root, id)
		if err != nil {
			return nil, SubmitContradictionOutput{}, fmt.Errorf("mcpserver: read stored contradiction: %w", err)
		}
		return nil, SubmitContradictionOutput{Contradiction: *stored}, nil
	}
}

// ListContradictionsInput is list_contradictions's input.
type ListContradictionsInput struct {
	Status string `json:"status,omitempty" jsonschema:"filter to exactly this status; when omitted returns only still-open contradictions"`
}

// ListContradictionsOutput is list_contradictions's output.
type ListContradictionsOutput struct {
	Contradictions []protocol.Contradiction `json:"contradictions"`
}

func listContradictionsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListContradictionsInput) (*mcp.CallToolResult, ListContradictionsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListContradictionsInput) (*mcp.CallToolResult, ListContradictionsOutput, error) {
		all, err := contradiction.List(a.Workspace.Root)
		if err != nil {
			return nil, ListContradictionsOutput{}, fmt.Errorf("mcpserver: list contradictions: %w", err)
		}
		out := make([]protocol.Contradiction, 0, len(all))
		for _, c := range all {
			if in.Status != "" {
				if string(c.Status) == in.Status {
					out = append(out, c)
				}
				continue
			}
			if openContradictionStatuses[c.Status] {
				out = append(out, c)
			}
		}
		return nil, ListContradictionsOutput{Contradictions: out}, nil
	}
}

// ResolveContradictionInput is resolve_contradiction's input.
type ResolveContradictionInput struct {
	Id        string `json:"id" jsonschema:"the contradiction id to resolve"`
	Statement string `json:"statement" jsonschema:"the confirmed resolution statement"`
	By        string `json:"by" jsonschema:"who confirmed the resolution"`
}

// ResolveContradictionOutput is resolve_contradiction's output.
type ResolveContradictionOutput struct {
	Contradiction protocol.Contradiction `json:"contradiction"`
}

func resolveContradictionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ResolveContradictionInput) (*mcp.CallToolResult, ResolveContradictionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ResolveContradictionInput) (*mcp.CallToolResult, ResolveContradictionOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Gareng, "contradictions"); err != nil {
			return nil, ResolveContradictionOutput{}, err
		}
		root := a.Workspace.Root
		if err := contradiction.Resolve(root, in.Id, in.Statement, in.By); err != nil {
			return nil, ResolveContradictionOutput{}, fmt.Errorf("mcpserver: resolve contradiction: %w", err)
		}
		stored, err := contradiction.Get(root, in.Id)
		if err != nil {
			return nil, ResolveContradictionOutput{}, fmt.Errorf("mcpserver: read resolved contradiction: %w", err)
		}
		return nil, ResolveContradictionOutput{Contradiction: *stored}, nil
	}
}
