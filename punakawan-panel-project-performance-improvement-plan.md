# Punakawan Panel Project-Centric Redesign and Performance Improvement Plan

**Status:** Proposed  
**Assumption:** Punakawan Panel v1 is already implemented  
**Repository:** `gdncomm-yunaz-ramadhan/punakawan`  
**Primary goals:**

1. Make the panel usable as a project-oriented control and review surface.
2. Reduce normal panel navigation from multi-second latency to near-instant cached reads.
3. Keep Dolt as the canonical versioned knowledge store without placing it on the page-rendering hot path.
4. Make project metadata, workflows, knowledge, tasks, and plans consistently scoped by project.
5. Allow safe user mutation through review, validation, immutable versions, and explicit acceptance.

---

## 1. Executive Summary

The current panel performs expensive live inspection during normal list and overview
requests. A single workspace description may open or connect to Dolt, read all
knowledge records, execute `bd list`, execute `bd ready`, read workflow history,
and run `git status` for every repository.

The same workspace listing is also executed by the background reconciler every
second. Parallelizing those probes reduces the sum of their latency, but it does
not make the architecture lightweight. It merely performs several expensive
operations at once.

The target architecture separates:

```text
Fast panel reads
  → registry
  → project metadata
  → cached project snapshots
  → immediate response

Expensive source inspection
  → background refresh
  → Dolt / Beads / Git / adapters
  → snapshot update
  → SSE notification
```

The primary UI entity becomes **Project**.

A workspace remains the physical directory that backs a project, but users should
interact with:

```text
Project
├── Metadata
├── Workflows
├── Knowledge
├── Tasks
├── Plans
├── Sessions
└── Health
```

---

## 2. Current-State Findings

### 2.1 Workspace list performs deep inspection

The current workspace reader loads and describes every registered workspace.

For each workspace, the description may perform:

- Knowledge store open or reuse.
- Full knowledge-record retrieval for a count.
- `bd list`.
- `bd ready`.
- Workflow history folding.
- `git status` for each repository.
- Adapter command checks.

Non-primary workspaces are loaded through a fresh `app.Load()` and closed after
each description.

This is inappropriate for:

```http
GET /api/v1/workspaces
GET /api/v1/overview
```

Those endpoints should return registry and snapshot information, not perform a
complete project diagnosis.

### 2.2 Reconciliation repeats expensive reads

The current reconciler polls every second and calls the workspace list reader.

This creates repeated:

- Filesystem scans.
- Beads processes.
- Git processes.
- Dolt connection checks.
- Workspace application loads.
- Competition with user-facing requests.

One-second reconciliation is reasonable only for lightweight session-state
inspection. It is not reasonable for full project health discovery.

### 2.3 Knowledge count loads all records

The overview currently derives knowledge count by loading every record and taking
the collection length.

The knowledge browser also loads all records and filters in Go.

This prevents:

- Cheap counts.
- SQL filtering.
- SQL pagination.
- Cursor-based loading.
- Predictable growth as the knowledge corpus expands.

### 2.4 Task views duplicate Beads work

The task reader runs both:

```text
bd list
bd ready
```

It then computes board status and blocker reasons.

Workspace summary performs similar work independently to calculate task counts.
Multiple pages therefore execute the same Beads commands repeatedly.

### 2.5 Project scope is incomplete

Several readers only support the primary workspace used to start the panel.

This prevents a registered project from consistently exposing:

- Knowledge.
- Tasks.
- Plans.
- Workflows.
- Sessions.
- Reviews.

The project list is global, while many detail sources remain primary-only.

### 2.6 Workflow definitions and workflow runs are conflated

The current workflow store persists execution checkpoints.

A mutable, invokable **workflow definition** is a different entity and requires
its own schema and store.

```text
Workflow definition
  describes what can be invoked

Workflow run
  records one execution of a workflow
```

---

## 3. Target Product Model

## 3.1 Project

A project is the user-facing aggregate.

```go
type Project struct {
    ID          string
    Name        string
    Description string
    Workspace   WorkspaceReference
    Metadata    []ProjectMetadataEntry
    Repositories []RepositoryReference
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

A project owns or scopes:

- Project metadata.
- Workflow definitions.
- Knowledge records.
- Tasks.
- Plans.
- Sessions.
- Reviews.
- Evidence.
- Health snapshots.

## 3.2 Workspace

A workspace is the physical local backing directory.

```go
type WorkspaceReference struct {
    Path      string
    Available bool
    LastSeen  time.Time
}
```

The workspace path is operational configuration. It should not be the primary
navigation concept.

## 3.3 Registered project registry

The global registry remains intentionally small.

```yaml
version: punakawan.project-registry/v1

