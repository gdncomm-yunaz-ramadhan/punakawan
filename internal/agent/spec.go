// Package agent defines Punakawan's provider-neutral role specification:
// what Semar/Gareng/Petruk/Bagong each are, what instructions they read,
// what output shape they must produce, and what tool access and mutation
// posture they're declared to have. Before this package, a role's
// instructions (prompts/<role>/prompt.md), its output schema
// (protocol/knowledge.schema.json's <role>_review/plan keys), and its tool
// access (nowhere - internal/workflowdef.RoleRestriction.Capabilities is
// declared but never read for tool-access decisions) were three
// independently-maintained artifacts. RoleSpec is the single declared
// record that ties them together; internal/mcpserver's role_get/role_list
// tools (a later slice) and internal/telemetry's RoleVersion enrichment
// are its first real consumers.
//
// This slice is deliberately static: nothing here enforces ToolPolicy or
// ExecutionPolicy at call time (see the "tool-policy enforcement
// middleware" design tracked separately). RoleSpec today is a declared,
// validated fact about a role, not yet a mechanism.
package agent

// RoleSpec is one role's complete provider-neutral declaration, loaded
// from prompts/<role>/agent.yaml.
type RoleSpec struct {
	// ID is the role's stable key (e.g. "bagong"), matching the role name
	// used elsewhere in the codebase (internal/roleconfig.Role,
	// internal/delivery/rolestage.go's role strings).
	ID string `json:"id"`
	// Name is the role's display name (e.g. "Bagong").
	Name string `json:"name"`
	// Description is a short human-readable summary of what this role does.
	Description string `json:"description"`
	// Version identifies this exact manifest revision. It is surfaced
	// verbatim (e.g. into telemetry.AgentSession.RoleVersion) so a session
	// can be tied back to the manifest content that produced it.
	Version string `json:"version"`
	// Instructions is a path relative to prompts.FS (e.g.
	// "bagong/prompt.md") naming this role's instruction template. It does
	// not include prompts/shared/communication.md, which every role's
	// served MCP prompt still prepends separately.
	Instructions string `json:"instructions"`
	// Capabilities is a coarse, free-form list of capability tags this role
	// is associated with. Nothing consumes it yet; it exists for the same
	// reason internal/workflowdef.RoleRestriction.Capabilities does -
	// declared now, wired to real enforcement later.
	Capabilities []string `json:"capabilities,omitempty"`
	// ToolPolicy declares which MCP tools this role is allowed to call.
	ToolPolicy ToolPolicy `json:"tool_policy"`
	// OutputSchemaID names the key in protocol/knowledge.schema.json's
	// top-level `properties` object that this role's structured output
	// must satisfy (e.g. "bagong_review"). Checked at startup by Validate,
	// not enforced against actual tool output at request time - see
	// internal/mcpserver/server.go's own comment on why no runtime
	// JSON-schema validator exists yet.
	OutputSchemaID string `json:"output_schema"`
	// ExecutionPolicy declares this role's mutation and evidence posture.
	ExecutionPolicy ExecutionPolicy `json:"execution_policy"`
}

// ToolPolicy declares one role's static tool-access posture. Nothing in
// this slice enforces it against a live MCP call; Validate only checks
// that every named tool actually exists as a registered capability, so a
// manifest can never silently drift from the real tool surface.
type ToolPolicy struct {
	// AllowedTools, when non-empty, is the only tools this role may call.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// DeniedTools is always checked first were enforcement to exist:
	// a tool named here is off-limits regardless of AllowedTools.
	DeniedTools []string `json:"denied_tools,omitempty"`
	// ReadOnly marks a role that should never be able to call a mutating
	// tool. Meaningful only once individual tools are annotated as
	// mutating or not - see the deferred enforcement design.
	ReadOnly bool `json:"read_only"`
}

// ExecutionPolicy declares one role's mutation and evidence posture.
type ExecutionPolicy struct {
	// CanMutate reports whether this role is expected to change persisted
	// state (e.g. save a plan) as part of its normal operation.
	CanMutate bool `json:"can_mutate"`
	// RequiresEvidence reports whether this role's output must be grounded
	// in raw evidence rather than another role's summary.
	RequiresEvidence bool `json:"requires_evidence"`
	// ParallelSafe reports whether this role can safely run concurrently
	// with another instance of itself (or with another role) against the
	// same delivery, per the source design doc's smallest-sufficient-team
	// diagram (Semar -> Gareng+Petruk in parallel).
	ParallelSafe bool `json:"parallel_safe"`
}
