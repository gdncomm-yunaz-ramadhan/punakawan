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
// Both yaml and json tags are declared: yaml is the on-disk format the store
// reads/writes; json is the panel API's wire format. Without explicit json
// tags the HTTP layer would emit Go's PascalCase field names, which the
// Svelte client (expecting snake_case/camelCase) silently reads as undefined.
type Definition struct {
	Version          string   `yaml:"version" json:"version"`
	ID               string   `yaml:"id" json:"id"`
	Name             string   `yaml:"name" json:"name"`
	Description      string   `yaml:"description" json:"description"`
	Enabled          bool     `yaml:"enabled" json:"enabled"`
	RequiredMetadata []string `yaml:"required_metadata,omitempty" json:"required_metadata,omitempty"`
	Inputs           []Input  `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	// Selectors, when present, let this workflow be resolved implicitly by an
	// exact capability/intent match instead of requiring the caller to name its
	// id (plan §4.2). Resolution is deliberately non-fuzzy: a match must be
	// exact, and more than one match is returned as ambiguous rather than
	// guessed. A definition without selectors is still fully usable — it just
	// must be invoked by explicit id.
	Selectors           []Selector `yaml:"selectors,omitempty" json:"selectors,omitempty"`
	Steps               []Step     `yaml:"steps" json:"steps"`
	AllowedCapabilities []string   `yaml:"allowed_capabilities,omitempty" json:"allowed_capabilities,omitempty"`
	// Roles carries this workflow's optional per-role restrictions, keyed by
	// role name (semar|gareng|petruk|bagong). A missing key means the
	// workflow imposes no restriction on that role.
	//
	// This field also selects the execution engine: a non-empty map makes the
	// definition delivery-shaped, so invoking it starts a delivery orchestration
	// whose fixed lane/lease/role-stage sequence runs instead of Steps — the
	// steps of such a definition never execute. Validate therefore rejects a
	// definition that declares both.
	Roles    map[string]RoleRestriction `yaml:"roles,omitempty" json:"roles,omitempty"`
	Revision int                        `yaml:"revision" json:"revision"`
}

// RoleRestriction is a workflow's per-role restriction (plan §15, ROLE-010). It
// mirrors the shape internal/roleconfig.Restriction consumes: Mode, if set, is
// a ceiling the effective mode is clamped down to (it can never raise the mode);
// Capabilities entries that are false switch that capability off (entries that
// are true are ignored, since a workflow cannot grant a capability the project
// disabled). Required records whether the workflow expects this role to
// participate at all.
type RoleRestriction struct {
	Required     bool            `yaml:"required,omitempty" json:"required,omitempty"`
	Mode         *string         `yaml:"mode,omitempty" json:"mode,omitempty"`
	Capabilities map[string]bool `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// Selector is one exact capability/intent match rule for implicit workflow
// resolution (plan §4.2). Capability is required; Intent is optional and, when
// set, must match exactly too. Selectors never do fuzzy or partial matching.
type Selector struct {
	Capability string `yaml:"capability" json:"capability"`
	Intent     string `yaml:"intent,omitempty" json:"intent,omitempty"`
}

// Input is one declared workflow input parameter (§6.1 inputs[]).
type Input struct {
	Name     string `yaml:"name" json:"name"`
	Type     string `yaml:"type" json:"type"`
	Required bool   `yaml:"required" json:"required"`
	Default  any    `yaml:"default,omitempty" json:"default,omitempty"`
}

// Step is one workflow step (§6.1 steps[]). Capability names a registered
// capability (never an arbitrary shell command — see Validate). InputFrom
// lists the ids of prior steps whose output feeds this one.
type Step struct {
	Capability string   `yaml:"capability" json:"capability"`
	Intent     string   `yaml:"intent,omitempty" json:"intent,omitempty"`
	ID         string   `yaml:"id" json:"id"`
	InputFrom  []string `yaml:"input_from,omitempty" json:"input_from,omitempty"`
}
