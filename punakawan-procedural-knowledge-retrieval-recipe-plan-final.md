# Punakawan Procedural Knowledge and Retrieval Recipe Implementation Plan

**Status:** Proposed  
**Assumptions:** Punakawan Panel v1 and New Plan 1 artifact review/mutation are already implemented  
**Roadmap position:** New Plan 2 of 2  
**Primary owner:** Punakawan core  
**Panel role:** Observation and explanation UI, not the execution engine  
**Initial provider:** Jira  
**Initial operation class:** Registered read-only retrieval operations  
**Canonical persistence:** Existing durable knowledge store, with immutable versions and evidence  
**New database:** None  
**Embedding dependency:** None  

---

## 1. Existing Foundation and Scope

This plan starts from an already working Panel v1 and the completed artifact review/mutation workflow from New Plan 1.

The remaining sequence is:

```text
Existing Panel v1
  + completed Artifact Review and Plan Mutation workflow
  → Procedural Knowledge Core
  → Jira Retrieval Recipe MVP
  → Recipe discovery and correction workflow
  → Panel recipe extension
```

The existing panel and review workflow provide the inspection and mutation surfaces needed to expose:

- The selected recipe and exact version.
- Why it matched the current task.
- Dynamic values resolved during execution.
- The compiled provider query.
- Validation samples and user acceptance.
- Execution evidence and result counts.
- Staleness, dispute, and supersession state.
- Corrections between versions.

The recipe engine must live in Punakawan core. The panel only reads and explains
its state.

### 1.1 Assumed Existing Panel and Review Capabilities

This plan assumes the following already work:

- Workspace registration and stable workspace identifiers.
- Durable knowledge browsing.
- Session summaries and timelines.
- Task and dependency views.
- Evidence indexing and safe preview.
- Approval visibility.
- SSE or equivalent live updates.
- Versioned API contracts.
- Source health and partial-failure handling.
- Secret redaction and filesystem boundaries.
- Authenticated local mutation sessions and CSRF protection.
- Anchored review comments and immutable review submissions.
- Automatic revision workflow dispatch.
- Proposal diff, validation, request-changes, rejection, and acceptance flows.

### 1.2 Required Core Prerequisites

Punakawan core must already provide:

- A capability registry for adapters and low-level tools.
- Jira read/search adapter support.
- Durable knowledge persistence with provenance and relations.
- Immutable or append-only evidence.
- Session checkpoints.
- BD task expansion.
- Existing exact, alias, BM25, fuzzy, and relationship-aware search.
- A policy engine capable of restricting operations to registered read-only capabilities.

### 1.3 Release Boundary

The first release supports only retrieval recipes that:

- Call a registered adapter capability.
- Perform a read-only operation.
- Produce typed output.
- Use a declarative selector.
- Can be validated against the provider.
- Do not execute arbitrary code.

Write operations, shell commands, Git mutations, Jira transitions, deployments,
and browser side effects remain outside this plan. Humanity has already invented
enough ways to turn a convenient shortcut into an incident report.

---

Punakawan knowledge must support more than durable facts. It must also support **verified, reusable procedures** for recurring retrieval tasks.

This capability uses the existing panel as its primary inspection surface and the completed review workflow as its mutation protocol.

A procedure such as “how to find Jira work items associated with this project” should be stored as a typed knowledge record called a **retrieval recipe**.

A retrieval recipe is:

- Declarative rather than arbitrary executable code.
- Bound to a registered adapter capability.
- Scoped to a workspace, repository, component, or organization.
- Parameterized where values change between executions.
- Tested against the real provider before becoming trusted.
- Versioned, traceable, and explicitly correctable.
- Reusable by later workflows without asking the same question again.
- Read-only by default.

This converts a user-taught lookup rule into durable operational knowledge without turning the knowledge store into a cupboard full of shell scripts.

## 2. Target Use Case

User request:

```text
Get all Jira work items for the next sprint for this project.
```

Expected flow:

