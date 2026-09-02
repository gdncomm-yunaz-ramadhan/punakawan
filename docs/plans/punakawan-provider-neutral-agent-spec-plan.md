# Punakawan Provider-Neutral Agent Specification Plan

## Goal

Evolve Punakawan from prompt-based roles into a **provider-neutral agent specification** that can describe Semar, Gareng, Petruk, Bagong, and future roles independently of any model provider or harness.

Punakawan should define:
- what an agent is
- what it is allowed to do
- what tools it can use
- what output it must produce
- how it participates in a workflow
- what evidence and state it can access

The actual model execution should remain pluggable across Codex, Claude, OpenAI Agents SDK, OMP, and future runtimes.

---

# 1. Design Principles

## 1.1 Provider-neutral by default

No core Punakawan type should depend on:
- OpenAI Agents SDK
- Anthropic SDK
- Codex-specific APIs
- OMP-specific APIs
- any provider-specific tool format

Provider-specific behavior belongs behind adapters.

## 1.2 Punakawan owns specification and workflow, not intelligence

Punakawan owns:
- agent definitions
- workflow state
- plans
- tasks
- evidence
- role permissions
- output schemas
- execution records

The configured harness or provider owns:
- model invocation
- reasoning
- token generation
- native tool calling
- provider-specific session behavior

## 1.3 Prompts describe behavior only

Do not duplicate information already represented structurally.

Prompts should contain only:
- role purpose
- responsibilities
- decision boundaries
- constraints
- verification expectations

Do not repeat:
- JSON schema fields
- tool argument schemas
- provider configuration
- workflow implementation details

## 1.4 Evidence over narrative

Agent output should remain traceable to:
- files
- symbols
- commands
- tool calls
- Jira/Confluence records
- plans
- task outputs
- test results

Agent summaries are not evidence.

## 1.5 Smallest sufficient agent team

Semar should invoke only the roles required for the delivery.

```text
Simple change:
Semar -> Petruk -> Bagong

Risky change:
Semar -> Gareng + Petruk -> Semar -> Bagong
```

---

# 2. Target Architecture

```text
                  Coding Harness / Client
             Codex / Claude / OMP / Custom
                         |
                         v
                  Punakawan MCP/API
                         |
                +--------+--------+
                |                 |
          Agent Registry      Workflow Engine
                |                 |
                v                 v
             RoleSpec        Delivery / Plan
                |
        +-------+-------+
        |       |       |
        v       v       v
      Semar   Gareng  Petruk  Bagong
                |
                v
          Runtime Adapter
        +-------+-------+------+
        |       |       |      |
      Codex   Claude  OpenAI   OMP
```

Punakawan should remain usable even when no embedded runtime adapter is configured.

---

# 3. Introduce `RoleSpec`

Create a durable, provider-neutral agent definition.

```go
type RoleSpec struct {
    ID          string
    Name        string
    Description string
    Version     string

    Instructions string

    Capabilities []string
    ToolPolicy   ToolPolicy

    InputSchemaID  string
    OutputSchemaID string

    ExecutionPolicy ExecutionPolicy

    Metadata map[string]string
}
```

## Tool policy

```go
type ToolPolicy struct {
    AllowedTools []string
    DeniedTools  []string
    ReadOnly     bool
}
```

## Execution policy

```go
type ExecutionPolicy struct {
    CanMutate          bool
    RequiresEvidence   bool
    ParallelSafe       bool
    MaxDelegationDepth int
}
```

Avoid provider-specific fields in core role definitions, such as:
- model
- temperature
- reasoning effort
- provider API version
- provider-native agent type

Those belong in runtime configuration.

---

# 4. Role Definition Files

Recommended layout:

```text
agents/
  shared/
    communication.md

  semar/
    agent.yaml
    instructions.md

  gareng/
    agent.yaml
    instructions.md

  petruk/
    agent.yaml
    instructions.md

  bagong/
    agent.yaml
    instructions.md
```

Example:

