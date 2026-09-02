package agent

import (
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/protocol"
)

// knowledgeSchemaFile is the small slice of protocol/knowledge.schema.json
// this package actually needs: which keys exist in the top-level schema's
// `properties` object (there is no separate `$defs`/`definitions` object
// in this file - gareng_review/petruk_plan/bagong_review/final_plan are
// themselves top-level properties).
type knowledgeSchemaFile struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

// KnowledgeSchemaChecker implements SchemaChecker against the real,
// embedded protocol/knowledge.schema.json - the source of truth for every
// role's structured output shape (see protocol.FS). It is a leaf reader,
// not a JSON-Schema validator: it only answers "does this key exist",
// which is exactly what Validate needs to catch a manifest's
// output_schema drifting from the schema file.
type KnowledgeSchemaChecker struct {
	keys map[string]bool
}

// NewKnowledgeSchemaChecker reads and parses the embedded
// knowledge.schema.json once.
func NewKnowledgeSchemaChecker() (*KnowledgeSchemaChecker, error) {
	data, err := protocol.FS.ReadFile("knowledge.schema.json")
	if err != nil {
		return nil, fmt.Errorf("agent: read embedded knowledge.schema.json: %w", err)
	}
	var schema knowledgeSchemaFile
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("agent: parse embedded knowledge.schema.json: %w", err)
	}
	keys := make(map[string]bool, len(schema.Properties))
	for k := range schema.Properties {
		keys[k] = true
	}
	return &KnowledgeSchemaChecker{keys: keys}, nil
}

// Has reports whether id is a key in knowledge.schema.json's top-level
// properties object.
func (c *KnowledgeSchemaChecker) Has(id string) bool {
	return c.keys[id]
}
