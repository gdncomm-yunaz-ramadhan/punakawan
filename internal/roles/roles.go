// Package roles validates and durably persists the structured outputs
// Semar, Gareng, Petruk, and Bagong submit through Punakawan's MCP tools
// (§8, §28.4). Each role's reasoning happens in the connected MCP client;
// this package only validates and records the result, per §28.2's division
// of responsibility ("Punakawan is the trusted data and protocol boundary;
// the connected client supplies the reasoning").
package roles

import (
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// submissionProvider marks a knowledge record as produced by a connected
// MCP client submitting a role's output through one of Punakawan's
// submit_* tools, rather than imported from an external system like Jira or
// git. The plan does not name a provider value for this case; this is this
// package's judgment call.
const submissionProvider = "punakawan-mcp"

// newSubmissionRecord builds the common KnowledgeRecord envelope shared by
// every role submission. A role's structured output is the connected
// model's judgment, not an observed fact, so validity starts as inferred
// per §7.4 ("must not silently promote inferred knowledge to verified
// fact") — promoting a submission to verified is a distinct, explicit later
// step this package does not perform.
func newSubmissionRecord(id, title string, recordType protocol.KnowledgeRecordType) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     id,
		Type:   recordType,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    submissionProvider,
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateInferred,
		},
	}
}