projects:
  - id: affiliate-platform
    path: /workspace/affiliate-platform
    display_name: Affiliate Platform
    pinned: true
    registered_at: 2026-07-20T09:00:00Z
    last_seen_at: 2026-07-24T08:00:00Z

    snapshot:
      updated_at: 2026-07-24T08:04:12Z
      availability: available
      active_tasks: 12
      blocked_tasks: 2
      knowledge_count: 184
      active_runs: 1
      plan_count: 6
      workflow_count: 4
```

The `snapshot` is:

- Derived.
- Disposable.
- Cheap to read.
- Safe to regenerate.
- Not the canonical source for project entities.

---

## 4. Generic Project Metadata

Project metadata must be generic and self-explanatory.

The required entry shape is:

```yaml
- key: jira.project_key
  description: Jira project key used for work associated with this project.
  value: TRF
```

The core schema contains exactly:

```go
type ProjectMetadataEntry struct {
    Key         string
    Description string
    Value       any
}
```

### 4.1 Metadata rules

- `key` is required.
- `description` is required.
- `value` is required.
- Keys are unique within one project.
- Keys use lowercase dot-separated namespaces where practical.
- Keys are case-insensitive for uniqueness.
- Descriptions explain meaning, not merely repeat the key.
- Values may be:
  - string;
  - number;
  - boolean;
  - string list;
  - structured JSON/YAML value when genuinely required.
- Secret values are forbidden in normal metadata.
- Credentials remain in environment or secure configuration.
- Metadata entries are ordered for presentation but addressed by key.
- The agent receives only metadata relevant to the current capability or task.

### 4.2 Example project metadata

```yaml
version: punakawan.project/v1

id: affiliate-platform
name: Affiliate Platform
description: Affiliate acquisition, attribution, and payout platform.

metadata:
  - key: jira.project_key
    description: Jira project key used for this project.
    value: TRF

  - key: jira.board_id
    description: Jira board that defines the active and next sprint for this project.
    value: 127

  - key: team.owner
    description: Primary engineering team responsible for the project.
    value: AFFILIATE-PLATFORM

  - key: build.command
    description: Default command used to compile all project repositories.
    value: make build

  - key: test.command
    description: Default command used to run the main automated test suite.
    value: make test

  - key: architecture.style
    description: High-level architecture style used by the project.
    value: modular-services

  - key: compatibility.requirement
    description: Compatibility rule that plans and implementations must preserve.
    value: Public APIs must remain backward compatible unless explicitly approved.

  - key: documentation.locations
    description: Project locations that contain authoritative documentation.
    value:
      - docs/
      - README.md
      - .punakawan/knowledge/
```

### 4.3 Metadata API

```http
GET    /api/v1/projects/{projectId}/metadata
POST   /api/v1/projects/{projectId}/metadata
PATCH  /api/v1/projects/{projectId}/metadata/{key}
DELETE /api/v1/projects/{projectId}/metadata/{key}
```

Mutation behavior:

```text
User edits metadata
  → validate key, description, and value
  → create proposed project-metadata version
  → display diff
  → explicit acceptance
  → write immutable accepted version
  → publish project.metadata_changed
  → invalidate project context and summary caches
```

For simple metadata editing, the UI may combine proposal and acceptance in one
explicit confirmation dialog, provided:

- the previous version is retained;
- the diff is visible;
- optimistic locking is used;
- the change is auditable.

### 4.4 Metadata use by agents

Metadata must not be dumped wholesale into every prompt.

Introduce a context selector:

```go
type MetadataSelector interface {
    Select(
        project Project,
        capability string,
        intent string,
        requestedKeys []string,
    ) []ProjectMetadataEntry
}
```

Selection priority:

1. Explicitly requested metadata keys.
2. Exact capability namespace.
3. Exact intent mapping.
4. Workflow-declared metadata dependencies.
5. General project context with a strict limit.

Example:

```yaml
workflow:
  required_metadata:
    - jira.project_key
    - jira.board_id
```

The agent receives:

```text
jira.project_key
  Jira project key used for this project.
  Value: TRF

jira.board_id
  Jira board that defines the active and next sprint for this project.
  Value: 127
```

This keeps metadata generic while remaining understandable to both humans and
agents.

---

## 5. Project-Centric Panel Navigation

Recommended navigation:

```text
Overview
Projects
  └── Project Detail
      ├── Summary
      ├── Metadata
      ├── Workflows
      ├── Knowledge
      ├── Tasks
      ├── Plans
      ├── Sessions
      └── Health