```text
1. Normalize the request:
   capability: jira.issue.search
   intent: project.next-sprint.issues

2. Search knowledge for a verified retrieval recipe that:
   - applies to the current workspace;
   - uses the Jira adapter;
   - produces Jira work items;
   - supports a next-sprint selector;
   - is still valid for the connected Jira instance.

3. When a suitable recipe exists:
   - bind current parameters;
   - resolve the next sprint;
   - compile the provider query;
   - execute it;
   - return the work items;
   - record recipe usage and evidence.

4. When no suitable recipe exists:
   - enter guided discovery;
   - ask how Jira work is associated with the project;
   - compile the answer into a candidate recipe;
   - validate it against Jira;
   - show the query and representative results;
   - allow correction and retesting;
   - store it only after explicit acceptance.

5. Continue the original workflow using the accepted recipe.
```

Example user guidance:

```text
The Jira project key is TRF.

The item must either:
- have component AFFILIATE-PLATFORM; or
- contain the phrase AFFILIATE PLATFORM in its title.
```

Punakawan should store this first as a structured selector:

```yaml
provider: jira
resource: issue

scope:
  workspace_id: affiliate-platform

constraints:
  all:
    - field: project
      operator: equals
      value: TRF

    - any:
        - field: component
          operator: equals
          value: AFFILIATE-PLATFORM

        - field: summary
          operator: phrase_contains
          value: AFFILIATE PLATFORM

sprint:
  selector: next
```

The Jira adapter compiles that selector into JQL. A broad candidate might be:

```jql
project = "TRF"
AND sprint IN futureSprints()
AND (
  component = "AFFILIATE-PLATFORM"
  OR summary ~ "\"AFFILIATE PLATFORM\""
)
ORDER BY Rank ASC
```

However, `futureSprints()` may include more than one future sprint. For strict `next` semantics, Punakawan should:

```text
1. Resolve the relevant board.
2. List future sprints for that board.
3. Select the earliest valid sprint using board order or start date.
4. Bind its sprint ID.
5. Compile the final JQL with sprint = <resolved-sprint-id>.
```

When no unique board or sprint can be resolved, Punakawan asks the user to choose. It must not silently reinterpret “next sprint” as “all future sprints.”

## 3. Knowledge Type Taxonomy

Extend durable knowledge with typed procedural records:

```text
fact
requirement
decision
constraint
relationship
mapping
retrieval_recipe
validation_rule
workflow_hint
```

The first executable procedural type should be `retrieval_recipe` only.

A record is executable only when:

- its type explicitly supports execution;
- its capability exists in the capability registry;
- its schema validates;
- its provider adapter is available;
- its validity state permits use;
- required inputs can be resolved;
- policy permits the operation.

## 4. Retrieval Recipe Schema

