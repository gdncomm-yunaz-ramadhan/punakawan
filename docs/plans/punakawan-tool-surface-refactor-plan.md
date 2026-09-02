# Punakawan Tool Surface Refactor Plan

## Status

Proposed implementation plan.

## Objective

Refactor Punakawan from a large, low-level MCP toolbox into a focused multi-project delivery orchestrator.

The current tool surface exposes too many internal mechanics to the calling agent. Punakawan currently has 102 MCP tools, including task graph mutation, leases, workflow runtime transitions, plan execution, knowledge management, Jira CRUD, GitHub PR operations, verification bookkeeping, worktree management, file writes, test execution, and role-specific submission APIs.

The target is a small, stable public MCP API centered on four concepts:

1. **Projects**
2. **Workflows**
3. **Plans**
4. **Deliveries**

Everything else should either:

- become internal orchestration behavior,
- move to the subsystem that actually owns the capability,
- be invoked through integrations/hooks,
- or be removed entirely.

The desired result is roughly **12–15 public MCP tools**, with Punakawan internally handling orchestration mechanics without requiring the agent to manually operate its state machine.

---

# 1. Goals

## 1.1 Primary goals

Punakawan should become:

- a multi-project orchestrator,
- an auditable plan and delivery runtime,
- a reusable workflow registry,
- a coordinator for independent implementation and review agents,
- a worktree-aware execution manager,
- a delivery evidence recorder,
- an integration point for delivery hooks such as Jira updates,
- and a clean coordination layer that can work across sessions, models, and agent harnesses.

The calling agent should operate in terms of intent:

```text
register/configure project
save workflow
save plan
start delivery
inspect delivery
answer clarification
approve delivery when required
```

The calling agent should **not** manually operate:

```text
leases
heartbeats
workflow state transitions
task queues
plan-step execution state
worktree lifecycle
verification matrix rows
Jira adapter calls
knowledge pruning
PR thread bookkeeping
repair-cycle state
role-stage state
```

---

# 2. Non-Goals

This refactor should not turn Punakawan into:

- a general-purpose knowledge database,
- a source-code graph database,
- a generic Jira MCP server,
- a generic GitHub MCP server,
- a coding harness,
- a shell replacement,
- a file editor,
- a CI platform,
- or a second task-management system.

Those responsibilities already belong elsewhere or are better handled by the active agent harness.

---

# 3. Current Architectural Problems

## 3.1 Tool count is a symptom, not the root problem

The current default facade exposes only 11 tools, while another 91 tools are hidden behind `find_tool`.

This reduces initial schema exposure but does not reduce architectural complexity.

The agent still has to reason about:

- which hidden tool exists,
- when to discover it,
- which state must exist first,
- which sequence of operations is valid,
- which tool belongs to which execution model,
- and which low-level state transition should happen next.

`find_tool` therefore hides complexity instead of removing it.

---

## 3.2 Punakawan has multiple overlapping execution models

There are currently several ways to represent and execute work.

### Delivery execution

Examples:

- `create_parent_task`
- `create_lane`
- `add_dependency_edge`
- `list_runnable_lanes`
- `claim_lane`
- `heartbeat_lease`
- `complete_lease`
- `reject_lease`

### Legacy workflow execution

Examples:

- `create_workflow_run`
- `advance_workflow`
- `get_workflow_state`
- `get_next_workflow_step`
- `complete_workflow_step`
- `record_work_outcome`

### Plan-step execution

Examples:

- `plan_step_ready`
- `plan_step_claim`
- `plan_step_complete`
- `plan_step_reopen`

### Beads/taskstore execution

Examples:

- `list_ready_tasks`
- `claim_ready_task`
- `reopen_task`
- `report_discovered_task`
- `submit_task_graph`

These models answer substantially the same runtime questions:

- What work exists?
- What is ready?
- Who owns it?
- What is blocked?
- What completed?
- What needs to be retried?

Maintaining all of them creates duplicate state machines and forces the agent to understand internal implementation history.

---

## 3.3 Workflow definitions and workflow execution are conflated

A workflow should describe reusable execution policy.

It should not need its own independent runtime engine.

The desired model is:

```text
Workflow Definition
        |
        v
      Plan
        |
        v
     Delivery
```

A workflow invocation should instantiate or select a plan and start a delivery.

There should be one runtime execution domain: **Delivery**.

---

## 3.4 Plan execution duplicates delivery execution

Plans are valuable because they are:

- auditable,
- revisioned,
- reviewable,
- reusable as execution input,
- and stable across sessions.

However, plan-step claiming and completion create another scheduler.

Plans should define work and dependencies.

Delivery should execute them.

The Plan domain should remain an immutable/revisioned artifact model, not a queue manager.

---

## 3.5 Knowledge management is outside Punakawan's core responsibility

Punakawan currently exposes knowledge lifecycle operations such as:

- create,
- search,
- fetch,
- candidate capture,
- pruning,
- deletion,
- contradiction management,
- missing-context requests,
- reusable learning proposals.

This creates a second major product inside the orchestrator.

The long-term ownership should be:

- **Mom**: shared experiential memory and reusable knowledge across agents, sessions, models, and harnesses.
- **Codepedia**: codebase topology, dependency graph, blast radius, test coverage relationships, deployment/schema structure.
- **Punakawan**: workflows, plans, deliveries, orchestration state, execution evidence, and references to external/shared knowledge.

Punakawan may retain references such as:

```yaml
context_refs:
  - mom://decision/abc123
  - mom://convention/xyz789
  - codepedia://service/affiliate-platform/impact/456
```

It should not own duplicate canonical knowledge stores.

---