```yaml
id: gareng
name: Gareng
version: 1
description: Feasibility and risk reviewer
instructions: instructions.md

capabilities:
  - requirement-review
  - feasibility-analysis
  - security-review
  - compatibility-review
  - risk-analysis

output_schema: gareng_review

tools:
  read_only: true
  allowed:
    - evidence_get
    - plan_get
    - delivery_get
    - repository_read

execution:
  can_mutate: false
  requires_evidence: true
  parallel_safe: true
```

---

# 5. Separate Agent Specification From Runtime Configuration

Runtime configuration should live independently.

```yaml
runtimes:
  codex:
    type: codex

  claude:
    type: anthropic

  openai:
    type: openai-agents
```

Optional role-to-runtime overrides:

```yaml
role_runtime:
  semar: codex
  gareng: claude
  petruk: codex
  bagong: openai
```

The same `RoleSpec` must work regardless of runtime.

---

# 6. Agent Registry

Introduce an internal registry:

```go
type AgentRegistry interface {
    List() []RoleSpec
    Get(id string) (RoleSpec, error)
    Reload() error
}
```

Responsibilities:
- load manifests
- load instruction files
- validate schema references
- validate tool references
- reject duplicate IDs
- expose role versions
- support reload where practical

---

# 7. MCP/API Surface

Add:

```text
role_list
role_get
```

## `role_list`

Returns lightweight metadata.

```json
[
  {
    "id": "gareng",
    "name": "Gareng",
    "description": "Feasibility and risk reviewer",
    "version": "1"
  }
]
```

## `role_get`

Returns the complete provider-neutral specification.

```json
{
  "id": "gareng",
  "name": "Gareng",
  "instructions": "...",
  "capabilities": ["risk-analysis"],
  "output_schema": {},
  "tool_policy": {},
  "execution_policy": {}
}
```

Keep `prompt_get` temporarily as a compatibility wrapper around `role_get`.

---

# 8. Harness-Owned Execution

This should remain the default mode.

```text
Codex
  |
  | role_get("gareng")
  v
Punakawan
  |
  | RoleSpec + schema + tools
  v
Codex executes Gareng
  |
  | gareng_review
  v
Punakawan workflow
```

Benefits:
- no provider lock-in
- harness controls model/session
- Punakawan stays lightweight
- works with Codex/Claude/OMP sessions
- no provider credentials required inside Punakawan

---

# 9. Optional Runtime-Owned Execution

Add only after the specification layer is stable.

Possible API:

```text
role_run
```

```json
{
  "role": "gareng",
  "context": {},
  "runtime": "claude"
}
```

Internally:

```text
RoleSpec
   |
   v
Runtime Adapter
   |
   +--> Codex
   +--> Claude
   +--> OpenAI Agents SDK
   +--> OMP
```

`role_run` must remain optional.

---

# 10. Runtime Adapter Interface

```go
type AgentRuntime interface {
    ID() string

    Run(
        ctx context.Context,
        role RoleSpec,
        input AgentInput,
    ) (AgentResult, error)
}
```

Suggested result:

```go
type AgentResult struct {
    RuntimeID string
    RoleID    string

    Output any

    EvidenceRefs []string

    Usage UsageStats

    StartedAt   time.Time
    CompletedAt time.Time
}
```

Provider adapters translate `RoleSpec` into provider-native concepts.

---

# 11. Structured Output Ownership

Schemas remain the source of truth.

```text
RoleSpec.output_schema_id
        |
        v
Schema Registry
        |
        v
JSON Schema
        |
        v
provider/harness structured output
```

Do not duplicate schema definitions inside prompts.

Validate returned agent output before accepting it into workflow state.

This specifically prevents the kind of Bagong prompt/schema drift already observed.

---

# 12. Tool Capability Model

Avoid exposing every MCP tool to every role.

Introduce capability groups:

```yaml
capabilities:
  repository.read:
    - repository_file_get
    - repository_search

  plan.read:
    - plan_get

  plan.write:
    - plan_save

  evidence.read:
    - evidence_get
```

Role manifests can then reference capabilities:

```yaml
tools:
  capabilities:
    - repository.read
    - evidence.read
    - plan.read
```

---

# 13. Recommended Role Permissions

## Semar