Global Search
System
```

### 5.1 Overview

The global overview should show cached aggregates only:

- Registered projects.
- Available projects.
- Active workflow runs.
- Blocked tasks.
- Plans awaiting review.
- Knowledge changes.
- Pending approvals.
- Recent project activity.

The global overview must not open every project runtime.

### 5.2 Project detail

Project detail uses project-scoped routes:

```text
/projects/{projectId}
/projects/{projectId}/metadata
/projects/{projectId}/workflows
/projects/{projectId}/knowledge
/projects/{projectId}/tasks
/projects/{projectId}/plans
/projects/{projectId}/sessions
/projects/{projectId}/health
```

---

## 6. Project Workflows

## 6.1 Workflow definition

A workflow is a mutable, invokable subset of registered actions or tool calls.

Recommended location:

```text
.punakawan/workflows/
├── feature-delivery.yaml
├── jira-next-sprint.yaml
├── review-pr.yaml
└── fix-pr-review.yaml
```

Schema:

```yaml
version: punakawan.workflow/v1

id: jira-next-sprint
name: Retrieve next sprint work
description: Retrieve Jira issues associated with the project's next sprint.
enabled: true

required_metadata:
  - jira.project_key
  - jira.board_id

inputs:
  - name: include_subtasks
    type: boolean
    required: false
    default: false

steps:
  - id: resolve-recipe
    capability: knowledge.resolve_recipe
    intent: project.next-sprint.issues

  - id: retrieve-issues
    capability: jira.issue.search
    input_from:
      - resolve-recipe

allowed_capabilities:
  - knowledge.search
  - knowledge.resolve_recipe
  - jira.board.read
  - jira.sprint.read
  - jira.issue.search

approval:
  required_for:
    - external_write

output:
  type: jira_issue_list
```

### 6.2 Workflow constraints

- Every step references a registered capability.
- Arbitrary command strings are not accepted as workflow actions.
- Workflow definitions are immutable by version.
- Changes use the existing review-and-proposal mechanism.
- The panel displays:
  - current version;
  - steps;
  - required metadata;
  - allowed capabilities;
  - validation result;
  - usage history;
  - related plans and tasks.
- Invoking a definition creates a workflow run in the existing run/checkpoint
  store.

### 6.3 Workflow API

```http
GET    /api/v1/projects/{projectId}/workflows
POST   /api/v1/projects/{projectId}/workflows
GET    /api/v1/projects/{projectId}/workflows/{workflowId}
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/reviews
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/invoke
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/enable
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/disable
```

---

## 7. Project Knowledge

Knowledge is:

- Project-scoped.
- Mutable through immutable versions.
- Searchable.
- Related to plans, workflows, tasks, repositories, and external entities.

### 7.1 Knowledge API

```http
GET    /api/v1/projects/{projectId}/knowledge
POST   /api/v1/projects/{projectId}/knowledge
GET    /api/v1/projects/{projectId}/knowledge/{knowledgeId}
POST   /api/v1/projects/{projectId}/knowledge/{knowledgeId}/reviews
POST   /api/v1/projects/{projectId}/knowledge/{knowledgeId}/dispute
POST   /api/v1/projects/{projectId}/knowledge/{knowledgeId}/supersede
GET    /api/v1/projects/{projectId}/knowledge/{knowledgeId}/history
GET    /api/v1/projects/{projectId}/knowledge/{knowledgeId}/relations
```

### 7.2 Knowledge mutation

```text
Current version
  + review comments or structured edits
  → proposal
  → schema and relation validation
  → diff
  → explicit acceptance
  → new immutable knowledge version
  → incremental search-index update
  → cache invalidation
```

### 7.3 Dolt role

Keep Dolt as the canonical knowledge store because it provides:

- SQL querying.
- Durable local state.
- Version-aware synchronization.
- Relation tables.
- Auditable knowledge changes.

Dolt must not be used synchronously for project-list rendering.

---

## 8. Project Tasks

Tasks remain backed by Beads.

The panel should expose a reusable task snapshot:

```go
type ProjectTaskSnapshot struct {
    ProjectID       string
    UpdatedAt       time.Time
    Tasks           []TaskSummary
    DependencyGraph TaskGraph
    OpenCount       int
    ReadyCount      int
    ActiveCount     int
    BlockedCount    int
    CompletedCount  int
}
```

One snapshot refresh performs:

```text
bd list
bd ready
derive status
derive blocker reasons
derive counts
derive dependency graph
```

Every panel consumer reuses the snapshot:

- Project summary.
- Global overview.
- Task board.
- Task table.
- Dependency graph.
- Needs-attention cards.

Do not execute independent Beads processes per component or endpoint.

---

## 9. Project Plans

Plans are project-scoped and derived from knowledge, requirements, sessions, or
workflow output.

Recommended storage:

```text
.punakawan/plans/
└── <plan-id>/
    ├── manifest.yaml
    ├── current.yaml
    ├── versions/
    │   ├── 1.md
    │   └── 2.md
    ├── reviews/
    └── evidence/
```

Manifest:

```yaml
version: punakawan.plan-manifest/v1