# 4. Target Architecture

```text
                    +-------------------+
                    |      Project      |
                    | config + policies |
                    +---------+---------+
                              |
                              v
                    +-------------------+
                    |     Workflow      |
                    | reusable recipe   |
                    +---------+---------+
                              |
                              v
                    +-------------------+
                    |       Plan        |
                    | immutable/revised |
                    +---------+---------+
                              |
                              v
                    +-------------------+
                    |     Delivery      |
                    | runtime authority |
                    +---------+---------+
                              |
          +-------------------+-------------------+
          |                   |                   |
          v                   v                   v
   Worktree Manager      Worker Manager      Review Manager
          |                   |                   |
          |              implementer          Bagong
          |                   |                   |
          +-------------------+-------------------+
                              |
                              v
                         Evidence Store
                              |
                  +-----------+-----------+
                  |                       |
                  v                       v
              Jira Hook               PR Output
```

Only the upper orchestration concepts should be directly exposed through MCP.

Most runtime mechanics stay internal.

---

# 5. Target Public MCP Surface

The recommended target is **13 public tools**.

## 5.1 Project tools

### `upsert_project`

Create or update a project's complete orchestration configuration.

Replaces or absorbs:

- `register_project`
- `set_project_metadata`

Suggested input shape:

```yaml
id: affiliate-platform
repository:
  url: git@github.com:org/affiliate-platform.git
  default_branch: master

execution:
  test_command: ./mvnw test
  build_command: ./mvnw verify

review:
  reviewer_role: bagong
  model: configurable-model
  reasoning: high
  independent_session: true

delivery:
  approval: human
  cleanup_worktree: true
  require_clean_worktree: true

integrations:
  jira:
    enabled: true
    project_key: TRF

hooks:
  delivery_started:
    - jira_progress
  delivery_completed:
    - jira_progress
    - jira_comment
```

### `list_projects`

Return registered projects and concise orchestration metadata.

This avoids requiring the agent to inspect raw config files or internal registries.

---

# 5.2 Workflow tools

### `save_workflow`

Create or update a reusable workflow definition.

This replaces:

- `save_workflow_definition`

The workflow should contain execution policy, not live execution state.

Example:

```yaml
id: feature-delivery

steps:
  - id: analyze
    role: gareng

  - id: implement
    role: petruk
    depends_on:
      - analyze

  - id: review
    role: bagong
    depends_on:
      - implement

review:
  independent_session: true

delivery:
  require_all_projects_ready: true
```

### `get_workflow`

Read a workflow definition and revision.

### `list_workflows`

List reusable workflows available to the project.

### `invoke_workflow`

Resolve a workflow into a plan and delivery.

The invocation should always use the Delivery runtime.

There must be no second legacy workflow execution engine.

---

# 5.3 Plan tools

### `plan_save`

Keep.

Responsibilities:

- create a new plan,
- append immutable revisions,
- preserve dependency structure,
- preserve project routing,
- preserve intent and acceptance criteria.

The plan must remain auditable.

### `plan_get`

Keep.

Responsibilities:

- fetch latest revision,
- fetch an exact historical revision,
- provide the plan used by a delivery.

No public plan-step claim/complete API should remain.

---

# 5.4 Delivery tools

### `start_delivery`

Keep, but simplify its contract.

Responsibilities:

- resolve requirement references,
- select or attach projects,
- attach workflow,
- attach or generate plan,
- create executable internal work graph,
- start orchestration.

It must not create a permanently inert delivery when project routing is unresolved.

If routing is ambiguous, create a pending clarification.

Example:

```yaml
references:
  - TRF-21851

workflow_id: feature-delivery

projects:
  - affiliate-platform
  - affiliate-platform-ui
```

### `get_delivery`

Keep and make it the primary read model.

It should expose:

- delivery status,
- plan revision,
- projects,
- execution units,
- blocked work,
- running work,
- completed work,
- reviewer state,
- verification state,
- pending questions,
- pending approvals,
- PR/output references,
- cleanup state,
- and the single best next action.

This tool should absorb many dedicated getter tools.

### `answer_delivery_question`

Keep, but make it generic.

Suggested contract:

```yaml
delivery_id: del_123
question_id: q_456
answer:
  project_id: affiliate-platform
```

The question itself should carry its required schema.

Do not create separate MCP tools for each new clarification type.

### `cancel_delivery`

Keep.

Cancellation should internally:

- stop new scheduling,
- reject/release active leases,
- preserve audit history,
- cleanup safe temporary worktrees,
- leave user-created remote artifacts untouched unless policy explicitly says otherwise.

### `approve_project_delivery`

Keep only for projects with an explicit human approval gate.

For projects configured as automatic delivery, the internal runtime should continue without an approval tool call.

---

# 6. Tools to Remove Completely

## 6.1 Remove `find_tool`

Delete:

- `find_tool`

Reason:

The final public tool surface should be small enough that discovery is unnecessary.

If a future plugin ecosystem requires capability discovery, use a typed integration registry rather than exposing hidden internal orchestration functions.

---

## 6.2 Remove Beads/taskstore MCP tools

Delete:

- `claim_ready_task`
- `list_ready_tasks`
- `reopen_task`
- `report_discovered_task`
- `submit_task_graph`

If Beads remains temporarily as an internal implementation dependency, hide it entirely behind the Delivery runtime.

The final architecture should not expose Beads semantics.

---

## 6.3 Remove legacy workflow runtime

Delete:

- `create_workflow_run`
- `advance_workflow`
- `get_workflow_state`
- `get_next_workflow_step`
- `complete_workflow_step`
- `record_work_outcome`