```yaml
api_version: punakawan.dev/v1
kind: RetrievalRecipe

metadata:
  id: kr-jira-affiliate-platform-next-sprint
  title: Find Affiliate Platform Jira work for the next sprint
  aliases:
    - affiliate platform next sprint Jira
    - project Jira scope
    - TRF affiliate issues
  workspace_id: affiliate-platform
  status: verified
  version: 3
  created_at: 2026-07-23T11:20:00+07:00
  updated_at: 2026-07-23T11:41:00+07:00
  verified_at: 2026-07-23T11:41:00+07:00
  verified_by: user
  supersedes: kr-jira-affiliate-platform-next-sprint@2

spec:
  capability: jira.issue.search
  intent: project.next-sprint.issues
  provider: jira
  resource: issue
  operation: search
  read_only: true

  applies_to:
    workspace_ids:
      - affiliate-platform
    repository_ids:
      - affiliate-api
      - affiliate-ui

  inputs:
    - name: sprint_selector
      type: sprint_selector
      required: true
      default: next

  selector:
    all:
      - field: project
        operator: equals
        value:
          literal: TRF

      - any:
          - field: component
            operator: equals
            value:
              literal: AFFILIATE-PLATFORM

          - field: summary
            operator: phrase_contains
            value:
              literal: AFFILIATE PLATFORM

      - field: sprint
        operator: equals
        value:
          resolver: jira.next_sprint
          arguments:
            board:
              resolver: jira.board_for_project
              project_key: TRF

  ordering:
    - field: rank
      direction: ascending

  output:
    entity_type: jira_issue
    identity_field: key
    fields:
      - key
      - summary
      - status
      - assignee
      - priority
      - sprint
      - component

validation:
  status: passed
  validation_id: val-20260723-0041
  provider_instance_fingerprint: jira-cloud-company
  compiled_query_hash: sha256:...
  sample_size: 20
  accepted_result_count: 14
  accepted_by: user
  accepted_at: 2026-07-23T11:41:00+07:00

provenance:
  source: user_instruction
  session_id: run-20260723-001
  task_id: bd-a8f3
  evidence_ids:
    - ev-jql-compile-001
    - ev-jql-sample-001
    - ev-user-acceptance-001

relations:
  - type: applies_to
    target: workspace:affiliate-platform
  - type: uses_adapter
    target: adapter:jira
  - type: retrieves
    target: entity:jira_issue
  - type: validated_by
    target: evidence:ev-jql-sample-001
```

## 5. Selector and Query Compilation

The canonical recipe should store a structured selector. Compiled JQL is evidence or a cached projection.

Benefits:

- Values are escaped safely.
- Dynamic values such as the next sprint are resolved at execution time.
- Jira Cloud or Data Center differences stay inside the adapter compiler.
- Punakawan can explain each condition.
- Users can edit one rule without rewriting an opaque query.
- Provider changes can trigger recompilation.
- Validation can identify the failing clause.
- Recipe diffs remain meaningful.

```go
type QueryCompiler interface {
    Compile(
        ctx context.Context,
        selector Selector,
        bindings map[string]Value,
    ) (CompiledQuery, error)
}
```

The recipe engine must never concatenate unescaped user text directly into JQL.

## 6. Intent and Capability Matching

The planner turns the current task into a typed operation request:

```yaml
capability: jira.issue.search
intent: project.next-sprint.issues

context:
  workspace_id: affiliate-platform
  repository_ids:
    - affiliate-api

required_output: jira_issue[]
```

Candidate filtering order:

```text
1. Exact capability match.
2. Exact provider and resource match.
3. Compatible operation.
4. Exact workspace scope.
5. Repository or component scope.
6. Recipe validity state.
7. Required inputs are resolvable.
8. Provider adapter is available.
9. Recipe version is compatible.
10. Intent alias or BM25 relevance.
```

Suggested ranking:

| Signal | Weight |
|---|---:|
| Exact workspace scope | 100 |
| Exact capability | 100 |
| Exact intent | 80 |
| Exact repository scope | 40 |
| Verified state | 30 |
| Previous success in this workspace | 20 |
| Intent alias match | 15 |
| BM25 textual relevance | 0–15 |
| Stale warning | -30 |
| Disputed state | Exclude |
| Superseded or invalid state | Exclude |

BM25 helps when wording differs, but cannot override incompatible capability, scope, or validity.

## 7. Candidate Selection

- One clear verified candidate: reuse automatically and report the recipe ID/version.
- Multiple materially different candidates: show their scope, conditions, validation age, and why each matched.
- No valid candidate: enter guided discovery.
- Stale candidate: revalidate before reuse.
- Disputed, superseded, or invalid candidate: never execute automatically.

## 8. Guided Discovery and Correction Loop

```text
MISSING
  → COLLECTING_RULES
  → COMPILING
  → TESTING
  → PRESENTING_RESULTS
  → CORRECTING
  → RETESTING
  → ACCEPTED
  → STORED
  → EXECUTING_ORIGINAL_TASK
```

Possible exits:

```text
CANCELLED
REJECTED
UNRESOLVED
PROVIDER_UNAVAILABLE
POLICY_BLOCKED
```

