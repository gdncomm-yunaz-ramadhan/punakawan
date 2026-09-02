# Punakawan Efficiency & Project Hygiene Refactor Plan

## Objective

Refactor Punakawan into a lean, durable orchestration layer for multi-project agent work.

Punakawan should own only the coordination state it uniquely needs:

```text
Project
  ↓
Workflow
  ↓
Plan
  ↓
Delivery
  ↓
Review
```

Execution infrastructure:

```text
Worktree
Runner
Hooks
Knowledge facade
```

Punakawan should **not** become another generic memory system, code intelligence system, shell wrapper, file editor, or issue tracker.

Target responsibility split:

```text
Punakawan
  orchestration
  plans
  reusable workflows
  deliveries
  review state
  execution state
  Jira delivery logging

Mom
  cross-session agent memory
  decisions
  learnings
  rationale
  constraints
  durable experience

Codepedia
  repository/system knowledge
  dependency graph
  APIs
  schemas
  deployment configuration
  blast radius
```

---

# 1. Target Architecture

## 1.1 Core domains

Reduce Punakawan toward these domains:

```text
internal/
  project/
  workflow/
  plan/
  delivery/
  review/
  worktree/
  runner/
  knowledge/
  hooks/
  store/
  mcp/
  panel/
```

Do not try to force this exact directory layout in one commit.

The important rule is:

> Every package must map to a durable Punakawan responsibility or execution infrastructure.

Anything else should be deleted, folded into another domain, or moved behind a provider.

---

# 2. Project Hygiene

## Goal

Punakawan must not leave runtime garbage inside managed repositories.

A project checkout should remain a normal Git repository.

Punakawan-generated runtime state must live outside the project.

## 2.1 Central runtime directory

Use Punakawan's existing OS-specific data directory.

Conceptually:

```text
$PUNAKAWAN_DATA_DIR/
  punakawan.db

  worktrees/
    <delivery-id>/
      <project-id>/
        <repo-id>/

  indexes/
    <project-id>/

  cache/
  logs/
```

Default locations may continue using platform conventions:

```text
macOS:
~/Library/Application Support/punakawan/

Linux:
~/.config/punakawan/
or XDG-compatible data directory

Windows:
%AppData%\punakawan\
```

Prefer a true user **data** directory rather than config directory if changing storage layout is practical.

## 2.2 Remove repo-local runtime state

Stop creating runtime files such as:

```text
<repo>/.punakawan/index/
<repo>/.punakawan/worktrees/
<repo>/.punakawan/runtime/
```

Project repositories may contain declarative configuration if explicitly chosen, but runtime state must not be generated there.

Longer term, prefer central project registration rather than mandatory `.punakawan/project.yaml`.

## Acceptance criteria

- Running Punakawan against a clean repo does not create new untracked files in that repo.
- `git status --porcelain` of the user's normal checkout is unchanged after a delivery.
- Search indexes are outside repositories.
- Worktrees are outside repositories.
- Punakawan can fully clean its runtime worktree directory after successful delivery.

---

# 3. Worktree Lifecycle

## Goal

Use Git worktrees directly as disposable execution environments.

The user's primary checkout must never be modified by Punakawan execution.

## 3.1 Worktree location

Replace:

```text
<workspace>/.punakawan/worktrees/<repo>/<task>
```

with:

```text
<data-dir>/worktrees/<delivery-id>/<project-id>/<repo-id>
```

Use delivery/lane identity rather than generic task identity where possible.

## 3.2 Creation flow

Recommended lifecycle:

```text
resolve repository
  ↓
resolve base ref
  ↓
resolve exact base SHA
  ↓
git worktree add <path> -b punakawan/<delivery-or-lane-id> <base-sha>
  ↓
worker operates only inside worktree
```

Do **not** require the user's main checkout to be clean.

A dirty main checkout is unrelated to a linked worktree created from a resolved commit.

The base SHA is the isolation boundary.

## 3.3 Remove worktree approval ceremony

Creating a worktree is internal execution infrastructure.

Delete the requirement that worktree creation itself needs a human approval record.

Do not ask a user to approve:

```text
git worktree add
```

Approvals should be reserved for genuinely consequential external actions if retained at all.

## 3.4 Safe cleanup