Can:
- read workflow state
- read evidence
- invoke roles
- create/update plans
- create/update tasks
- resolve workflow gates

## Gareng

Read-only.

Can:
- inspect requirements
- inspect evidence
- inspect plans
- inspect repositories

Cannot:
- mutate implementation
- modify workflow state directly

## Petruk

Can:
- read repository/evidence
- create/update implementation plans
- execute implementation tasks in execution mode

## Bagong

Read-only verifier.

Can:
- inspect diffs
- inspect tests
- inspect requirements
- inspect evidence

Cannot:
- fix its own findings
- mutate verification evidence
- silently change the implementation it is reviewing

This preserves independent verification.

---

# 14. Workflow Integration

Keep existing semantics:

```text
Delivery Created
      |
      v
    Semar
      |
      +------------+
      |            |
      v            v
   Gareng        Petruk
      |            |
      +------+
             |
             v
           Semar
             |
      clarification?
        |        |
       yes       no
        |        |
        +--------+
             |
             v
        implementation
             |
             v
           Bagong
          /      \
       pass      fail
        |          |
        v          v
     complete    reopen
```

Do not add mandatory approval gates.

Clarification should happen only when:
- important context is missing
- requirements materially conflict
- user intent cannot safely be inferred
- a deliberate product/architecture choice belongs to the user

---

# 15. Agent Context Contract

Define a common input envelope:

```go
type AgentInput struct {
    DeliveryID string

    Goal string

    ContextRefs  []string
    EvidenceRefs []string

    PlanID string

    PreviousOutputs map[string]any
    UserDecisions   []Decision
}
```

Prefer references and targeted hydration instead of injecting the entire delivery history.

---

# 16. Context Budgeting

Support role-specific context policies.

```yaml
context:
  include:
    - requirements
    - acceptance_criteria
    - relevant_evidence

  exclude:
    - unrelated_delivery_history

  max_evidence_items: 20
```

Semar should assemble the minimum useful context for each specialist.

---

# 17. Agent Versioning

Every role should have a version.

```yaml
id: bagong
version: 2
```

Execution records should store:

```text
role_id
role_version
runtime_id
runtime_version
output_schema_version
```

This makes behavior reproducible when instructions evolve.

---

# 18. Execution Records

Track every role invocation.

```go
type AgentExecution struct {
    ID string

    DeliveryID  string
    RoleID      string
    RoleVersion string

    RuntimeID string

    InputRefs []string
    OutputRef string

    ToolCalls int
    TokensIn  int64
    TokensOut int64

    EstimatedCost float64
    DurationMs    int64

    Status string
}
```

Integrate this with Punakawan's existing delivery usage tracking instead of creating another telemetry subsystem.

---

# 19. Mom Integration

Mom remains external to the Punakawan role hierarchy.

```text
Semar
  |
  v
Mom search
  |
  v
relevant knowledge refs
  |
  v
Role execution context
```

Do not make Mom:
- another Punakawan role
- an agent runtime
- a workflow orchestrator

Mom provides shared knowledge. Punakawan owns delivery workflow.

---

# 20. Migration Plan

## Phase 1 - Introduce `RoleSpec`

Add:

```text
agent/
  role_spec.go
  registry.go
```

Create manifests for:
- Semar
- Gareng
- Petruk
- Bagong

Keep existing prompt behavior working.

### Acceptance
- all four roles load as `RoleSpec`
- no workflow behavior changes
- definitions validate at startup

## Phase 2 - Add Role Registry APIs

Add:

```text
role_list
role_get
```

Implement:

```text
prompt_get -> role_get compatibility adapter
```

### Acceptance
- current clients remain functional
- new clients can discover roles without knowing prompt paths

## Phase 3 - Move Schema Ownership Out of Prompts

Runtime injects separately:

```text
instructions
+
output schema
+
tool definitions
```

### Acceptance
- prompts contain behavior only
- structured output validation is unchanged or stricter
- schema drift cannot be introduced by prompt duplication

## Phase 4 - Add Tool Policies

Introduce:

```text
ToolPolicy
CapabilityRegistry
```

