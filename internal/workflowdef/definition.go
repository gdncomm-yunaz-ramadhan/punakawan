// Package workflowdef defines and persists workflow *definitions* — the
// mutable, versioned, invokable subsets of registered capabilities described
// in the panel project-performance plan §6 ("Project Workflows"). This is
// deliberately distinct from internal/workflow, which stores workflow *runs*
// (append-only state-machine checkpoints keyed off a fixed 5-value protocol
// enum). A definition is authored/edited by a human via the panel; invoking
// one creates a run in that separate store.
//
// Definitions live one-file-per-id as YAML under
// <workspaceRoot>/.punakawan/workflows/*.yaml, matching the plan's schema
// (version: punakawan.workflow/v1). This package hand-writes the structs
// rather than going through protocol codegen: definitions are a panel-side
// authoring concept, not part of the MCP wire protocol.
package workflowdef

// SchemaVersion is the only accepted value of Definition.Version. Validate
// rejects anything else so a future incompatible schema can be introduced
// under a new version string without silently misreading old files.
const SchemaVersion = "punakawan.workflow/v1"

// Definition is one workflow definition, mirroring the plan §6.1 YAML schema.
// It is a plain data struct: the store (un)marshals it to YAML and Validate
// checks it against the capability registry. Revision is an integer bumped on
// every Save; definitions are immutable *by version*, so each prior revision
// is snapshotted before a new one overwrites the live file.
type Definition struct {
	Version             string         `yaml:"version"`
	ID                  string         `yaml:"id"`
	Name                string         `yaml:"name"`
	Description         string         `yaml:"description"`
	Enabled             bool           `yaml:"enabled"`
	RequiredMetadata    []string       `yaml:"required_metadata,omitempty"`
	Inputs              []Input        `yaml:"inputs,omitempty"`
	Steps               []Step         `yaml:"steps"`
	AllowedCapabilities []string       `yaml:"allowed_capabilities,omitempty"`
	Approval            ApprovalPolicy `yaml:"approval,omitempty"`
	Output              OutputSpec     `yaml:"output,omitempty"`
	Revision            int            `yaml:"revision"`
}

// Input is one declared workflow input parameter (§6.1 inputs[]).
type Input struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  any    `yaml:"default,omitempty"`
}

// Step is one workflow step (§6.1 steps[]). Capability names a registered
// capability (never an arbitrary shell command — see Validate). InputFrom
// lists the ids of prior steps whose output feeds this one.
type Step struct {
	Capability string   `yaml:"capability"`
	Intent     string   `yaml:"intent,omitempty"`
	ID         string   `yaml:"id"`
	InputFrom  []string `yaml:"input_from,omitempty"`
}

// ApprovalPolicy declares which capability classes require human approval
// before the run may perform them (§6.1 approval.required_for).
type ApprovalPolicy struct {
	RequiredFor []string `yaml:"required_for,omitempty"`
}

// OutputSpec names the shape a workflow produces (§6.1 output.type).
type OutputSpec struct {
	Type string `yaml:"type,omitempty"`
}
