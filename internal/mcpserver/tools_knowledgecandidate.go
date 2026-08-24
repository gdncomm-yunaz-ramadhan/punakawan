package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/knowledgefacade"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// KnowledgeRecordCandidateInput is knowledge_record_candidate's input: the
// write-side counterpart to search_knowledge/get_knowledge_records, named
// "candidate" (not "create") because it is evidence offered to a
// KnowledgeSink to decide what to do with, not an assertion of canonical
// truth - today's only sink with a real backend is the local store, but a
// future Mom sink is expected to receive these the same way.
type KnowledgeRecordCandidateInput struct {
	Id      string                       `json:"id,omitempty" jsonschema:"optional stable id in pkw:<kind>/<workspace>/<local-id> form; a project-scoped id is generated when omitted"`
	Type    protocol.KnowledgeRecordType `json:"type" jsonschema:"knowledge type, for example decision, assumption, constraint, evidence, risk, or convention-profile"`
	Title   string                       `json:"title" jsonschema:"short human-readable title"`
	Content string                       `json:"content" jsonschema:"the reusable knowledge to persist"`
	Summary string                       `json:"summary,omitempty" jsonschema:"optional compact summary for search results"`
	Tags    []string                     `json:"tags,omitempty"`

	ValidityState  protocol.KnowledgeRecordValidityState `json:"validity_state,omitempty" jsonschema:"defaults to inferred; use observed for directly observed facts, assumed for assumptions, or verified only with verified_by"`
	VerifiedBy     []string                              `json:"verified_by,omitempty" jsonschema:"required when validity_state is verified"`
	SourceProvider string                                `json:"source_provider,omitempty" jsonschema:"source system or actor; defaults to agent"`
}

// KnowledgeRecordCandidateOutput is knowledge_record_candidate's output. Ref
// is the sink's own identifier for the persisted candidate (the local
// provider's ref is the record id).
type KnowledgeRecordCandidateOutput struct {
	Ref    string                   `json:"ref"`
	Record protocol.KnowledgeRecord `json:"record"`
}

func knowledgeRecordCandidateHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, KnowledgeRecordCandidateInput) (*mcp.CallToolResult, KnowledgeRecordCandidateOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in KnowledgeRecordCandidateInput) (*mcp.CallToolResult, KnowledgeRecordCandidateOutput, error) {
		var out KnowledgeRecordCandidateOutput
		if strings.TrimSpace(in.Title) == "" {
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: title is required")
		}
		if strings.TrimSpace(in.Content) == "" {
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: content is required")
		}

		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: open knowledge store: %w", err)
		}

		id := strings.TrimSpace(in.Id)
		if id == "" {
			id = fmt.Sprintf("pkw:knowledge/%s/%s", a.Workspace.ID, randomLocalID("candidate"))
		} else {
			parts := strings.SplitN(id, "/", 3)
			if len(parts) < 3 || parts[1] != a.Workspace.ID {
				return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: id %q must belong to project %q", id, a.Workspace.ID)
			}
		}

		validity := in.ValidityState
		if validity == "" {
			validity = protocol.KnowledgeRecordValidityStateInferred
		}
		switch validity {
		case protocol.KnowledgeRecordValidityStateObserved,
			protocol.KnowledgeRecordValidityStateInferred,
			protocol.KnowledgeRecordValidityStateAssumed,
			protocol.KnowledgeRecordValidityStateVerified:
		default:
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: validity_state must be observed, inferred, assumed, or verified (got %q)", validity)
		}
		if validity == protocol.KnowledgeRecordValidityStateVerified && len(in.VerifiedBy) == 0 {
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: verified_by is required when validity_state is verified")
		}

		provider := strings.TrimSpace(in.SourceProvider)
		if provider == "" {
			provider = "agent"
		}
		now := time.Now().UTC()
		content := strings.TrimSpace(in.Content)
		hash := knowledge.ContentHash([]byte(content))
		rec := protocol.KnowledgeRecord{
			Id: id, Type: in.Type, Title: strings.TrimSpace(in.Title), Status: "active",
			Content: &content, Tags: in.Tags,
			Source:     protocol.KnowledgeRecordSource{Provider: provider, RetrievedAt: now, ContentHash: &hash},
			Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodModelAssisted},
			Validity:   protocol.KnowledgeRecordValidity{State: validity, VerifiedBy: in.VerifiedBy},
		}
		if summary := strings.TrimSpace(in.Summary); summary != "" {
			rec.Summary = &summary
		}
		if validity == protocol.KnowledgeRecordValidityStateVerified {
			rec.Validity.VerifiedAt = &now
		}

		sink := &knowledgefacade.LegacyLocalKnowledgeProvider{Store: store, Project: a.Workspace.ID}
		ref, err := sink.Record(ctx, rec)
		if err != nil {
			return nil, out, fmt.Errorf("mcpserver: knowledge_record_candidate: %w", err)
		}
		return nil, KnowledgeRecordCandidateOutput{Ref: ref, Record: rec}, nil
	}
}