Current behavior uses:

```text
git worktree remove --force
```

Remove this from normal completion.

Successful cleanup:

```text
git status --porcelain
```

must return empty.

Then:

```text
git worktree remove <path>
git worktree prune
```

Afterward remove empty Punakawan-owned directories.

## 3.5 Dirty worktree behavior

Never destroy a dirty worktree automatically.

If dirty:

```text
cleanup_status = blocked
reason = dirty_worktree
```

Delivery remains inspectable.

Expose:

```text
worktree path
changed files
branch
base SHA
current HEAD
```

A dirty orphan can be recovered manually.

## 3.6 Startup janitor

On startup:

```text
git worktree list --porcelain
```

Reconcile Punakawan runtime directories against active deliveries.

Automatically remove only:

- clean worktrees
- associated with terminal deliveries
- whose commits are already recorded
- whose path belongs to Punakawan's central runtime directory

Never auto-delete an unknown dirty worktree.

## Tests

Add tests for:

```text
create worktree while main checkout is dirty
worktree path is outside repository
clean worktree removal succeeds
dirty worktree removal fails
normal cleanup never passes --force
git worktree prune runs after removal
main checkout status remains unchanged
```

---

# 4. Plans Become First-Class

## Goal

Every implementation must be traceable to a plan revision.

No implementation without a persisted plan.

## 4.1 Plan model

Introduce a first-class plan aggregate.

Suggested fields:

```yaml
id:
project_ids:
revision:
objective:
steps:
acceptance_criteria:
verification:
assumptions:
unresolved_questions:
created_by:
created_at:
status:
previous_revision:
reason_for_change:
```

## 4.2 Immutable revisions

Plans are append-only revisions.

Never silently mutate an executed plan.

Example:

```text
plan-123 r1
  ↓ clarification
plan-123 r2
  ↓ implementation begins
plan-123 r3
```

Every delivery and implementation worker references an exact:

```text
plan_id
plan_revision
```

## 4.3 Executable plan rule

A plan step may be delegated to a cheaper implementation worker only when it contains:

```text
objective
target project/repository
expected outcome
acceptance criteria
verification method
no unresolved blocking question
```

Avoid fuzzy "plan confidence" scores.

Use deterministic completeness checks.

## 4.4 Remove plan-as-knowledge

Current final plans are stored through generic knowledge records.

Move them into the Plan domain.

Compatibility code may read historical plan knowledge records during migration.

New plans must not be persisted as generic knowledge.

---

# 5. Reusable Workflows

## Goal

A workflow is a reusable versioned plan template.

Agents should be able to invoke, revise, and reuse workflows across sessions.

## 5.1 Merge workflow concepts

Current duplication:

```text
internal/workflow
internal/workflowdef
recipe
procedural identity/registry concepts
```

Move toward one domain:

```text
workflow
```

## 5.2 Workflow model

Suggested:

```yaml
id:
project_id:
name:
description:
revision:
inputs:
steps:
acceptance_defaults:
verification_defaults:
agent_policy_overrides:
created_at:
created_by:
previous_revision:
change_reason:
```

## 5.3 Invocation

Flow:

```text
Workflow revision
  ↓
resolve inputs
  ↓
instantiate Plan revision
  ↓
create Delivery
```

A workflow itself is not an execution run.

## 5.4 Mutation

Agents may improve workflows.

But mutation means:

```text
workflow r3
  ↓ improvement
workflow r4
```

Never edit historical workflow revisions in place.

## 5.5 Candidates to retire

Fold or delete:

```text
workflowdef      -> workflow
recipe           -> workflow
procident        -> workflow metadata if still needed
procreg          -> workflow registry
learning         -> external memory or explicit workflow revision
```

---

# 6. Delivery as the Human-Facing Audit Artifact

## Goal

A delivery should answer:

> What was requested, what plan was executed, what changed, how was it verified, what did Bagong conclude, and what was reported externally?

## 6.1 Delivery record

Recommended view:

```yaml
id:
title:
objective:
status:

projects:
repositories:

plan:
  id:
  revision:

execution:
  lanes:
  workers:
  branches:
  commits:
  pull_requests:

verification:
  checks:
  tests:
  evidence:

review:
  reviewer:
  result:
  findings:
  reviewed_commit:

jira:
  issue_keys:
  events_logged:

timeline:
```