id: panel-performance-redesign
title: Panel project and performance redesign
status: accepted
current_version: 2

derived_from:
  knowledge:
    - pkw:decision/panel/project-model
    - pkw:constraint/panel/local-only
  workflows:
    - panel-improvement
  metadata:
    - build.command
    - compatibility.requirement

related_tasks:
  - punakawan-panel-cache
  - punakawan-project-runtime
```

Plans can be:

- Created.
- Reviewed.
- Commented.
- Revised.
- Accepted.
- Rejected.
- Superseded.

The current artifact review protocol should be resolved through the selected
project rather than a single startup workspace.

---

## 10. Performance Architecture

## 10.1 Fast read model

Introduce a process-local read model.

```go
type ProjectSnapshot struct {
    ProjectID       string
    UpdatedAt       time.Time
    Availability    string
    RepositoryCount int
    KnowledgeCount  int
    WorkflowCount   int
    PlanCount       int
    ActiveRunCount  int
    OpenTaskCount   int
    BlockedTaskCount int
    PendingReviewCount int
    SourceHealth    []SourceHealthSummary
}
```

The project registry may persist the last accepted snapshot for fast startup.

The in-memory snapshot is authoritative only for presentation, never for
canonical writes.

## 10.2 Stale-while-revalidate

Every summary endpoint follows:

```text
1. Read cached snapshot.
2. Return immediately.
3. If snapshot is stale, trigger one background refresh.
4. Deduplicate concurrent refreshes.
5. Update snapshot.
6. Publish SSE event.
7. UI refreshes affected cards.
```

Suggested TTLs:

| Data | Refresh strategy |
|---|---|
| Project registry | File event or explicit mutation |
| Project metadata | File event or explicit mutation |
| Project summary | 5–15 seconds |
| Tasks while active | 2–5 seconds |
| Tasks while idle | 30 seconds |
| Knowledge count | Knowledge-event driven or 30 seconds |
| Plan count | Plan-event driven |
| Workflow count | Workflow-definition event driven |
| Git status | 15–30 seconds |
| Adapter executable check | 5 minutes |
| Deep source health | 30–60 seconds or explicit refresh |
| Session progress | Event-driven, 1-second lightweight fallback |

## 10.3 Project runtime manager

Avoid `app.Load()` and `app.Close()` per request.

```go
type ProjectRuntimeManager interface {
    Acquire(ctx context.Context, projectID string) (*ProjectRuntime, error)
    Release(projectID string)
    Invalidate(projectID string)
    CloseIdle(ctx context.Context) error
}

type ProjectRuntime struct {
    App        *app.App
    Metadata   Project
    Snapshot   atomic.Pointer[ProjectSnapshot]
    LastUsedAt atomic.Int64
}
```

Behavior:

- Project list does not acquire runtimes.
- Project detail may acquire a runtime.
- Active runtimes are reused.
- Dolt stores and search indexes remain memoized per runtime.
- Apply an LRU or bounded pool.
- Suggested active-runtime cap: 4–8.
- Suggested idle close timeout: 10–15 minutes.
- Never open all project databases merely to count registered projects.

## 10.4 Reconciliation tiers

Replace one universal one-second loop.

### Tier 1: lightweight live state

Frequency:

```text
event-driven
or 1 second fallback
```

Sources:

- Session summary files.
- Approval journal tail.
- Workflow-run checkpoint modification time.

### Tier 2: project source summaries

Frequency:

```text
10–30 seconds
or change-driven
```

Sources:

- Task snapshot.
- Knowledge event watermark.
- Plan/workflow manifests.
- Git status.

### Tier 3: deep diagnostics

Frequency:

```text
30–60 seconds
or explicit user refresh
```

Sources:

- Full source health.
- Adapter connectivity.
- Repository diagnostics.
- Cross-process Dolt availability.

The one-second reconciler must not call full project listing or deep workspace
description.

---

## 11. Dolt Performance Improvements

## 11.1 Keep Dolt off the overview hot path

Overview and project list must not call:

```go
OpenKnowledge()
AllWithUpdatedAt()
```

They use the project snapshot.

## 11.2 Add lightweight count query

```go
func (s *Store) Count(ctx context.Context) (int, error)
```

SQL:

```sql
SELECT COUNT(*)
FROM knowledge_records;
```

## 11.3 Add filtered cursor list

```go
type KnowledgeListQuery struct {
    Type          string
    Status        string
    ValidityState string
    Repository    string
    Source        string
    Limit         int
    Cursor        string
}
```

SQL filtering and pagination occur before records are decoded.

Example:

```sql
SELECT id, type, status, validity_state, data, updated_at
FROM knowledge_records
WHERE (? = '' OR type = ?)
  AND (? = '' OR status = ?)
  AND (? = '' OR validity_state = ?)
  AND (
    ? = ''
    OR JSON_UNQUOTE(JSON_EXTRACT(data, '$.scope.repository')) = ?
  )