All workflow execution should become Delivery execution.

Workflow remains definition-only.

---

## 6.4 Remove plan-step runtime API

Delete from MCP:

- `plan_step_ready`
- `plan_step_claim`
- `plan_step_complete`
- `plan_step_reopen`

If equivalent records are useful for audit history, maintain them internally as delivery execution records.

---

## 6.5 Remove duplicate final-plan submission API

Delete:

- `submit_final_plan`

Use:

- `plan_save`

If finalization semantics are required, model them as plan metadata:

```yaml
status: approved
```

or as a delivery transition.

Do not keep separate persistence APIs for nearly identical Plan writes.

---

# 7. Knowledge Subsystem Migration

## 7.1 Move canonical reusable knowledge to Mom

The following tools should be removed from Punakawan MCP:

- `create_knowledge_record`
- `knowledge_record_candidate`
- `get_knowledge_records`
- `search_knowledge`
- `delete_knowledge`
- `find_prune_candidates`
- `propose_project_learning`

Punakawan may temporarily keep a compatibility adapter while Mom is not yet available, but the adapter must sit behind an internal interface.

Recommended interface:

```go
type KnowledgeProvider interface {
    Search(ctx context.Context, query Query) ([]Reference, error)
    Get(ctx context.Context, refs []Reference) ([]ContextItem, error)
    Propose(ctx context.Context, candidate Candidate) (Reference, error)
}
```

Implementations:

```text
LocalKnowledgeProvider        temporary compatibility
MomKnowledgeProvider          target
```

The MCP API must not change when the backing provider changes.

---

## 7.2 Remove contradiction lifecycle from Punakawan

Remove public tools:

- `submit_contradiction`
- `list_contradictions`
- `resolve_contradiction`

Contradictions that affect the current delivery should appear as delivery blockers.

Example:

```yaml
blockers:
  - type: contradictory_context
    refs:
      - mom://decision/a
      - mom://decision/b
```

Canonical contradiction lifecycle belongs in the knowledge system.

---

## 7.3 Remove missing-context workflow ceremony

Remove public tools:

- `submit_missing_context_request`
- `list_missing_context_requests`
- `resolve_missing_context_request`

Convert missing context into a standard delivery question.

Example:

```yaml
question:
  id: q123
  type: missing_context
  prompt: Which API contract should this implementation follow?
  options:
    - v1
    - v2
```

Then use:

```text
answer_delivery_question
```

This avoids a separate context-request state machine.

---

## 7.4 Replace context-building tools

Remove from public MCP:

- `build_context_dossier`
- `build_task_context`
- `prepare_work_context`

Context preparation should happen automatically when Punakawan dispatches a worker.

The worker should receive a bounded execution package containing:

```yaml
delivery_id:
plan_revision:
project:
task:
acceptance_criteria:
base_commit:
workflow_policy:
context_refs:
codepedia_refs:
required_verification:
worktree:
```

No caller-visible preparatory ritual should be required.

---

# 8. Codepedia Ownership

Move code topology and blast-radius responsibility to Codepedia.

Remove from Punakawan MCP:

- `analyze_impact`
- `record_impact_edge`
- `verify_impact_coverage`

Punakawan should depend on an interface such as:

```go
type CodeIntelligenceProvider interface {
    AnalyzeImpact(ctx context.Context, subject Subject) (ImpactReport, error)
    VerifyCoverage(ctx context.Context, impact ImpactReport) (CoverageReport, error)
}
```

Punakawan stores only evidence references and summaries needed for the delivery.

Example:

```yaml
evidence:
  - type: impact_analysis
    provider: codepedia
    ref: codepedia://impact/abc
    summary: 3 services and 4 tests affected
```

---

# 9. Jira Refactor

## 9.1 Remove Jira CRUD from Punakawan's public agent API

Remove from public MCP:

- `get_jira_issue`
- `ingest_jira_requirement`
- `update_jira_task_progress`
- `add_jira_comment`
- `create_jira_issue`
- `jira_assign_issue`
- `jira_find_sprint`
- `jira_link_issues`
- `jira_search_user`
- `jira_set_story_points`
- `check_jira_skippable`
- `request_jira_clarification`
- `submit_jira_assessment`
- `sync_jira_subtasks`
- `list_jira_sync_queue`
- `retry_jira_sync_entry`
- `call_adapter_operation`

Punakawan should not be a generic Jira server.

---

## 9.2 Resolve Jira references automatically

`start_delivery` should accept:

```text
TRF-21851
```

Punakawan resolves it internally through the configured Jira provider.

There should be no required sequence like:

```text
get issue
ingest issue
create knowledge record
build context
submit graph
start execution
```

One meaningful orchestration call should initiate the work.

---

## 9.3 Jira updates become delivery hooks

Example project configuration:

```yaml
integrations:
  jira:
    enabled: true

hooks:
  delivery_started:
    - jira.update_status
    - jira.comment

  delivery_completed:
    - jira.comment
    - jira.worklog

  delivery_failed:
    - jira.comment
```

Hooks receive stable delivery events.

Example event:

```yaml
type: delivery.completed
delivery_id: del_123
project_id: affiliate-platform
plan_revision: 4
outputs:
  - type: pull_request
    url: https://github.com/org/repo/pull/42
summary: Implemented and verified campaign sharing changes.
```

---

## 9.4 Keep integration failures internal and auditable

A failed Jira hook should not fail the completed code delivery.

Record:

```yaml
integration_events:
  - provider: jira
    event: delivery.completed
    status: failed
    retry_count: 2
```

Retry infrastructure may exist internally.

Do not expose queue management as a normal MCP responsibility.

---