### Acceptance
- Gareng and Bagong cannot mutate state
- unauthorized tool calls are rejected
- permissions are test-covered

## Phase 5 - Update Workflow to Use Role IDs

Replace hard-coded prompt paths with:

```text
role_get("semar")
role_get("gareng")
role_get("petruk")
role_get("bagong")
```

### Acceptance
- workflow no longer depends on prompt directory layout
- role version is recorded in execution state

## Phase 6 - Add Agent Execution Records

Persist:
- role/version
- runtime
- tool calls
- token usage
- estimated cost
- duration
- evidence/output references

### Acceptance
Continuing a delivery remains additive and usage metrics accumulate correctly.

## Phase 7 - Add Optional Runtime Adapter Interface

Introduce:

```go
AgentRuntime
```

Start with a mock/test runtime.

### Acceptance
- a role can run through `AgentRuntime`
- harness-owned execution remains unchanged
- core packages remain provider-neutral

## Phase 8 - First Real Runtime Adapter

Implement one provider only.

Recommended first choices:

```text
1. Codex/native harness integration
or
2. OpenAI Agents SDK adapter
```

### Acceptance
The same `RoleSpec` works through both harness-owned and runtime-owned execution.

## Phase 9 - Multi-Runtime Support

Gradually add:

```text
Codex
Claude
OpenAI Agents SDK
OMP
```

Keep provider implementations isolated under:

```text
agent/runtime/<provider>/
```

### Acceptance
Changing provider requires no role definition changes.

---

# 21. Tests

## Unit
Test:
- RoleSpec validation
- duplicate IDs
- invalid schema references
- invalid capabilities
- permissions
- version loading
- runtime adapter selection

## Integration

```text
agent manifest
  -> registry
  -> role_get
  -> schema resolution
  -> output validation
```

## Workflow

```text
Semar
  -> Gareng + Petruk
  -> Semar synthesis
  -> Bagong
```

Verify:
- parallel-safe roles may run concurrently
- Bagong remains independent
- clarification only occurs when justified
- role version is recorded

## Compatibility

Verify existing MCP clients using `prompt_get` continue working during migration.

---

# 22. Suggested Package Layout

```text
agent/
  spec.go
  registry.go
  execution.go
  validation.go

agent/runtime/
  runtime.go

agent/runtime/codex/
agent/runtime/claude/
agent/runtime/openai/
agent/runtime/omp/

agent/capability/
  registry.go

agents/
  shared/
    communication.md

  semar/
    agent.yaml
    instructions.md

  gareng/
    agent.yaml
    instructions.md

  petruk/
    agent.yaml
    instructions.md

  bagong/
    agent.yaml
    instructions.md
```

Do not create provider packages until they are actually implemented.

---

# 23. Non-Goals

Do not turn Punakawan into:
- another general-purpose LLM framework
- a model router
- a vector memory system
- a replacement for Mom
- a mandatory inference server
- an OpenAI/Claude-specific framework
- an autonomous swarm framework

Punakawan should not need to own every agent session.

---

# 24. Definition of Done

The migration is complete when:

1. Semar, Gareng, Petruk, and Bagong are represented as provider-neutral `RoleSpec`s.
2. Prompt files contain behavior rather than duplicated schemas/tool documentation.
3. Clients can discover roles through `role_list` and `role_get`.
4. Existing prompt-based clients remain compatible during migration.
5. Output schemas are centrally validated.
6. Tool access is governed by role permissions.
7. Workflow execution records role and version.
8. Harness-owned execution remains the default.
9. At least one optional runtime adapter proves runtime-owned execution.
10. Switching providers requires no role-definition changes.
11. Bagong remains independently verifiable and read-only.
12. Punakawan remains responsible for workflow/evidence while Mom remains responsible for shared durable knowledge.

---

# Recommended First Implementation Slice

Build only:

```text
RoleSpec
AgentRegistry
agent.yaml manifests
role_list
role_get
prompt_get compatibility
schema resolution
tool policy validation
```

Do **not** build provider adapters yet.

This first slice creates the architectural boundary everything else depends on while keeping the current Punakawan workflow operational.