ORDER BY updated_at DESC, id ASC
LIMIT ?;
```

## 11.4 Add database indexes

```sql
CREATE INDEX idx_knowledge_updated_at
ON knowledge_records(updated_at);

CREATE INDEX idx_knowledge_type_updated_at
ON knowledge_records(type, updated_at);

CREATE INDEX idx_knowledge_status_updated_at
ON knowledge_records(status, updated_at);

CREATE INDEX idx_knowledge_validity_updated_at
ON knowledge_records(validity_state, updated_at);
```

If repository and source filtering become frequent, normalize them into columns
rather than relying indefinitely on JSON extraction.

## 11.5 Avoid repeated migration work

Schema migration should be version-gated.

Add:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(64) PRIMARY KEY,
  applied_at DATETIME NOT NULL
);
```

Do not execute every migration statement on every reused-store open when the
schema version is already current.

## 11.6 Background warm-up

When a user opens a project detail page:

```text
return cached project snapshot
  → acquire runtime in background
  → connect to existing Dolt server or start once
  → refresh knowledge summary
  → publish project.snapshot_updated
```

The page must remain usable while Dolt warms.

---

## 12. Beads Performance Improvements

Introduce:

```go
type TaskSnapshotService interface {
    Get(projectID string) (*ProjectTaskSnapshot, bool)
    Refresh(ctx context.Context, projectID string) (*ProjectTaskSnapshot, error)
    Invalidate(projectID string)
}
```

Requirements:

- Deduplicate refreshes with `singleflight`.
- Cache both issue list and ready set.
- Calculate dependency graph once.
- Reuse counts everywhere.
- Detect Beads data modification time before executing commands.
- Refresh after Punakawan performs task mutations.
- Allow explicit UI refresh.
- Keep the old snapshot when refresh fails and mark it stale.

---

## 13. Git and Adapter Health

## 13.1 Git

- Cache `git status` per repository.
- Refresh at a 15–30 second TTL.
- Refresh when the user opens Health.
- Run repository statuses concurrently with a small limit.
- Ignore generated directories through normal Git configuration.
- Preserve the last successful result if a refresh times out.

## 13.2 Adapters

Separate:

```text
Configured
Executable found
Process starts
Handshake succeeds
Remote provider reachable
```

Project summary should use only cheap cached states.

Deep live checks belong on Health or explicit refresh.

---

## 14. API Redesign

Introduce project routes:

```http
GET /api/v1/projects
GET /api/v1/projects/{projectId}
GET /api/v1/projects/{projectId}/snapshot

GET    /api/v1/projects/{projectId}/metadata
POST   /api/v1/projects/{projectId}/metadata
PATCH  /api/v1/projects/{projectId}/metadata/{key}
DELETE /api/v1/projects/{projectId}/metadata/{key}

GET  /api/v1/projects/{projectId}/health
POST /api/v1/projects/{projectId}/health/refresh

GET    /api/v1/projects/{projectId}/workflows
POST   /api/v1/projects/{projectId}/workflows
GET    /api/v1/projects/{projectId}/workflows/{workflowId}
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/reviews
POST   /api/v1/projects/{projectId}/workflows/{workflowId}/invoke

GET    /api/v1/projects/{projectId}/knowledge
POST   /api/v1/projects/{projectId}/knowledge
GET    /api/v1/projects/{projectId}/knowledge/{knowledgeId}
POST   /api/v1/projects/{projectId}/knowledge/{knowledgeId}/reviews

GET /api/v1/projects/{projectId}/tasks
GET /api/v1/projects/{projectId}/tasks/{taskId}
GET /api/v1/projects/{projectId}/task-graph

GET  /api/v1/projects/{projectId}/plans
POST /api/v1/projects/{projectId}/plans
GET  /api/v1/projects/{projectId}/plans/{planId}
POST /api/v1/projects/{projectId}/plans/{planId}/reviews

GET /api/v1/projects/{projectId}/sessions
GET /api/v1/projects/{projectId}/events
```

Compatibility:

```text
/api/v1/workspaces
```

may remain as a deprecated alias during migration.

---

## 15. Mutation and Versioning

All mutable project entities use the same principles:

```text
Current immutable version
  → user edit or review
  → validation
  → proposed version
  → diff
  → explicit acceptance
  → next immutable version
```

Entities:

- Project metadata.
- Workflow definitions.
- Knowledge.
- Plans.

Simple project metadata edits may use a compact confirmation UI, but must still
retain:

- old version;
- new version;
- actor;
- timestamp;
- diff;
- optimistic base revision.

---

## 16. Cache Invalidation Events

Add events:

```text
project.registered
project.updated
project.metadata_changed
project.snapshot_updated
project.health_changed

workflow.definition_created
workflow.definition_changed
workflow.definition_enabled
workflow.definition_disabled
workflow.run_started
workflow.run_changed

knowledge.created
knowledge.changed
knowledge.disputed
knowledge.superseded

task.snapshot_updated
task.changed

plan.created
plan.changed
plan.review_submitted
plan.proposal_ready
plan.accepted
```

Handlers invalidate only relevant caches.

Example:

```text
knowledge.changed
  → invalidate project knowledge count
  → update Bleve incrementally
  → refresh project snapshot
  → publish snapshot event
```

---

## 17. Observability and Performance Measurement

Current request-level duration is insufficient.

Instrument:

```text
panel.project_registry.list
panel.project_snapshot.get
panel.project_runtime.acquire
panel.project_runtime.load
panel.project_metadata.read
panel.knowledge.open
panel.knowledge.count
panel.knowledge.list
panel.beads.list
panel.beads.ready
panel.task_snapshot.refresh
panel.workflow.current
panel.workflow_definition.list
panel.plan.list
panel.git.status
panel.overview.aggregate
```

Add development-mode `Server-Timing` headers.

Example:

```http
Server-Timing:
  registry;dur=2,
  snapshot;dur=1,
  aggregate;dur=3
```

Deep refresh example:

```http
Server-Timing:
  runtime_load;dur=24,
  dolt_connect;dur=720,
  knowledge_count;dur=12,
  beads_list;dur=510,
  beads_ready;dur=320,
  git_status;dur=170
```

Also record:

- Cache hit ratio.
- Refresh duration.
- Refresh failure count.
- Active project runtimes.
- Dolt process count.
- Beads process count.
- SSE update delay.
- Response payload sizes.

---

## 18. Performance Targets

### Warm reads

| Operation | Target |
|---|---:|
| Global overview | `< 100 ms` |
| Project list | `< 50 ms` |
| Project summary | `< 100 ms` |
| Metadata page | `< 100 ms` |
| Workflow list | `< 150 ms` |
| Plan list | `< 150 ms` |
| Cached task page | `< 150 ms` |
| Cached knowledge first page | `< 200 ms` |

### Cold/background operations

| Operation | Target behavior |
|---|---|
| Dolt startup | May take seconds, never blocks initial project page |
| Task refresh | Background; old snapshot remains usable |
| Git health | Background; cached result shown |
| Deep health | Explicit or background |
| Search index warm-up | Background with progress state |

### Resource goals

- Idle panel should not spawn Beads or Git processes every second.
- One Dolt process maximum per active project data directory.
- Project list should not start Dolt.
- Base panel remains usable with no active Dolt process.
- Background refresh concurrency is bounded.
- Closing the panel terminates owned idle processes cleanly.

---

## 19. Implementation Phases

## Phase 0: Instrument and Confirm Baseline

Tasks:

- Add sub-probe timings.
- Add `Server-Timing`.
- Record current `/overview`, `/workspaces`, and project-detail timings.
- Count processes spawned during one minute of idle panel operation.
- Confirm whether the rebuilt binary includes the latest embedded panel assets.
- Add benchmark fixtures for:
  - 1 project;
  - 5 projects;
  - 20 projects;
  - large knowledge corpus;
  - large Beads graph.

Exit criteria:

- Every major latency source is measurable.
- Baseline numbers are stored in a performance report.

## Phase 1: Remove Deep Work From Hot Paths

Tasks:

- Introduce `ProjectSnapshot`.
- Make project list registry-and-snapshot only.
- Make global overview snapshot-only.
- Remove full workspace listing from the one-second reconciler.
- Split lightweight live reconciliation from source-health refresh.
- Add stale-while-revalidate.
- Add `singleflight` refresh deduplication.

Exit criteria:

- Project list no longer opens Dolt.
- Project list no longer runs Beads.
- Project list no longer runs Git.
- Overview warm latency is below 100 ms.

## Phase 2: Project Domain and Generic Metadata

Tasks:

- Introduce `Project` contracts.
- Migrate registry naming from workspace to project while preserving compatibility.
- Add `.punakawan/project.yaml`.
- Implement generic `key`, `description`, `value` metadata.
- Add metadata validation.
- Add metadata versioning and audit events.
- Add project navigation and metadata UI.
- Add metadata context selector for agent workflows.

Exit criteria:

- User can register and open a project.
- User can add, edit, and remove generic metadata.
- Agent can request relevant metadata by key.
- Metadata changes preserve history.

## Phase 3: Project Runtime Manager

Tasks:

- Add bounded runtime manager.
- Reuse loaded apps.
- Reuse Dolt stores.
- Reuse search indexes.
- Add idle runtime closing.
- Add project-aware readers.
- Remove primary-workspace-only restrictions.
- Add project-scoped store resolvers for plans and reviews.