The loop asks only for missing information and checkpoints accepted answers so an interrupted session can resume.

Example:

```text
Punakawan:
I do not yet know how this workspace maps to Jira.
How should I identify its Jira work items?

User:
Project TRF, with component AFFILIATE-PLATFORM or AFFILIATE PLATFORM in the title.

Punakawan:
I found two boards associated with TRF:
1. Transfer Platform
2. Affiliate Delivery

Which board defines “next sprint”?

User:
Affiliate Delivery.
```

## 9. Validation Pipeline

A recipe does not become `verified` just because Jira accepts the JQL.

```text
1. Schema validation.
2. Capability and adapter validation.
3. Field and operator validation.
4. Dynamic resolver validation.
5. Query compilation.
6. Provider dry run.
7. Result-count sanity check.
8. Representative result sampling.
9. Optional negative-example check.
10. Explicit user acceptance.
11. Durable evidence creation.
```

Validation presentation should include:

```text
Candidate rule
  Project: TRF
  Match any:
    Component equals AFFILIATE-PLATFORM
    Summary contains phrase AFFILIATE PLATFORM
  Sprint: next sprint from board Affiliate Delivery

Resolved sprint
  TRF Sprint 42
  Sprint ID: 1834
  Starts: 27 July 2026

Compiled JQL
  project = "TRF"
  AND sprint = 1834
  AND (
    component = "AFFILIATE-PLATFORM"
    OR summary ~ "\"AFFILIATE PLATFORM\""
  )
  ORDER BY Rank ASC

Results
  14 work items
  10 matched by component
  3 matched by title
  1 matched both conditions
```

Representative results:

| Key | Summary | Component | Match reason |
|---|---|---|---|
| TRF-1842 | Affiliate payout retry | AFFILIATE-PLATFORM | component |
| TRF-1851 | AFFILIATE PLATFORM dashboard audit | WEB | title phrase |

The user may accept, correct conditions, select a different board, exclude a result, identify a missing result, reject, or save as draft.

## 10. Corrections and Immutable Versioning

Corrections create immutable new versions.

Rules:

- Never overwrite a verified version in place.
- Preserve the previous compiled query and validation evidence.
- Store a human-readable diff.
- Link the new version with `supersedes`.
- Existing sessions keep referencing the version they used.
- Future automatic reuse selects only the latest valid version.

## 11. Explicit Update and Revalidation

Natural-language triggers:

```text
Update how you find Jira for this project.
The Jira mapping has changed.
Use another component from now on.
This recipe returned the wrong issues.
Revalidate the Jira lookup.
Forget the previous Jira rule.
```

CLI equivalents:

```bash
punakawan knowledge recipe list --workspace .
punakawan knowledge recipe show kr-jira-affiliate-platform-next-sprint
punakawan knowledge recipe explain kr-jira-affiliate-platform-next-sprint
punakawan knowledge recipe validate kr-jira-affiliate-platform-next-sprint
punakawan knowledge recipe update kr-jira-affiliate-platform-next-sprint
punakawan knowledge recipe dispute kr-jira-affiliate-platform-next-sprint
punakawan knowledge recipe supersede kr-jira-affiliate-platform-next-sprint
```

`update` starts guided discovery using the current recipe as the baseline.

`validate` recompiles and tests without changing the rule.

`dispute` prevents automatic use.

`supersede` points to an accepted replacement.

## 12. Staleness and Invalidation

A recipe may become stale when:

- A referenced field no longer exists.
- A Jira project, component, board, or sprint source is unavailable.
- Jira rejects the compiled query.
- The provider instance changes.
- The workspace-to-provider mapping changes.
- A required adapter capability changes version.
- The recipe repeatedly returns structurally invalid output.
- The user marks results as incorrect.
- Its configured revalidation period expires.

A zero-result query alone must not invalidate the recipe because a valid next sprint may genuinely contain no work items.

States:

```text
draft
validating
verified
stale
disputed
superseded
invalid
```