# 10. Approval Model

## 10.1 Delivery approval is meaningful

Keep:

- `approve_project_delivery`

Only when configured.

Example:

```yaml
delivery:
  approval: human
```

or:

```yaml
delivery:
  approval: automatic
```

---

## 10.2 Adapter approval should be removed from public orchestration

Remove:

- `list_pending_approvals`
- `respond_to_adapter_approval`

Adapter authorization should come from project/integration configuration and the permissions of the connected provider.

Human approval should occur at meaningful business boundaries, not individual transport operations.

Good approval:

```text
Approve project delivery?
```

Bad approval:

```text
Approve adapter operation editJiraIssue?
```

---

# 11. Internalize Lane and Lease Mechanics

Keep the underlying concepts if they are useful for concurrency and safety.

Remove from public MCP:

- `claim_lane`
- `heartbeat_lease`
- `complete_lease`
- `reject_lease`
- `list_runnable_lanes`

Recommended internal state:

```text
pending
ready
leased
running
reviewing
repair
completed
failed
cancelled
```

The scheduler manages leases.

Workers should receive a scoped execution token automatically when dispatched.

A user or agent should not manually heartbeat a task.

---

# 12. Internalize Task Graph Mutation

Remove from public MCP:

- `create_parent_task`
- `create_lane`
- `add_dependency_edge`
- `report_discovered_dependency`

The graph should be derived from the Plan.

Example plan:

```yaml
steps:
  - id: backend
    project_id: affiliate-platform

  - id: ui
    project_id: affiliate-platform-ui
    depends_on:
      - backend
```

Delivery creates internal execution units from this structure.

If execution discovers a dependency not present in the plan:

1. record the discovery,
2. create a new Plan revision,
3. reconcile the Delivery graph,
4. keep unaffected work running,
5. block only affected units.

The agent should report a discovery through its normal worker result, not mutate graph tables directly.

---

# 13. Verification Simplification

## 13.1 Keep verification data internally

The following concepts remain useful:

- logic
- unit
- integration
- quality
- e2e
- ci

But remove public MCP bookkeeping tools:

- `record_ci_check`
- `record_verification_dimension`
- `get_verification_matrix`
- `list_task_evidence`
- `check_merge_readiness`
- `request_project_approval`

---

## 13.2 Derive verification state from execution evidence

Examples:

```text
test command success
    -> unit/integration evidence

CI result
    -> ci evidence

Bagong review
    -> logic/quality evidence

Codepedia coverage check
    -> coverage evidence
```

`get_delivery` should summarize the final matrix.

Example:

```yaml
verification:
  logic: passed
  unit: passed
  integration: passed
  quality: passed
  e2e: not_required
  ci: passed
```

---

# 14. Review Model

## 14.1 Bagong remains independent

The independence requirement is valuable and should remain.

The implementation worker and reviewer must use different sessions unless explicitly overridden by project policy.

Example:

```yaml
review:
  role: bagong
  independent_session: true
  model: gpt-x
  reasoning: high
```

---

## 14.2 Remove explicit role-stage submission ceremony

Remove from public MCP:

- `submit_lane_review`
- `submit_review_conclusion`

The orchestrator should dispatch roles according to the workflow.

Suggested lifecycle:

```text
Gareng analysis
      |
      v
Petruk implementation
      |
      v
Bagong independent review
      |
      +---- approved ------> verification
      |
      +---- changes --------> repair cycle
```

Role outputs are stored internally as structured delivery evidence.

---

# 15. Repair Cycles

Remove from public MCP:

- `start_repair_cycle`

Repair should be event-driven.

Example:

```text
review result = changes_requested
       |
       v
Delivery scheduler creates repair attempt
       |
       v
Petruk executes repair
       |
       v
Bagong reviews again
```

Project policy controls limits:

```yaml
review:
  max_repair_cycles: 2
  on_exhaustion: human_escalation
```

---

# 16. Worktree Lifecycle

Worktree safety is a core Punakawan feature and should remain.

The public tool surface should not expose worktree mechanics.

Remove from public MCP:

- `create_worktree`
- `start_task_execution`
- `finish_task_execution`

The existing requirement for the user to manually run a separate CLI approval command before execution should also be removed unless there is a very strong security requirement.

Preferred lifecycle:

```text
Delivery unit starts
    |
    v
Punakawan creates isolated worktree
    |
    v
worker receives worktree path
    |
    v
worker performs implementation
    |
    v
verification/review
    |
    v
commit/push if configured
    |
    v
ensure clean
    |
    v
remove worktree
```

Required invariants:

- never mutate the main checkout,
- one active worktree per execution unit,
- no dirty file remains after successful delivery,
- cleanup happens after delivery completion or safe cancellation,
- failed cleanup is visible in `get_delivery`,
- remote branches are never force-deleted implicitly.

---

# 17. Coding Harness Responsibilities

Remove from Punakawan MCP:

- `write_files`
- `run_in_lane`
- `run_tests`
- `check_diff`
- `commit_task`
- `push_task_branch`

The coding harness already knows how to:

- read files,
- edit files,
- run commands,
- run tests,
- inspect diffs,
- commit changes,
- push branches.

Punakawan should provide:

- isolated worktree,
- execution constraints,
- task scope,
- plan,
- acceptance criteria,
- project policy,
- context references.

The harness performs the implementation.

Punakawan records the result.

---

# 18. PR and GitHub Responsibilities

Remove from Punakawan public MCP:

- `create_pr`
- `publish_pr`
- `review_pr`
- `fetch_unresolved_pr_comments`
- `resolve_review_thread`
- `submit_pr_review_findings`

PR creation may be:

1. performed by the active harness using GitHub,
2. performed by an internal delivery hook,
3. or handled by a configured GitHub provider.

Punakawan stores the resulting output reference:

```yaml
outputs:
  - type: pull_request
    repository: org/affiliate-platform
    number: 42
    url: https://github.com/org/affiliate-platform/pull/42
```

Bagong can review through the normal GitHub/harness capability.

Punakawan stores the structured review result.

---

# 19. OpenAPI Compatibility

`check_openapi_compatibility` is useful, but should not necessarily remain public.

Preferred design:

- register it as a verification capability,
- allow workflows/projects to require it,
- invoke it automatically when relevant files change.

Example:

```yaml
verification:
  openapi_compatibility:
    enabled: true
    required: true
```

The check result becomes evidence.

If keeping the current implementation temporarily, mark the MCP tool deprecated and invoke it internally during the migration.

---

# 20. `get_delivery` as the Main Read Model

Many public getter tools exist because internal state is fragmented.

The refactor should make `get_delivery` the canonical status API.

Suggested response:

```yaml
id: del_123
status: reviewing

workflow:
  id: feature-delivery
  revision: 3

plan:
  id: plan_456
  revision: 7

projects:
  - id: affiliate-platform
    status: reviewing
    work:
      total: 3
      completed: 2
      running: 0
      reviewing: 1
      blocked: 0

    verification:
      logic: pending
      unit: passed
      integration: passed
      quality: pending
      e2e: not_required
      ci: passed

    outputs:
      - type: branch
        ref: task/TRF-21851
      - type: pull_request
        url: https://github.com/org/repo/pull/42

questions: []

approvals:
  - id: approval_1
    type: project_delivery
    project_id: affiliate-platform

cleanup:
  worktrees_remaining: 1

next_action:
  type: approve_project_delivery
  approval_id: approval_1
```

This reduces tool selection reasoning substantially.

---

# 21. Revised Delivery State Machine

Recommended high-level state:

```text
created
  |
  v
resolving
  |
  +--> awaiting_input
  |
  v
planned
  |
  v
executing
  |
  v
reviewing
  |
  +--> repairing --> reviewing
  |
  v
verifying
  |
  +--> awaiting_approval
  |
  v
delivering
  |
  v
completed
```

Terminal states:

```text
completed
failed
cancelled
```

Internal execution-unit state can remain more detailed.

The caller should not manually drive these transitions.

---

# 22. Revised Workflow Model

A workflow should define reusable policy.

Example:

```yaml
id: standard-feature-delivery
revision: 4

roles:
  analysis:
    role: gareng
    required: true

  implementation:
    role: petruk
    model_class: efficient

  review:
    role: bagong
    model_class: reasoning
    independent_session: true

planning:
  require_approval: false

verification:
  require:
    - logic
    - unit
    - quality
    - ci

delivery:
  require_project_approval: true
  cleanup_worktree: true

hooks:
  completed:
    - jira
```

The workflow should not carry runtime execution records.

---

# 23. Revised Plan Model

A Plan should answer:

- what is being changed,
- why,
- which projects are affected,
- which steps exist,
- dependencies,
- acceptance criteria,
- expected outputs,
- verification requirements.

Suggested model:

```yaml
id: plan_123
revision: 5

title: Move banner controller path

source_refs:
  - jira://TRF-21851

steps:
  - id: backend-route
    project_id: affiliate-platform
    objective: Move the controller route.
    acceptance_criteria:
      - Existing clients continue to work through the new route.
    required_verification:
      - unit
      - integration

  - id: automation
    project_id: cucumber-api-affiliate-platform
    objective: Update automated coverage.
    depends_on:
      - backend-route

risks:
  - API consumers may still reference the legacy path.
```

Plan revisions are immutable.

Delivery pins an exact revision.

---

# 24. Revised Project Model

Project configuration should become the single place for project-level orchestration policy.

Suggested fields:

```yaml
id:
name:

repository:
  url:
  default_branch:

execution:
  build_command:
  test_command:
  allowed_commands:
  environment:

worktree:
  enabled:
  cleanup:
  require_clean:

implementation:
  default_role:
  model:
  reasoning:

review:
  enabled:
  role:
  model:
  reasoning:
  independent_session:
  max_repair_cycles:

verification:
  required_dimensions:
  openapi_compatibility:

delivery:
  approval:
  push:
  create_pr:

integrations:
  jira:
  github:
  codepedia:
  mom:

hooks:
  started:
  completed:
  failed:
```

---

# 25. Internal Provider Interfaces

To avoid rebuilding another huge public tool surface, integrations should use internal interfaces.

## 25.1 Requirement provider

```go
type RequirementProvider interface {
    Resolve(ctx context.Context, ref string) (Requirement, error)
}
```

Implementations may include:

```text
JiraRequirementProvider
GitHubIssueRequirementProvider
URLRequirementProvider
FreeTextRequirementProvider
```

---

## 25.2 Knowledge provider

```go
type KnowledgeProvider interface {
    Search(ctx context.Context, query Query) ([]ContextReference, error)
    Resolve(ctx context.Context, refs []ContextReference) ([]ContextItem, error)
}
```

Target implementation:

```text
MomKnowledgeProvider
```

---

## 25.3 Code intelligence provider

```go
type CodeIntelligenceProvider interface {
    AnalyzeImpact(ctx context.Context, subject Subject) (ImpactReport, error)
    VerifyCoverage(ctx context.Context, report ImpactReport) (CoverageReport, error)
}
```

Target implementation:

```text
CodepediaProvider
```

---

## 25.4 Delivery hook