Exit criteria:

- Any registered project can expose knowledge, tasks, plans, and sessions.
- Repeated project navigation does not reload the app.
- Active runtimes stay within configured limits.

## Phase 4: Knowledge Query Optimization

Tasks:

- Add `Count`.
- Add filtered cursor listing.
- Add indexes.
- Add schema migration versioning.
- Add incremental Bleve updates.
- Make knowledge page project-scoped.
- Keep old snapshot while Dolt warms or refresh fails.

Exit criteria:

- Knowledge count does not load records.
- First page does not read the full corpus.
- Typical filtered browse is below 200 ms when warm.

## Phase 5: Task Snapshot Service

Tasks:

- Add reusable task snapshot.
- Deduplicate `bd list` and `bd ready`.
- Reuse derived counts and graph.
- Add modification-time invalidation.
- Add explicit refresh.
- Add stale snapshot status.

Exit criteria:

- Overview, task board, table, and graph share one refresh.
- Idle panel does not repeatedly execute Beads commands.

## Phase 6: Workflow Definitions

Tasks:

- Add workflow-definition schema and store.
- Separate definitions from run checkpoints.
- Add list/detail/review/version APIs.
- Add registered-capability validation.
- Add required metadata declarations.
- Add invocation and workflow-run linkage.
- Add project workflow UI.

Exit criteria:

- User can review and mutate a project workflow.
- Accepted workflow can be invoked.
- Arbitrary executable commands are rejected.

## Phase 7: Project Knowledge and Plan Mutation

Tasks:

- Add project-scoped knowledge mutations.
- Add knowledge proposal and acceptance.
- Add project-scoped plan stores.
- Add plan list and derivation relationships.
- Reuse review, revision, and proposal flows.
- Add cache invalidation events.

Exit criteria:

- Knowledge and plans are reviewable and mutable per project.
- Exact versions remain traceable from sessions and tasks.

## Phase 8: Health, Hardening, and Migration

Tasks:

- Add health refresh endpoint.
- Cache Git and adapter states.
- Add project registry migration.
- Add compatibility aliases.
- Add concurrency and process-lifecycle tests.
- Add corrupted project handling.
- Add load and idle-resource tests.
- Update documentation.

Exit criteria:

- A broken project does not slow or break the global panel.
- Old workspace registry data migrates safely.
- Idle resource use remains low.

---

## 20. Detailed Backlog

### Performance Foundation

- `PANEL-PERF-001` Add per-source timing instrumentation.
- `PANEL-PERF-002` Add development `Server-Timing`.
- `PANEL-PERF-003` Add performance fixtures.
- `PANEL-PERF-004` Add process-spawn counters.
- `PANEL-PERF-005` Add project snapshot contract.
- `PANEL-PERF-006` Add snapshot store and in-memory cache.
- `PANEL-PERF-007` Add stale-while-revalidate.
- `PANEL-PERF-008` Add refresh `singleflight`.
- `PANEL-PERF-009` Remove deep workspace list from one-second reconciliation.
- `PANEL-PERF-010` Add tiered reconciliation.

### Project Model

- `PROJECT-001` Define project schema.
- `PROJECT-002` Add project registry compatibility layer.
- `PROJECT-003` Add `.punakawan/project.yaml`.
- `PROJECT-004` Add project list/detail APIs.
- `PROJECT-005` Add project navigation.
- `PROJECT-006` Add project event types.

### Generic Metadata

- `PROJECT-META-001` Define `key`, `description`, `value` schema.
- `PROJECT-META-002` Add unique-key validation.
- `PROJECT-META-003` Add scalar/list/structured value validation.
- `PROJECT-META-004` Add metadata read API.
- `PROJECT-META-005` Add metadata create/update/delete API.
- `PROJECT-META-006` Add metadata diff and confirmation.
- `PROJECT-META-007` Add immutable metadata versions.
- `PROJECT-META-008` Add metadata audit events.
- `PROJECT-META-009` Add metadata UI.
- `PROJECT-META-010` Add metadata context selector.

### Runtime Management

- `PROJECT-RUNTIME-001` Add runtime manager.
- `PROJECT-RUNTIME-002` Add bounded active-runtime pool.
- `PROJECT-RUNTIME-003` Add idle runtime cleanup.
- `PROJECT-RUNTIME-004` Add project-aware reader resolver.
- `PROJECT-RUNTIME-005` Add project-scoped artifact-store resolver.
- `PROJECT-RUNTIME-006` Add runtime health metrics.

### Knowledge