## 6.2 Remove role-stage ceremony

Do not make these first-class delivery lifecycle fields:

```text
SemarRecordID
GarengRecordID
PetrukRecordID
BagongRecordID
```

Keep role names as UX/branding where helpful.

Runtime concepts should instead be:

```text
orchestrator
implementation worker
reviewer
```

Bagong remains the branded reviewer.

## 6.3 Tasks become plan execution steps

Avoid maintaining a second generic task-management domain if a plan already has steps and a delivery has execution state.

Move toward:

```text
PlanStep
DeliveryStepExecution
```

instead of:

```text
Task
TaskStore
Beads fallback
Beads synchronization
```

This removes a major source of ceremony.

---

# 7. Knowledge Becomes a Facade

## Goal

Punakawan must stop being the source of truth for generic knowledge.

## 7.1 New conceptual interface

Create:

```go
type KnowledgeProvider interface {
    Search(ctx context.Context, req SearchRequest) ([]Record, error)
    Get(ctx context.Context, ref string) (Record, error)
}
```

Optional write interface:

```go
type KnowledgeSink interface {
    Record(ctx context.Context, candidate Candidate) (Reference, error)
}
```

Do not require every provider to support writes.

## 7.2 Provider roles

Conceptually:

```text
MomProvider
  decisions
  learnings
  history
  constraints
  agent memory

CodepediaProvider
  repos
  dependencies
  APIs
  schemas
  environments
  blast radius
```

## 7.3 Compatibility provider

Do not block this refactor on Mom being finished.

Keep existing Punakawan knowledge DB temporarily as:

```text
LegacyLocalKnowledgeProvider
```

It exists only for backward compatibility and migration.

New code should depend on the facade, not directly on:

```text
knowledge.Store
search.Index
OpenKnowledge()
```

## 7.4 Federated search

The Punakawan knowledge tool should support:

```text
source=all
source=mom
source=codepedia
```

Punakawan combines results and returns provider metadata:

```json
{
  "source": "mom",
  "ref": "...",
  "title": "...",
  "summary": "...",
  "score": 0.82
}
```

Do not normalize away provider-specific identifiers.

## 7.5 Remove generic create/delete/prune tools

Eventually retire:

```text
create_knowledge_record
delete_knowledge_record
prune_knowledge
project_learning proposal tools
```

Replace with a much smaller surface:

```text
knowledge_search
knowledge_get
knowledge_record_candidate
```

`knowledge_record_candidate` should normally feed Mom once available.

## 7.6 Delivery feedback to Mom

On successful delivery:

```text
implementation complete
  ↓
verification passes
  ↓
Bagong review accepted
  ↓
delivery accepted
  ↓
candidate durable learning emitted
  ↓
Mom decides how to store/promote it
```

Punakawan should provide evidence, not invent memory truth.

---

# 8. Agent Execution Policy

## Goal

Replace role personality configuration with execution policy.

## 8.1 Target configuration

Example:

```yaml
agents:
  orchestrator:
    model: inherit
    reasoning: high

  implementation:
    strategy: fork
    model: cheaper
    reasoning: medium

  review:
    type: general-purpose
    isolated: true
    model: inherit
    reasoning: high
```

Per-project overrides should be supported.

Workflow revision may further constrain the policy.

## 8.2 Runner abstraction

Introduce a small capability-aware abstraction:

```go
type AgentRunner interface {
    Capabilities(ctx context.Context) Capabilities
    Run(ctx context.Context, req Request) (Result, error)
}
```

Suggested request:

```go
type Request struct {
    Purpose      Purpose
    ProjectID    string
    RepoID       string
    WorktreePath string
    PlanID       string
    PlanRevision int
    Model        string
    Reasoning    string
    Isolated     bool
}
```

Purposes:

```text
orchestrate
implement
review
```

## 8.3 Capability detection

Runner capabilities may include:

```text
fork
model_selection
reasoning_control
isolated_context
```

If project configuration requests a capability the current harness cannot enforce:

```text
fail clearly
```

Do not pretend compliance.

## 8.4 Semar

Semar becomes orchestration behavior:

```text
retrieve knowledge
select workflow
create/revise plan
split executable work
dispatch implementation workers
collect results
trigger Bagong review
finalize delivery
```

Do not create Semar-specific persistence models.

## 8.5 Gareng

Retire Gareng as a mandatory runtime stage.

Gareng-style reasoning becomes part of planning:

```text
risk analysis
contradiction checking
impact questions
security considerations
```

A named Gareng subagent can still be used optionally by a harness.

It must not be required by the state machine.

## 8.6 Petruk

Retire Petruk as a mandatory implementation lifecycle entity.

Implementation becomes:

```text
generic forked worker
```

Branding may still call it Petruk in UI if desired.

State should not depend on that name.

## 8.7 Bagong

Bagong remains mandatory review before successful delivery.

Review must use:

```text
general-purpose subagent
isolated context
configured model/reasoning policy
```

Input should contain:

```text
original requirement
exact plan revision
final diff
commit SHA
verification evidence
required Mom/Codepedia context
```

Do not provide implementation chain-of-thought or implementation transcript.

Review output:

```yaml
result: accepted | changes_required | blocked
findings:
reviewed_commit:
plan_revision:
reviewer_policy:
```

---

# 9. Jira Hooks

## Goal

If a Jira issue is linked and project settings enable logging, Punakawan should update Jira automatically from delivery events.

Do not require the agent to remember to call Jira tools manually.

## 9.1 Event model

Emit delivery events:

```text
delivery.started
plan.created
plan.revised
implementation.started
implementation.completed
review.changes_required
review.accepted
delivery.completed
delivery.failed
```

## 9.2 Hook interface

Suggested:

```go
type Hook interface {
    Handle(ctx context.Context, event Event) error
}
```

Jira becomes one hook implementation.

## 9.3 Configuration

Example:

```yaml
jira:
  auto_log: true
  comment_events:
    - delivery.started
    - plan.revised
    - review.changes_required
    - delivery.completed

  transition_on_complete: false
  log_work: false
```

Comment logging may be enabled by configuration as prior consent.

Jira status transitions and worklog changes should remain separate toggles because they have stronger semantics.

## 9.4 Idempotency

Use deterministic event idempotency keys:

```text
<delivery-id>:<event-type>:<revision>
```

Retry failures safely.

Do not create duplicate comments.

## 9.5 Delivery summary comment

Completion comment should be compact and useful:

```text
Punakawan delivery completed

Plan: plan-123 r4
Projects: service-a, deployment-a
PRs: #123, #456

Verification:
- unit tests passed
- integration tests passed

Bagong review: accepted

Delivery: <id>
```

---

# 10. MCP Surface Reduction

## Goal

Punakawan MCP tools should coordinate durable state, not duplicate Git, shell, file editing, testing, or generic memory.

## 10.1 Target tool surface

Aim for approximately 10-15 tools.

Example:

```text
project_register
project_get
project_list

workflow_save
workflow_list
workflow_invoke

plan_save
plan_get

delivery_start
delivery_get
delivery_update

knowledge_search
knowledge_get

review_submit
```

Some read operations may later become MCP resources.

## 10.2 Remove `find_tool`

The current hidden tool registry exists because the surface is too large.

Delete `find_tool` after tool reduction.

Do not solve tool bloat with a tool-search tool.

## 10.3 Remove coding tools

Punakawan should not provide generic coding-agent tools such as:

```text
fileops
run_tests wrappers
generic git diff wrappers
RTK shell abstraction
```

The implementation agent already has native shell/editor capabilities in its harness.

Punakawan only needs to provide the assigned worktree and record results.

## 10.4 External integration tools

Prefer event hooks and domain actions over dozens of low-level Jira actions.

Example:

```text
link Jira issue to delivery
```

is Punakawan state.

The hook then handles logging.

---

# 11. Beads Removal

## Goal

Punakawan should not require another task tracker to track its own orchestration.

Current architecture still carries Beads-specific state, adapters, instructions, and fallback stores.

This is redundant once Plans and Deliveries become authoritative.

## Migration

Phase out:

```text
internal/beads
taskstore fallback
tools_beadsready
Beads-specific AGENTS.md instructions
Dolt references
Beads synchronization
```