```go
type DeliveryHook interface {
    OnEvent(ctx context.Context, event DeliveryEvent) error
}
```

Examples:

```text
JiraHook
GitHubHook
SlackHook
AuditHook
```

---

# 26. Tool Disposition Summary

## Keep public

- `plan_get`
- `plan_save`
- `start_delivery`
- `get_delivery`
- `answer_delivery_question`
- `cancel_delivery`
- `approve_project_delivery`

Rename/replace:

- `register_project` + `set_project_metadata` -> `upsert_project`
- `save_workflow_definition` -> `save_workflow`
- `invoke_workflow_definition` -> `invoke_workflow`

Add:

- `list_projects`
- `get_workflow`
- `list_workflows`

Target total: **13 tools**.

---

## Remove completely

- `find_tool`
- all Beads tools
- legacy workflow runtime tools
- plan-step execution tools
- `submit_final_plan`

---

## Move to Mom

- knowledge CRUD/search
- learning candidate/proposal lifecycle
- canonical contradiction lifecycle

---

## Move to Codepedia

- impact graph analysis
- impact edge ownership
- cross-repository coverage verification

---

## Internalize

- lane creation
- dependency graph reconciliation
- leases
- heartbeat
- runnable frontier
- worktree lifecycle
- role stage state
- verification matrix writes
- repair cycle
- merge readiness
- context assembly
- integration retries

---

## Delegate to harness/provider

- file writes
- shell execution
- tests
- diff inspection
- commits
- pushes
- PR creation
- PR thread manipulation
- generic Jira CRUD

---

# 27. Migration Plan

## Phase 0 - Freeze Tool Growth

### Goal

Stop adding more low-level MCP tools during the refactor.

### Actions

1. Add an architecture rule:
   - new internal operations must not automatically become MCP tools.
2. Require every proposed MCP tool to answer:
   - Why can this not be represented through Project, Workflow, Plan, or Delivery?
3. Mark the current 102-tool API as legacy.
4. Add telemetry for current tool usage if not already available.

### Exit criteria

- no new hidden MCP tools added,
- current usage frequency is known,
- migration target is agreed.

---

# 28. Phase 1 - Remove Beads

### Goal

Finish the in-flight taskstore detach.

### Remove

- `claim_ready_task`
- `list_ready_tasks`
- `reopen_task`
- `report_discovered_task`
- `submit_task_graph`

### Actions

1. Move all remaining scheduling responsibility into Delivery.
2. Convert Beads-backed task identifiers to Plan step or Delivery execution-unit identifiers.
3. Remove fallback code paths.
4. Remove Beads-specific MCP tests.
5. Remove Beads-specific documentation.
6. Remove configuration that only exists for Beads.

### Exit criteria

- no delivery requires Beads,
- no MCP schema references Beads,
- no runtime state is duplicated into Beads.

---

# 29. Phase 2 - Collapse Workflow Runtime into Delivery

### Goal

Make Delivery the only runtime state machine.

### Remove

- `create_workflow_run`
- `advance_workflow`
- `get_workflow_state`
- `get_next_workflow_step`
- `complete_workflow_step`
- `record_work_outcome`

### Change

`invoke_workflow_definition` -> `invoke_workflow`

Old:

```text
workflow with roles -> delivery
workflow without roles -> legacy run
```

New:

```text
every workflow -> plan -> delivery
```

### Data migration

If existing workflow runs need to remain readable:

- keep read-only legacy tables temporarily,
- expose them only through admin/debug tooling,
- do not allow creating new legacy runs.

### Exit criteria

- exactly one runtime engine exists,
- workflow invocation always returns `delivery_id`.

---

# 30. Phase 3 - Simplify Plan Domain

### Goal

Keep Plan as an auditable artifact only.

### Remove from public MCP

- `plan_step_ready`
- `plan_step_claim`
- `plan_step_complete`
- `plan_step_reopen`
- `submit_final_plan`

### Actions

1. Keep immutable revisions.
2. Pin delivery to exact plan revision.
3. Generate internal execution units from plan steps.
4. Map runtime completion to internal delivery records.
5. Record plan revisions when discovered dependencies materially change the plan.

### Exit criteria

- Plan contains no independently operated scheduler,
- execution state exists only in Delivery.

---

# 31. Phase 4 - Internalize Delivery Mechanics

### Goal

Remove the requirement for the calling agent to operate Punakawan's scheduler.

### Internalize

- lane CRUD,
- task graph CRUD,
- lease CRUD,
- heartbeat,
- repair cycles,
- role-stage transitions,
- verification writes,
- worktree creation/cleanup.

### Actions

1. Introduce `DeliveryScheduler`.
2. Introduce `WorktreeManager`.
3. Introduce `WorkerDispatcher`.
4. Introduce `ReviewCoordinator`.
5. Introduce `VerificationCoordinator`.
6. Have scheduler react to events rather than waiting for explicit MCP state-transition calls.

### Suggested internal events

```text
delivery.started
execution.ready
execution.claimed
execution.completed
review.approved
review.changes_requested
verification.updated
project.ready
delivery.approved
delivery.completed
delivery.failed
delivery.cancelled
```

### Exit criteria

- caller can start a delivery and inspect it without calling a lease or workflow-state tool,
- no manual heartbeat calls are required,
- repair is automatic.

---

# 32. Phase 5 - Separate Knowledge and Code Intelligence

### Goal

Enforce clean domain ownership.

### Knowledge

Introduce:

```text
KnowledgeProvider
```

Temporary:

```text
LocalKnowledgeProvider
```

Target:

```text
MomKnowledgeProvider
```

### Code topology

Introduce:

```text
CodeIntelligenceProvider
```

Target:

```text
CodepediaProvider
```

### Actions

1. Stop adding records to Punakawan knowledge as canonical long-term storage.
2. Store external context references in delivery evidence.
3. Move impact graph ownership out of Punakawan.
4. Keep compatibility adapters until Mom/Codepedia are ready.
5. Remove MCP knowledge/impact tools after consumers are migrated.

### Exit criteria

- Punakawan owns no canonical shared knowledge,
- Punakawan owns no canonical cross-repository code graph.

---

# 33. Phase 6 - Convert Jira and GitHub to Integrations

### Goal

Stop exposing provider APIs as Punakawan orchestration APIs.

### Actions

1. Create `RequirementProvider`.
2. Create `DeliveryHook`.
3. Create optional `OutputPublisher`.
4. Resolve Jira/GitHub references inside `start_delivery`.
5. Move Jira progress logging to hooks.
6. Move PR creation to harness/provider policy.
7. Store output references in Delivery.

### Exit criteria

- no generic Jira CRUD tool is needed for normal delivery,
- no generic GitHub PR tool is needed for normal delivery,
- integration failures are auditable but do not pollute orchestration flow.

---

# 34. Phase 7 - Replace the Facade

### Goal

Delete the hidden-tool mechanism.

### Actions

1. Add the new 13-tool API.
2. Mark old tools deprecated.
3. Add compatibility aliases only where migration genuinely needs them.
4. Remove `find_tool`.
5. Remove `facadeTools`.
6. Stop registering hidden legacy tools.
7. Delete unreachable handlers after a deprecation window.

### Exit criteria

The MCP server directly exposes only the intended public tools.

---

# 35. Backward Compatibility Strategy

Do not keep 102 tools forever merely to avoid a breaking change.

Use a short compatibility layer.

## Stage A

Expose:

```text
new API
legacy API
```

Legacy tools return a deprecation warning.

## Stage B

Disable legacy tools by default.

Allow temporary opt-in:

```yaml
compatibility:
  legacy_mcp_tools: true
```

## Stage C

Delete legacy implementation.

Do not recreate `find_tool` as a permanent compatibility mechanism.

---

# 36. Suggested Package Boundaries

A possible Go structure:

```text
internal/
  project/
    model.go
    service.go
    store.go

  workflow/
    model.go
    service.go
    store.go

  plan/
    model.go
    service.go
    store.go

  delivery/
    model.go
    service.go
    scheduler.go
    execution.go
    events.go
    store.go

  worktree/
    manager.go

  worker/
    dispatcher.go
    result.go

  review/
    coordinator.go
    policy.go

  verification/
    coordinator.go
    evidence.go

  integration/
    requirements/
      provider.go
      jira.go
      github.go

    knowledge/
      provider.go
      local.go
      mom.go

    codeintel/
      provider.go
      codepedia.go

    hooks/
      hook.go
      jira.go
      github.go

  mcpserver/
    project_tools.go
    workflow_tools.go
    plan_tools.go
    delivery_tools.go
```

The MCP server should be a thin adapter around domain services.

It should not contain orchestration logic.

---

# 37. MCP Handler Rule

Every MCP tool should map to a meaningful domain command or query.

Good:

```text
start_delivery
get_delivery
save_workflow
plan_save
```

Bad:

```text
heartbeat_lease
record_verification_dimension
advance_workflow
resolve_missing_context_request
```

A useful test:

> Would a human describe this operation when explaining what Punakawan does?

If not, it probably belongs internally.

---

# 38. Auditability Requirements

Reducing tools must not reduce auditability.

Every delivery should preserve an append-only event history.

Suggested event fields:

```yaml
sequence:
timestamp:
delivery_id:
project_id:
execution_id:
event_type:
actor:
session_id:
model:
source:
summary:
evidence_refs:
```

Important decisions should remain reconstructable:

- plan revision selected,
- workflow revision selected,
- project config revision,
- requirement references,
- worker assignment,
- reviewer assignment,
- discovered dependency,
- repair attempts,
- verification outcome,
- approval,
- PR/output,
- cleanup status.

Auditability belongs in the event model, not in dozens of manual MCP state-write operations.

---

# 39. Delivery Evidence Model

Use a generic evidence record rather than a public tool per evidence type.

Example:

```yaml
id: ev_123
type: test
status: passed

source:
  execution_id: exec_456
  command: ./mvnw test

artifact_refs:
  - tests.json

summary: 438 tests passed.
```

Other types:

```text
analysis
diff
test
ci
review
impact
coverage
openapi
security
output
```

Internal components create evidence.

`get_delivery` summarizes it.

---

# 40. Multi-Agent Execution

Punakawan should orchestrate workers without exposing worker bookkeeping to the calling agent.

Example:

```text
Semar
  |
  +--> Gareng: analyze
  |
  +--> Petruk: implement project A
  |
  +--> Petruk: implement project B
  |
  +--> Bagong: review A
  |
  +--> Bagong: review B
```

Configuration determines:

- model,
- reasoning level,
- session independence,
- concurrency,
- retry limits.

The workflow captures policy.

The Delivery runtime enforces it.

---

# 41. Cross-Session and Cross-Harness Behavior

Punakawan's durable state should allow:

```text
Codex session A
    starts delivery

Claude session B
    resumes delivery

Codex session C
    reviews status
```

`get_delivery` should be sufficient to reconnect.

Do not rely on caller memory for:

- current lane,
- claimed task,
- workflow state,
- pending approval,
- plan revision,
- next action.

These are durable delivery state.

Shared semantic knowledge comes from Mom.

Codebase graph knowledge comes from Codepedia.