- `KNOW-PERF-001` Add `Store.Count`.
- `KNOW-PERF-002` Add cursor list query.
- `KNOW-PERF-003` Add SQL filtering.
- `KNOW-PERF-004` Add knowledge indexes.
- `KNOW-PERF-005` Add migration version table.
- `KNOW-PERF-006` Add incremental search-index update.
- `KNOW-PERF-007` Add project-scoped knowledge reader.
- `KNOW-PERF-008` Add mutable knowledge review flow.

### Tasks

- `TASK-SNAPSHOT-001` Define task snapshot.
- `TASK-SNAPSHOT-002` Add snapshot refresh.
- `TASK-SNAPSHOT-003` Deduplicate Beads commands.
- `TASK-SNAPSHOT-004` Reuse counts and graph.
- `TASK-SNAPSHOT-005` Add modification-time invalidation.
- `TASK-SNAPSHOT-006` Add stale state and explicit refresh.
- `TASK-SNAPSHOT-007` Add snapshot SSE events.

### Workflow Definitions

- `WORKFLOW-DEF-001` Define workflow-definition schema.
- `WORKFLOW-DEF-002` Add workflow-definition store.
- `WORKFLOW-DEF-003` Add capability validation.
- `WORKFLOW-DEF-004` Add required metadata declaration.
- `WORKFLOW-DEF-005` Add workflow review/versioning.
- `WORKFLOW-DEF-006` Add invocation.
- `WORKFLOW-DEF-007` Link invocation to workflow runs.
- `WORKFLOW-DEF-008` Add workflow UI.

### Plans

- `PLAN-PROJECT-001` Add project-scoped plan resolver.
- `PLAN-PROJECT-002` Add plan manifest.
- `PLAN-PROJECT-003` Add plan list/detail APIs.
- `PLAN-PROJECT-004` Add knowledge and workflow derivation links.
- `PLAN-PROJECT-005` Reuse plan review/proposal flows.
- `PLAN-PROJECT-006` Add plan snapshot counters.

### Health and Compatibility

- `PANEL-HEALTH-001` Add cached project health.
- `PANEL-HEALTH-002` Add explicit deep refresh.
- `PANEL-HEALTH-003` Add cached Git status.
- `PANEL-HEALTH-004` Add layered adapter health.
- `PANEL-MIGRATE-001` Add workspace-to-project registry migration.
- `PANEL-MIGRATE-002` Add deprecated workspace API aliases.
- `PANEL-MIGRATE-003` Add migration documentation.

---

## 21. Acceptance Criteria

### Performance

- Project list does not open Dolt.
- Project list does not execute Beads.
- Project list does not execute Git.
- One-second reconciliation does not perform full project inspection.
- Warm project list completes below 50 ms.
- Warm overview completes below 100 ms.
- Cold Dolt startup does not block initial project-page rendering.
- Concurrent refresh requests produce one source refresh.
- Idle panel process spawning is near zero.

### Projects

- Registered workspaces appear as projects.
- Each project has editable name and description.
- Each project exposes metadata, workflows, knowledge, tasks, plans, sessions,
  and health.
- A missing or broken project does not break global navigation.

### Metadata

- Metadata supports `key`, `description`, and `value`.
- Metadata keys are unique per project.
- Values support common primitive and list forms.
- Descriptions are shown in the UI and supplied to the agent with values.
- Metadata history is preserved.
- Secrets are rejected or redirected to secure configuration.
- Agent context receives only relevant metadata.

### Workflows

- Workflow definitions are separate from workflow runs.
- Workflows contain registered capability references.
- User can review and mutate workflows.
- Accepted workflows can be invoked.
- Invalid capability references block acceptance.

### Knowledge

- Knowledge is project-scoped.
- Knowledge browsing uses SQL pagination.
- Knowledge counts use a count query.
- Knowledge can be reviewed and mutated.
- Search index updates do not require a complete rebuild for each mutation.

### Tasks

- Task counts, board, table, and graph reuse one snapshot.
- Beads commands are not duplicated across components.
- Stale task snapshots remain visible during refresh failures.

### Plans

- Plans are project-scoped.
- Plans reference source knowledge, workflows, and metadata.
- Plans can be reviewed and mutated.
- Accepted versions remain immutable and traceable.

---

## 22. Definition of Done

```text
The Punakawan panel opens quickly from cached project snapshots.

The project list and global overview never perform synchronous Dolt,
Beads, Git, or adapter diagnostics.

Each registered project exposes generic key-description-value metadata,
reviewable workflow definitions, mutable knowledge, task status,
and reviewable plans.

Expensive source refreshes happen in the background, are deduplicated,
update snapshots, and notify the UI through events.

Dolt remains the canonical versioned knowledge store, but Dolt startup
or full-record queries never block normal panel navigation.

Warm project navigation feels immediate, idle resource use remains low,
and every accepted mutation remains versioned, validated, and auditable.
```