Replace task progression with:

```text
PlanStep
DeliveryStepExecution
```

Do not make removal depend on preserving old Beads behavior forever.

If historical Beads data matters, provide a one-time importer.

---

# 12. UI Simplification

## Target navigation

Keep:

```text
Projects
Deliveries
Plans
Workflows
Knowledge
Settings
```

Remove standalone primary navigation for:

```text
Roles
Tasks
Sessions
Approvals
Health
Context Improvements
```

Map them to:

```text
Roles              -> Settings / Agent Policy
Tasks              -> Plan / Delivery steps
Sessions           -> delivery provenance
Approvals          -> event/policy detail if still needed
Health             -> diagnostics page
Context Improvements -> Mom/workflow revisions
```

## Delivery page

This should become the main audit page.

Show:

```text
objective
plan revision
projects
repositories
workers
commits
PRs
verification
Bagong review
Jira activity
timeline
```

---

# 13. Recommended Implementation Sequence

Do not perform a giant-bang rewrite.

Use the following PR sequence.

---

## PR 1 — Project Hygiene + Safe Worktrees

### Changes

- Add central runtime path helpers.
- Move search index outside repository.
- Move worktrees outside repository.
- Stop requiring clean main checkout.
- Remove worktree approval ceremony.
- Refuse normal cleanup of dirty worktrees.
- Remove `--force` from normal worktree removal.
- Run `git worktree prune` after cleanup.
- Add hygiene tests.

### Acceptance

```text
clean repo before delivery
clean repo after delivery
no .punakawan runtime directory created
dirty user checkout does not block worktree creation
dirty Punakawan worktree cannot be destroyed accidentally
```

---

## PR 2 — First-Class Plan

### Changes

- Add `plan` package/domain.
- Add immutable plan revisions.
- Persist exact plan revision on delivery.
- Add deterministic executable-step validation.
- Stop writing new final plans as knowledge records.
- Add compatibility reader for historical plan knowledge records if needed.

### Acceptance

Every implementation execution references:

```text
plan_id
plan_revision
```

---

## PR 3 — Workflow Consolidation

### Changes

- Merge `workflowdef` behavior into `workflow`.
- Make workflows immutable revisioned templates.
- Invocation produces a Plan.
- Remove recipe/procedural registry duplication.

### Acceptance

```text
workflow revision -> plan revision -> delivery
```

is the only new execution path.

---

## PR 4 — Knowledge Facade

### Changes

- Add `KnowledgeProvider`.
- Add `LegacyLocalKnowledgeProvider`.
- Route MCP search through facade.
- Add provider/source metadata to results.
- Stop direct knowledge-store dependencies from orchestration code.
- Prepare Mom and Codepedia provider extension points.

### Acceptance

Punakawan core can operate without knowing whether knowledge came from:

```text
local compatibility DB
Mom
Codepedia
```

---

## PR 5 — Agent Runner + Policy

### Changes

- Add runner abstraction.
- Replace role configuration with execution policy.
- Implement capability checks.
- Support forked implementation worker.
- Support isolated general-purpose Bagong review.
- Remove Gareng/Petruk from mandatory state transitions.

### Acceptance

Project config can declare:

```yaml
implementation:
  strategy: fork
  model: cheaper

review:
  type: general-purpose
  isolated: true
```

Unsupported capabilities fail explicitly.

---

## PR 6 — Jira Delivery Hooks

### Changes

- Add delivery event bus.
- Implement Jira hook.
- Add idempotent retries.
- Auto-log configured delivery events.
- Stop relying on agents to remember progress calls manually.

### Acceptance

If Jira is configured and linked:

```text
delivery happens
  ↓
Jira receives configured updates automatically
```

---

## PR 7 — MCP Surface Reduction

### Changes

- Remove low-level and redundant MCP tools.
- Remove `find_tool`.
- Remove file editing tools.
- Remove Beads-specific tools.
- Collapse Jira tools around delivery linkage and configuration.

### Acceptance

Target <= 15 primary MCP tools.

---

## PR 8 — Beads / Legacy Knowledge Removal

Only after all new flows are stable.

### Changes

Delete:

```text
Beads domain
taskstore fallback
Dolt leftovers
legacy learning proposal subsystem
generic Punakawan knowledge ownership
obsolete role state
old workflow definition duplication
```