---

# 42. Worktree Cleanup Acceptance Rules

A successful project delivery must satisfy:

```text
no active worktree remains
no untracked task directory remains
no dirty task worktree remains
main checkout untouched
```

A failed cleanup must surface as:

```yaml
cleanup:
  status: failed
  reason: ...
  path: ...
```

Delivery may be marked:

```text
completed_with_cleanup_error
```

internally if desired, but the failure must not be silently ignored.

---

# 43. Performance and Token-Efficiency Targets

The refactor should improve agent efficiency measurably.

Track:

## MCP surface

```text
current: 102 tools
target: <= 15 tools
```

## Tool-discovery calls

```text
current: find_tool required for hidden capabilities
target: 0
```

## State-transition calls per normal delivery

Target sequence:

```text
start_delivery
get_delivery
... worker execution occurs internally ...
get_delivery
approve_project_delivery   # only when configured
get_delivery
```

Not:

```text
prepare context
create graph
create lane
claim lane
heartbeat
start execution
write files
run tests
record verification
submit role review
complete lease
request approval
...
```

---

# 44. Testing Strategy

## 44.1 Domain tests

Test:

- workflow instantiation,
- plan revision pinning,
- delivery scheduling,
- dependency blocking,
- repair cycles,
- cancellation,
- project approval,
- worktree cleanup,
- reviewer independence.

---

## 44.2 Migration tests

For every removed tool, verify either:

```text
behavior now happens automatically
```

or:

```text
behavior moved to provider/integration
```

No removed capability should simply disappear accidentally.

---

## 44.3 MCP contract tests

Verify:

- only intended tools are exposed,
- no hidden tool registry remains,
- no `find_tool`,
- tool descriptions stay concise,
- tool schemas do not expose internal state-machine implementation details.

---

## 44.4 End-to-end scenarios

### Scenario 1: Single-project delivery

```text
Jira reference
-> workflow
-> plan
-> implementation
-> review
-> verification
-> PR
-> Jira update
-> cleanup
```

### Scenario 2: Multi-project delivery

```text
one requirement
-> backend project
-> UI project
-> automation project
-> dependency ordering
-> independent review
-> project approval
```

### Scenario 3: Discovered dependency

```text
execution finds hidden dependency
-> plan revision
-> graph reconciliation
-> unaffected work continues
```

### Scenario 4: Review repair

```text
Bagong requests changes
-> repair cycle
-> re-review
-> approval
```

### Scenario 5: Session reconnect

```text
session A starts
session ends
session B calls get_delivery
session B sees exact state and next action
```

### Scenario 6: Integration failure

```text
delivery succeeds
Jira hook fails
delivery remains completed
hook is auditable and retried internally
```

---

# 45. Removal Acceptance Checklist

## Public API

- [ ] MCP exposes no more than 15 normal orchestration tools.
- [ ] `find_tool` is removed.
- [ ] No hidden MCP tool registry remains.
- [ ] No Beads tool remains public.
- [ ] No lease tool remains public.
- [ ] No workflow-state transition tool remains public.
- [ ] No plan-step execution tool remains public.
- [ ] No knowledge CRUD tool remains public.
- [ ] No impact graph mutation tool remains public.
- [ ] No generic Jira CRUD tool remains public.
- [ ] No generic file-writing or shell tool remains public.
- [ ] No role-stage submission tool remains public.

## Runtime

- [ ] Delivery is the only execution state machine.
- [ ] Workflow is definition-only.
- [ ] Plan is revisioned artifact-only.
- [ ] Plan execution is represented in Delivery.
- [ ] Worktree lifecycle is automatic.
- [ ] Lease lifecycle is automatic.
- [ ] Repair cycles are automatic.
- [ ] Verification state is derived automatically.
- [ ] Bagong review uses an independent session by default.

## Ownership

- [ ] Mom is the target owner of reusable shared knowledge.
- [ ] Codepedia is the target owner of code topology and impact analysis.
- [ ] Punakawan stores references rather than duplicate canonical knowledge.
- [ ] Jira is an integration/provider, not a core orchestration domain.
- [ ] GitHub PR operations are provider/harness responsibilities.

## Audit

- [ ] Every delivery pins workflow revision.
- [ ] Every delivery pins plan revision.
- [ ] Every delivery records project configuration revision or digest.
- [ ] Worker and reviewer sessions are auditable.
- [ ] Evidence is durable.
- [ ] Plan revisions are immutable.
- [ ] Cleanup result is auditable.

---

# 46. Recommended Implementation Order

The safest order is:

```text
1. Freeze tool growth
2. Remove Beads
3. Collapse workflow runtime into Delivery
4. Remove plan execution runtime
5. Internalize lane/lease/worktree mechanics
6. Simplify verification/review
7. Introduce provider interfaces
8. Move knowledge ownership toward Mom
9. Move impact ownership toward Codepedia
10. Convert Jira/GitHub behavior to integrations/hooks
11. Introduce final 13-tool MCP API
12. Remove legacy facade/find_tool
13. Delete compatibility code
```

Do not start by merely renaming tools.

The state-model consolidation must happen first, otherwise the new small API will become a facade over the same tangled internals.

---

# 47. Final Target

Punakawan should feel like this to an agent:

```text
What projects exist?
What workflow should I use?
What plan are we executing?
Start the delivery.
What is its current state?
Is anything blocked or awaiting a human?
```

Not:

```text
Which of 102 commands corresponds to the next transition in one of four overlapping state machines?
```

The internal system can remain sophisticated.

The external contract should be boring, small, predictable, and difficult to misuse.

That is the desired end state for Punakawan.