| State | Automatic reuse |
|---|---|
| Verified | Yes |
| Stale | Validate first |
| Draft | No |
| Validating | No |
| Disputed | No |
| Superseded | No |
| Invalid | No |

## 13. Execution Evidence

Each execution records:

```yaml
recipe_id: kr-jira-affiliate-platform-next-sprint
recipe_version: 3
session_id: run-20260723-004
task_id: bd-b9c2
executed_at: 2026-07-29T09:03:00+07:00
bindings:
  sprint_id: 1834
compiled_query_hash: sha256:...
result_count: 14
provider_request_id: optional
status: success
evidence_id: ev-recipe-execution-004
```

Do not store provider credentials, authentication headers, or secret field values.

## 14. Recipe Engine Boundaries

Initial execution is restricted to registered read operations.

Allowed examples:

- Search Jira work items.
- Retrieve Confluence pages.
- Locate API specifications.
- Find documentation associated with a service.
- Identify test reports for a repository.
- Resolve repository relationships.
- Find deployment manifests.
- Retrieve release notes.
- Select knowledge records using a project-specific rule.

Not executable initially:

- Arbitrary shell commands.
- Git push or merge.
- Jira create, edit, or transition.
- Confluence publish.
- File mutation.
- Deployment.
- Database writes.
- Browser actions with side effects.

A future `ActionRecipe` must use the approval engine and be a separate type, not a casual `read_only: false` flag dangling over a pit.

## 15. Core Interfaces

```go
type OperationRequest struct {
    Capability     string
    Intent         string
    WorkspaceID    string
    RepositoryIDs  []string
    RequiredOutput string
    Inputs         map[string]Value
}

type RecipeResolver interface {
    Resolve(ctx context.Context, request OperationRequest) (RecipeResolution, error)
}

type RecipeCompiler interface {
    Compile(
        ctx context.Context,
        recipe RetrievalRecipe,
        bindings map[string]Value,
    ) (CompiledOperation, error)
}

type RecipeValidator interface {
    Validate(ctx context.Context, candidate RetrievalRecipe) (ValidationReport, error)
}

type RecipeExecutor interface {
    Execute(ctx context.Context, operation CompiledOperation) (OperationResult, error)
}

type RecipeRepository interface {
    Search(ctx context.Context, query RecipeQuery) ([]RetrievalRecipe, error)
    CreateVersion(ctx context.Context, recipe RetrievalRecipe) (RetrievalRecipe, error)
    MarkState(ctx context.Context, recipeID string, state KnowledgeState, reason string) error
}
```

## 16. Workflow and BD Integration

```text
User objective
  → workflow planner
  → typed operation request
  → recipe resolver
      ├── verified recipe found
      │     → bind → compile → execute
      │
      ├── stale recipe found
      │     → revalidate → execute or discover
      │
      └── no valid recipe
            → guided discovery
            → validate
            → user accepts
            → store
            → execute
  → return result to workflow
```

The original task remains active during discovery. Knowledge creation is a prerequisite subtask, not a separate forgotten conversation.

Suggested BD expansion:

```text
Parent:
Retrieve Jira work items for the next sprint

Children:
1. Resolve Jira retrieval recipe
2. Discover project-to-Jira mapping, only when missing
3. Validate candidate query
4. Persist accepted recipe
5. Retrieve Jira work items
6. Attach retrieval evidence
```

When a verified recipe exists, tasks 2–4 are skipped. Task 1 records the reused recipe ID and version.

## 17. Panel Extension After Core Implementation

After the recipe core, Jira compiler, validation loop, and lifecycle commands are working, extend the already-completed Panel review workflow and knowledge page to expose `Retrieval Recipe` as a first-class type. Recipe edits must reuse the shared review request, proposal, diff, and acceptance protocol rather than introduce direct field mutation.

Recipe detail shows:

- Purpose and intent.
- Scope.
- Provider and adapter.
- Structured conditions.
- Dynamic resolvers.
- Last compiled query.
- Last resolved parameters.
- Validation state and evidence.
- Last successful use.
- Usage count and latest result count.
- Current and superseded versions.
- Staleness or dispute reason.
- Related sessions and tasks.

Session timeline example:

```text
09:03:01 Reused verified knowledge recipe
          kr-jira-affiliate-platform-next-sprint@3

09:03:02 Resolved next sprint
          TRF Sprint 42, ID 1834

09:03:02 Executed Jira search
          14 work items returned
```

The MVP panel remains read-only. Discovery, correction, acceptance, and supersession occur through the Punakawan conversation or CLI until panel mutation controls are deliberately added.

## 17A. Reused Review and Mutation Contract

Recipe review must reuse the complete New Plan 1 protocol:

```text
Open recipe version
  → add structured anchored comments
  → submit immutable review snapshot
  → dispatch revise_retrieval_recipe_from_review
  → generate complete proposed recipe version
  → compile and provider-test proposal
  → show selector, query, and result-set diff
  → accept, reject, or request another revision
  → create immutable canonical recipe version
```

Recipe-specific additions are limited to:

- Structured path anchors.
- Recipe schema validation.
- Selector compilation.
- Dynamic resolver validation.
- Provider dry run.
- Old-versus-new query comparison.
- Representative result-set comparison.
- Recipe lifecycle and staleness checks.

The implementation must not introduce:

- Direct field mutation.
- A second comment store.
- A second review state machine.
- A second acceptance API.
- Canonical writes before explicit acceptance.

---

## 18. Implementation Phases

### Phase 0: Recipe Contract Preparation

**Goal:** Establish stable recipe contracts without changing workflow behavior.

Tasks:

- Confirm the existing panel and review contracts that recipe views and mutations will reuse.
- Add `retrieval_recipe` to the knowledge taxonomy.
- Define capability, intent, selector, resolver, validation, execution, and version contracts.
- Define recipe lifecycle states.
- Add fixtures for valid, stale, disputed, superseded, and invalid recipes.
- Define evidence contracts for compilation, validation, acceptance, and execution.

Exit criteria:

- Schemas validate in Go and TypeScript.
- Recipe records can be stored and displayed as inert knowledge.
- No recipe can execute yet.

### Phase 1: Recipe Repository and Deterministic Resolution

**Goal:** Find a compatible recipe before asking the user for rules.

Tasks:

- Implement recipe repository interfaces.
- Filter by capability, provider, resource, operation, and scope.
- Exclude disputed, superseded, and invalid versions.
- Add deterministic ranking.
- Use alias and BM25 only after structural compatibility checks.
- Add ambiguity reporting.
- Record the selected recipe ID, version, and match explanation.

Exit criteria:

- A verified workspace-scoped recipe is resolved deterministically.
- Incompatible high-BM25 records cannot win.
- Multiple legitimate candidates produce an explicit ambiguity result.

### Phase 2: Jira Selector Compiler and Dynamic Resolvers

**Goal:** Turn a structured recipe into a safe Jira search.

Tasks:

- Implement the Jira selector AST compiler.
- Add safe value escaping and field/operator validation.
- Add Jira project and component validation.
- Resolve project-associated boards.
- Resolve a strict next sprint.
- Add an explicitly labeled `futureSprints()` approximation fallback.
- Compile ordering and requested output fields.
- Produce clause-level explanations.

Exit criteria:

- User-provided values are never concatenated directly into JQL.
- Strict `next` resolves to one sprint ID or asks for clarification.
- Compiled queries are reproducible and evidence-ready.

### Phase 3: Guided Discovery and Validation

**Goal:** Teach Punakawan a missing retrieval rule safely.

Tasks:

- Implement the resumable discovery state machine.
- Ask only for unresolved constraints.
- Compile a candidate recipe.
- Dry-run the provider query.
- Show resolved values, JQL, result count, representative results, and match reasons.
- Collect corrections, exclusions, and expected-but-missing examples.
- Retest until accepted, rejected, saved as draft, or cancelled.
- Store explicit acceptance evidence.

