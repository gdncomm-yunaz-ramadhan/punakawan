package mcpserver

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// MetadataContextEntry is one selected project-metadata item, rendered for an
// agent per the performance plan's §4.4 ("Metadata use by agents"). The
// structured Key/Description/Value fields carry the raw entry; Rendered is the
// exact key/description/Value text block §4.4 hands to the agent, e.g.
//
//	jira.project_key
//	  Jira project key used for this project.
//	  Value: TRF
//
// Both forms are included so a caller can either display Rendered verbatim or
// re-format the structured fields itself.
type MetadataContextEntry struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Value       any    `json:"value,omitempty" jsonschema:"the metadata value, of whatever type it was stored as"`
	Rendered    string `json:"rendered"`
}

// selectProjectMetadata loads the project rooted at workspaceRoot and returns
// the bounded, relevance-ordered subset of its metadata for one agent step,
// per §4.4's project.PrioritySelector (requested keys, then the capability
// namespace, then an exact intent match, then general fill up to the selector's
// strict limit). It never dumps the whole metadata set into a prompt.
//
// It is deliberately additive and safe on a project that has never had
// metadata: project.Load synthesizes a zero-metadata project when there is no
// .punakawan/project.yaml, so this returns (nil, nil) and callers inject
// nothing, leaving pre-metadata behavior unchanged.
func selectProjectMetadata(workspaceRoot, capability, intent string, requestedKeys []string) ([]MetadataContextEntry, error) {
	p, err := project.Load(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: load project metadata: %w", err)
	}
	if len(p.Metadata) == 0 {
		return nil, nil
	}

	selected := project.PrioritySelector{}.Select(*p, capability, intent, requestedKeys)
	if len(selected) == 0 {
		return nil, nil
	}

	out := make([]MetadataContextEntry, len(selected))
	for i, e := range selected {
		out[i] = MetadataContextEntry{
			Key:         e.Key,
			Description: e.Description,
			Value:       e.Value,
			Rendered:    renderMetadataEntry(e),
		}
	}
	return out, nil
}

// toDossierMetadata converts the selected entries to the codegen shape the
// persisted context-dossier record carries (§4.4). Returns nil for an empty
// selection so the schema-optional field is omitted entirely.
func toDossierMetadata(entries []MetadataContextEntry) []protocol.KnowledgeRecordContextDossierProjectMetadataElem {
	if len(entries) == 0 {
		return nil
	}
	out := make([]protocol.KnowledgeRecordContextDossierProjectMetadataElem, len(entries))
	for i, e := range entries {
		out[i] = protocol.KnowledgeRecordContextDossierProjectMetadataElem{
			Key:      e.Key,
			Value:    e.Value,
			Rendered: e.Rendered,
		}
		if e.Description != "" {
			d := e.Description
			out[i].Description = &d
		}
	}
	return out
}

// renderMetadataEntry renders a single metadata entry in §4.4's block format:
// the key on its own line, the description (when present) indented beneath it,
// and a "Value: <value>" line last. It is the per-entry unit of the example in
// §4.4; callers join multiple entries with a blank line.
func renderMetadataEntry(e project.MetadataEntry) string {
	var b strings.Builder
	b.WriteString(e.Key)
	if d := strings.TrimSpace(e.Description); d != "" {
		b.WriteString("\n  ")
		b.WriteString(d)
	}
	fmt.Fprintf(&b, "\n  Value: %v", e.Value)
	return b.String()
}
