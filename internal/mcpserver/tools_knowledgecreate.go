package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CreateKnowledgeRecordInput is the run-independent write path for durable,
// reusable project knowledge. The server supplies honest conservative
// provenance defaults so an agent only needs type/title/content for the common
// case, while still allowing callers to state stronger provenance explicitly.
type CreateKnowledgeRecordInput struct {
	Id               string                                  `json:"id,omitempty" jsonschema:"optional stable id in pkw:<kind>/<workspace>/<local-id> form; a project-scoped id is generated when omitted"`
	Type             protocol.KnowledgeRecordType            `json:"type" jsonschema:"knowledge type, for example decision, assumption, constraint, evidence, risk, or convention-profile"`
	Title            string                                  `json:"title" jsonschema:"short human-readable title"`
	Content          string                                  `json:"content" jsonschema:"the reusable knowledge to persist"`
	Summary          string                                  `json:"summary,omitempty" jsonschema:"optional compact summary for search results"`
	Status           string                                  `json:"status,omitempty" jsonschema:"record status; defaults to active"`
	Tags             []string                                `json:"tags,omitempty"`
	Aliases          []string                                `json:"aliases,omitempty"`
	Relations        []protocol.KnowledgeRecordRelationsElem `json:"relations,omitempty"`
	ValidityState    protocol.KnowledgeRecordValidityState   `json:"validity_state,omitempty" jsonschema:"defaults to inferred; use observed for directly observed facts, assumed for assumptions, or verified only with verified_by"`
	VerifiedBy       []string                                `json:"verified_by,omitempty" jsonschema:"required when validity_state is verified"`
	SourceProvider   string                                  `json:"source_provider,omitempty" jsonschema:"source system or actor; defaults to agent"`
	SourceUri        string                                  `json:"source_uri,omitempty"`
	SourceExternalId string                                  `json:"source_external_id,omitempty"`
}

type CreateKnowledgeRecordOutput struct {
	Id     string                   `json:"id"`
	Record protocol.KnowledgeRecord `json:"record"`
}

var dedicatedKnowledgeTypes = map[protocol.KnowledgeRecordType]string{
	protocol.KnowledgeRecordTypeContextDossier:  "build_context_dossier",
	protocol.KnowledgeRecordTypeGarengReview:    "submit_gareng_review",
	protocol.KnowledgeRecordTypePetrukPlan:      "submit_petruk_plan",
	protocol.KnowledgeRecordTypeSemarSynthesis:  "submit_semar_synthesis",
	protocol.KnowledgeRecordTypeBagongReview:    "submit_bagong_review",
	protocol.KnowledgeRecordTypeFinalPlan:       "submit_semar_synthesis",
	protocol.KnowledgeRecordTypeRetrievalRecipe: "the retrieval-recipe teaching and acceptance workflow",
}

func createKnowledgeRecordHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateKnowledgeRecordInput) (*mcp.CallToolResult, CreateKnowledgeRecordOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateKnowledgeRecordInput) (*mcp.CallToolResult, CreateKnowledgeRecordOutput, error) {
		_ = ctx
		_ = req
		out, err := createKnowledgeRecord(a, in)
		return nil, out, err
	}
}

func createKnowledgeRecord(a *app.App, in CreateKnowledgeRecordInput) (CreateKnowledgeRecordOutput, error) {
	var out CreateKnowledgeRecordOutput
	if strings.TrimSpace(in.Title) == "" {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: title is required")
	}
	if strings.TrimSpace(in.Content) == "" {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: content is required")
	}
	if tool, reserved := dedicatedKnowledgeTypes[in.Type]; reserved {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: type %q has required structured fields; use %s", in.Type, tool)
	}

	store, err := a.OpenKnowledge()
	if err != nil {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: open knowledge store: %w", err)
	}
	id := strings.TrimSpace(in.Id)
	if id == "" {
		id = fmt.Sprintf("pkw:knowledge/%s/%s", a.Workspace.ID, randomLocalID("record"))
	} else {
		parts := strings.SplitN(id, "/", 3)
		if len(parts) < 3 || parts[1] != a.Workspace.ID {
			return out, fmt.Errorf("mcpserver: create_knowledge_record: id %q must belong to project %q", id, a.Workspace.ID)
		}
	}
	if _, err := store.Get(id); err == nil {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: id %q already exists; use propose_project_learning to improve an existing record", id)
	} else if !errors.Is(err, knowledge.ErrNotFound) {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: check id %q: %w", id, err)
	}

	now := time.Now().UTC()
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "active"
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
		return out, fmt.Errorf("mcpserver: create_knowledge_record: validity_state must be observed, inferred, assumed, or verified (got %q)", validity)
	}
	provider := strings.TrimSpace(in.SourceProvider)
	if provider == "" {
		provider = "agent"
	}
	content := strings.TrimSpace(in.Content)
	hash := knowledge.ContentHash([]byte(content))
	rec := protocol.KnowledgeRecord{
		Id: id, Type: in.Type, Title: strings.TrimSpace(in.Title), Status: status,
		Content: &content, Tags: in.Tags, Aliases: in.Aliases, Relations: in.Relations,
		Source:     protocol.KnowledgeRecordSource{Provider: provider, RetrievedAt: now, ContentHash: &hash},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodModelAssisted},
		Validity:   protocol.KnowledgeRecordValidity{State: validity, VerifiedBy: in.VerifiedBy},
	}
	if summary := strings.TrimSpace(in.Summary); summary != "" {
		rec.Summary = &summary
	}
	if uri := strings.TrimSpace(in.SourceUri); uri != "" {
		rec.Source.Uri = &uri
	}
	if externalID := strings.TrimSpace(in.SourceExternalId); externalID != "" {
		rec.Source.ExternalId = &externalID
	}
	if validity == protocol.KnowledgeRecordValidityStateVerified {
		rec.Validity.VerifiedAt = &now
	}
	if err := store.Put(rec); err != nil {
		return out, fmt.Errorf("mcpserver: create_knowledge_record: %w", err)
	}
	return CreateKnowledgeRecordOutput{Id: id, Record: rec}, nil
}