Exit criteria:

- The original task remains checkpointed during discovery.
- The user can correct and retest the query repeatedly.
- Only explicitly accepted candidates become verified.

### Phase 4: Workflow Reuse, Versioning, and Lifecycle

**Goal:** Reuse accepted recipes and keep corrections auditable.

Tasks:

- Add the `resolve_operation` workflow step.
- Expand missing-recipe discovery into BD subtasks.
- Skip discovery when a valid recipe exists.
- Execute the compiled operation.
- Record recipe ID, version, bindings, query hash, and result count.
- Add immutable version creation.
- Add update, validate, dispute, and supersede commands.
- Add stale-recipe revalidation.
- Resume the original task after discovery or revalidation.

Exit criteria:

- A later Jira request reuses the accepted recipe without asking the same questions.
- Corrections create a new version.
- Historical sessions retain the exact version they used.
- Stale and disputed recipes are never silently reused.

### Phase 5: Recipe Review, Mutation, and Panel Extension

**Goal:** Reuse New Plan 1's review protocol for recipe correction and expose recipe behavior through the already-completed panel.

Tasks:

- Enable `retrieval_recipe` as a reviewable artifact type.
- Add structured-path comment anchors for recipe fields and conditions.
- Add `revise_retrieval_recipe_from_review` using the existing workflow dispatcher.
- Add recipe list and detail contracts.
- Add capability, intent, scope, selector, and resolver views.
- Show compiled query and dynamic bindings.
- Show validation, acceptance, and execution evidence.
- Show usage history and latest result count.
- Show immutable version diffs.
- Require compiler and provider validation before proposal acceptance.
- Compare previous and proposed result samples before acceptance.
- Add timeline events for resolution, validation, reuse, and failure.
- Add stale, disputed, superseded, and invalid states.
- Preserve Panel v1's read-only default outside an authenticated review session.

Exit criteria:

- A user can explain why Punakawan selected and executed a recipe.
- The exact recipe version is visible from the related session.
- Panel failures do not affect recipe execution in core.

### Phase 6: Hardening and Generalization

**Goal:** Make the Jira MVP reliable and prepare additional read providers.

Tasks:

- Add provider-instance compatibility checks.
- Add rate-limit and transient-failure handling.
- Add schema-change detection.
- Add security and secret-redaction tests.
- Add performance and concurrency tests.
- Add recipe export/import through existing knowledge mechanisms.
- Document how a second provider can implement compiler, resolver, validator, and executor interfaces.
- Do not implement write recipes in this phase.

Exit criteria:

- Jira recipes survive normal provider and workspace changes safely.
- A second read-only provider can be added without modifying the recipe engine.
- Arbitrary code and write operations remain impossible.

## 19. Detailed Backlog

### 19.1 Contracts and Taxonomy

- `KNOW-RECIPE-001` Add `retrieval_recipe` to the knowledge taxonomy.
- `KNOW-RECIPE-002` Define `RetrievalRecipe` schema.
- `KNOW-RECIPE-003` Define capability and intent identifiers.
- `KNOW-RECIPE-004` Define selector AST contracts.
- `KNOW-RECIPE-005` Define compiled-operation contracts.
- `KNOW-RECIPE-006` Define validation-report contracts.
- `KNOW-RECIPE-007` Define immutable versioning and state transitions.

### 19.2 Resolution

- `KNOW-RECIPE-010` Implement capability and scope filtering.
- `KNOW-RECIPE-011` Add alias and BM25 intent fallback.
- `KNOW-RECIPE-012` Add deterministic candidate ranking.
- `KNOW-RECIPE-013` Add ambiguity handling.
- `KNOW-RECIPE-014` Add stale-recipe revalidation policy.
- `KNOW-RECIPE-015` Exclude disputed and superseded recipes.

### 19.3 Discovery