Provide migration utilities only where actual historical data needs preservation.

---

## PR 9 — Panel Cleanup

### Changes

Simplify navigation and make Delivery the main audit artifact.

Remove UI built around deleted concepts.

---

# 14. Package Deletion / Merge Map

Use this as the cleanup target, not necessarily one PR.

| Current | Target |
|---|---|
| `beads` | delete |
| `doltimport` | delete |
| `tasks` | plan/delivery step |
| `taskstore` | delete |
| `workflowdef` | merge into workflow |
| `recipe` | merge into workflow |
| `procident` | delete/merge workflow metadata |
| `procreg` | workflow registry |
| `learning` | Mom / workflow revision |
| `knowledge` storage | facade + legacy provider |
| `search` | provider concern / temporary compatibility |
| `contextrequest` | plan unresolved questions |
| `contradiction` | orchestrator reasoning |
| `dossier` | delivery context/evidence |
| `deliverysummary` | delivery read model |
| `taskcontext` | context builder |
| `workcontext` | context builder |
| `roleconfig` | agent policy |
| `roles` | branding/prompt helpers only |
| `syncqueue` | generic hook retry queue |
| `jiraworkflow` | Jira hook config |
| `worklogalloc` | Jira hook |
| `fileops` | delete |
| RTK core dependency | remove |
| `find_tool` | delete |

---

# 15. Code Quality Rules for This Refactor

## Do

- Prefer deleting a subsystem over wrapping it indefinitely.
- Preserve backward compatibility only where data loss or active callers justify it.
- Add migration shims with an explicit deletion path.
- Make IDs and revisions visible in delivery audit output.
- Keep provider interfaces narrow.
- Fail loudly when configured agent capabilities cannot be enforced.
- Let Git be the authority for worktree cleanliness.
- Let Mom be the authority for durable agent memory.
- Let Codepedia be the authority for software topology.

## Do not

- Add another generic metadata store.
- Add another task abstraction.
- Add another role lifecycle.
- Build a "knowledge sync engine" inside Punakawan.
- Reimplement Git.
- Reimplement shell/editor functions for coding agents.
- Add a scoring system for whether a plan is "clear".
- Keep `--force` cleanup as the happy path.
- Make every internal operation approval-gated.
- Keep dead packages purely to avoid deleting code.

---

# 16. Verification Checklist

Before merging each refactor PR:

```text
go test ./...
go vet ./...
```

Run any existing lint command from the repository.

For worktree changes add manual integration verification:

```bash
# start from a repo with local dirty changes
echo "local user change" >> some-unrelated-file

git status --porcelain

# run Punakawan delivery execution

# verify user's checkout still contains only the original dirty change
git status --porcelain

# verify Punakawan worktree exists outside repo during execution

# after successful delivery:
git worktree list --porcelain

# Punakawan execution worktree should be gone
# user checkout must not contain Punakawan-generated files
```

Test dirty execution worktree:

```bash
# inside Punakawan worktree
echo "uncommitted" >> file

# finish delivery
# expected: cleanup refuses
# expected: worktree remains recoverable
```

---

# 17. Definition of Done

The revamp is complete when all of these are true:

- Punakawan coordinates multiple projects in one delivery.
- User repositories remain free from Punakawan runtime files.
- Worktrees are central, isolated, safe, and cleaned after successful delivery.
- Dirty worktrees are never silently destroyed.
- Every implementation is tied to an immutable plan revision.
- Reusable workflows are versioned and invoke into plans.
- Punakawan no longer owns generic knowledge.
- Mom can provide experience/memory knowledge through a facade.
- Codepedia can provide software/system knowledge through the same facade.
- Bagong review is always an isolated general-purpose review execution.
- Implementation may use a cheaper forked worker when the plan step is executable.
- Jira delivery logging can happen automatically through configured hooks.
- Deliveries are readable and auditable without reconstructing session history.
- Beads/Dolt/task-tracker duplication is gone.
- MCP surface is small enough that `find_tool` is unnecessary.
- Punakawan is primarily:

```text
durable coordination state for agents
```

rather than:

```text
a collection of wrappers around every tool an agent might possibly use
```