- `KNOW-RECIPE-020` Implement guided-discovery state machine.
- `KNOW-RECIPE-021` Persist discovery checkpoints.
- `KNOW-RECIPE-022` Add provider-aware missing-input prompts.
- `KNOW-RECIPE-023` Add candidate correction loop.
- `KNOW-RECIPE-024` Add accept, reject, and save-as-draft operations.
- `KNOW-RECIPE-025` Resume the original workflow after acceptance.

### 19.4 Jira Compiler and Resolvers

- `KNOW-RECIPE-030` Implement structured Jira selector compiler.
- `KNOW-RECIPE-031` Add safe JQL escaping.
- `KNOW-RECIPE-032` Add Jira project validation.
- `KNOW-RECIPE-033` Add Jira component validation.
- `KNOW-RECIPE-034` Add Jira board resolver.
- `KNOW-RECIPE-035` Add strict next-sprint resolver.
- `KNOW-RECIPE-036` Add `futureSprints()` fallback with approximation warning.
- `KNOW-RECIPE-037` Add representative result sampling and match reasons.

### 19.5 Validation and Evidence

- `KNOW-RECIPE-040` Add recipe schema validation.
- `KNOW-RECIPE-041` Add provider dry run.
- `KNOW-RECIPE-042` Add result-count sanity checks.
- `KNOW-RECIPE-043` Add positive and negative sample feedback.
- `KNOW-RECIPE-044` Store compiled-query and result-sample evidence.
- `KNOW-RECIPE-045` Record explicit user acceptance.
- `KNOW-RECIPE-046` Record execution evidence with recipe ID and version.

### 19.6 Lifecycle and Integration

- `KNOW-RECIPE-050` Add recipe list, show, and explain commands.
- `KNOW-RECIPE-051` Add validate and update commands.
- `KNOW-RECIPE-052` Add dispute and supersede commands.
- `KNOW-RECIPE-053` Add staleness detection.
- `KNOW-RECIPE-054` Add provider-instance fingerprinting without secrets.
- `KNOW-RECIPE-060` Add `resolve_operation` workflow step.
- `KNOW-RECIPE-061` Expand discovery into BD subtasks.
- `KNOW-RECIPE-062` Skip discovery when a verified recipe exists.
- `KNOW-RECIPE-063` Add recipe reuse events to session journals.
- `KNOW-RECIPE-064` Add panel recipe contracts and views.

## 20. Acceptance Criteria

- Punakawan checks for a compatible verified recipe before asking for lookup rules.
- Matching uses capability, intent, scope, provider, validity, and resolvable inputs.
- Missing knowledge triggers resumable guided discovery.
- User guidance is converted into a structured selector.
- Provider queries are compiled by adapters with safe escaping.
- Candidate queries are tested against the provider before acceptance.
- The user sees compiled query, resolved values, count, representative results, and match reasons.
- Only explicitly accepted candidates become verified reusable knowledge.
- Corrections create immutable new versions.
- The exact recipe version used by a session is recorded in evidence.
- Users can update, validate, dispute, or supersede a recipe explicitly.
- Stale recipes are revalidated before automatic reuse.
- Disputed, superseded, and invalid recipes are never automatically executed.
- Zero results alone do not invalidate a recipe.
- Recipes never store credentials or secret headers.
- Initial executable recipes are restricted to registered read-only adapter capabilities.
- The panel displays recipe definitions, versions, validation, usage, and evidence.

## 21. Definition of Done

The procedural knowledge and retrieval recipe capability is complete when:

```text
A user asks for recurring provider data,
Punakawan first searches for a compatible verified recipe,
reuses it when valid,
enters a resumable teaching loop when missing,
compiles and tests a declarative provider query,
shows representative results,
stores only an explicitly accepted immutable version,
resumes the original task,
and later explains every reuse through the completed panel.
```

The first release is considered successful only when the Jira next-sprint example
works end to end without embeddings, arbitrary executable scripts, duplicated
state, or silent reinterpretation of ambiguous sprint scope.
