# Punakawan Distinguished Capabilities and Role Configuration Plan

**Status:** Proposed  
**Repository:** `gdncomm-yunaz-ramadhan/punakawan`  
**Scope:** Project-scoped Panel and MCP runtime improvements

## 1. Purpose

This plan focuses on five improvements:

1. **Simple role configuration**
2. **Contradiction Registry**
3. **Cross-Repository Impact Graph**
4. **Change Dossier**
5. **Cross-Agent Handoff Capsule**

Procedural knowledge is consistently called **Workflows**.

The product flow is:

```text
Understand the work
  → detect contradictions
  → calculate cross-repository impact
  → execute through workflows
  → produce a verifiable change dossier
  → hand off safely to another agent
```

The four roles remain easy to understand:

```text
Semar coordinates.
Gareng challenges.
Petruk plans and builds.
Bagong verifies.
```

Punakawan should position itself as a model-agnostic trust and continuity layer for software agents, not as another generic multi-agent framework.

## 2. Design Principles

### 2.1 Keep user configuration simple

The Panel exposes only:

- Enabled or disabled
- Style: Strict, Balanced, or Creative
- Mode: Assist, Propose, or Execute
- A short list of role-specific capability toggles

Internal policy may be more detailed, but users should not configure confidence matrices, numeric thresholds, or dozens of low-level permissions.

### 2.2 Preserve role boundaries

- Semar does not modify implementation files.
- Gareng does not implement fixes.
- Petruk does not approve its own work.
- Bagong does not modify the implementation it reviews.

### 2.3 Require evidence for important claims

Punakawan distinguishes:

```text
draft
claimed
supported
verified
disputed
rejected
superseded
```

### 2.4 Keep all capabilities project-scoped

```text
Project
├── Metadata
├── Roles
├── Workflows
├── Knowledge
├── Tasks
├── Plans
├── Contradictions
├── Impact
├── Change Dossiers
└── Handoffs
```

# Part I: Role Configuration

## 3. Panel Location

Add:

```text
Project
└── Settings
    └── Roles
```

Each role card contains:

```text
Role name and responsibility
Enabled toggle
Style selector
Mode selector
Role-specific capability toggles
Effective behavior preview
Reset defaults
Save
```

Example:

```text
┌────────────────────────────────────────┐
│ Gareng                         Enabled  │
│ Finds risk, impact, and contradiction  │
│                                        │
│ Style   Strict | Balanced | Creative   │
│ Mode    Assist | Propose | Execute     │
│                                        │
│ ☑ Detect contradictions                │
│ ☑ Analyze cross-repository impact      │
│ ☑ Run security checks                  │
│ ☑ Mark critical risks as blocking      │
│ ☑ Add findings to dossier              │
│                                        │
│                  Reset defaults  Save  │
└────────────────────────────────────────┘
```

## 4. Shared Setting: Enabled

When disabled:

- the role is not invoked automatically;
- workflows requiring the role cannot start unless the workflow permits skipping it;
- the Panel shows affected workflows;
- historical outputs remain readable.

## 5. Shared Setting: Style

Allowed values:

```text
strict
balanced
creative
```

### Strict

- requires stronger evidence;
- makes fewer assumptions;
- requests clarification more often;
- prefers established conventions;
- stops on unresolved material issues more readily.

### Balanced

- makes reasonable assumptions;
- flags uncertainty without unnecessary blocking;
- considers alternatives when useful;
- follows accepted plans and project conventions.

### Creative

- explores more alternatives;
- challenges obvious solutions;
- searches more broadly across repositories;
- proposes simpler or unconventional approaches;
- still obeys policy and capability restrictions.

Style changes reasoning behavior, not permissions.

## 6. Shared Setting: Mode

Allowed values:

```text
assist
propose
execute
```

### Assist

The role may read, search, analyze, and report. It may not modify durable project state.

### Propose

The role may create reviewable proposals, but nothing is applied automatically.

### Execute

The role may execute enabled capabilities, still constrained by project policy, workflow restrictions, and human approval.

## 7. Recommended Defaults

| Role | Style | Mode |
|---|---|---|
| Semar | Balanced | Execute |
| Gareng | Strict | Propose |
| Petruk | Creative | Execute |
| Bagong | Strict | Propose |

## 8. Semar

### Responsibility

Semar:

- interprets intent;
- gathers relevant context;
- selects workflows;
- coordinates roles;
- requests clarification;
- manages the Change Dossier;
- creates Handoff Capsules.

### Capabilities

```text
Select and invoke workflows
Ask for clarification
Coordinate other roles
Manage the change dossier
Create handoff capsules
```

### Defaults

```yaml
semar:
  enabled: true
  style: balanced
  mode: execute
  capabilities:
    workflows: true
    clarification: true
    coordinate_roles: true
    change_dossier: true
    handoff_capsule: true
```

### Restrictions

Semar must not:

- modify implementation files;
- verify Petruk's implementation claims;
- merge pull requests;
- override Bagong failures;
- ignore blocking contradictions.

## 9. Gareng

### Responsibility

Gareng:

- finds missing context;
- detects contradictions;
- analyzes feasibility and risk;
- calculates cross-repository impact;
- runs configured read-only checks;
- adds risk evidence to the dossier.

### Capabilities

```text
Detect contradictions
Analyze cross-repository impact
Run security and dependency checks
Mark critical risks as blocking
Add findings to the change dossier
```

### Defaults

```yaml
gareng:
  enabled: true
  style: strict
  mode: propose
  capabilities:
    contradictions: true
    cross_repository_impact: true
    security_checks: true
    blocking_risks: true
    change_dossier: true
```

### Restrictions

Gareng must not:

- modify project files;
- commit implementation;
- create or merge pull requests;
- resolve its own findings;
- directly alter accepted plans.

## 10. Petruk

### Responsibility

Petruk:

- proposes solutions;
- creates plans;
- decomposes plans into tasks;
- implements accepted work;
- coordinates changes across repositories;
- runs tests;
- attaches implementation evidence.

### Capabilities

```text
Create and update plans
Create and update tasks
Modify project files
Work across repositories
Create pull requests
Add implementation evidence to dossier
```

### Defaults

```yaml
petruk:
  enabled: true
  style: creative
  mode: execute
  capabilities:
    plans: true
    tasks: true
    modify_files: true
    cross_repository_changes: true
    create_pull_request: true
    change_dossier: true
```

### Restrictions

Petruk must not:

- approve its own plan;
- verify its own claims;
- resolve Bagong findings;
- merge pull requests;
- apply unapproved material plan deviations.

## 11. Bagong

### Responsibility

Bagong independently verifies:

- plan-to-implementation conformance;
- test results;
- cross-repository coverage;
- dossier claims;
- contradiction status;
- pull request quality.

### Capabilities

```text
Verify plan against implementation
Rerun tests and checks
Verify cross-repository coverage
Challenge dossier claims
Block completion when verification fails
Review pull requests
```

### Defaults

```yaml
bagong:
  enabled: true
  style: strict
  mode: propose
  capabilities:
    plan_verification: true
    rerun_checks: true
    cross_repository_verification: true
    challenge_dossier: true
    block_completion: true
    review_pull_request: true
```

### Restrictions

Bagong must not:

- modify implementation files;
- commit fixes;
- resolve its own findings;
- approve external writes;
- merge pull requests.

## 12. Persisted Configuration

Recommended location:

```text
.punakawan/roles.yaml
```

Complete example:

```yaml
version: punakawan.roles/v1

roles:
  semar:
    enabled: true
    style: balanced
    mode: execute
    capabilities:
      workflows: true
      clarification: true
      coordinate_roles: true
      change_dossier: true
      handoff_capsule: true

  gareng:
    enabled: true
    style: strict
    mode: propose
    capabilities:
      contradictions: true
      cross_repository_impact: true
      security_checks: true
      blocking_risks: true
      change_dossier: true

  petruk:
    enabled: true
    style: creative
    mode: execute
    capabilities:
      plans: true
      tasks: true
      modify_files: true
      cross_repository_changes: true
      create_pull_request: true
      change_dossier: true

  bagong:
    enabled: true
    style: strict
    mode: propose
    capabilities:
      plan_verification: true
      rerun_checks: true
      cross_repository_verification: true
      challenge_dossier: true
      block_completion: true
      review_pull_request: true
```

## 13. Role Configuration API

```http
GET   /api/v1/projects/{projectId}/roles
PATCH /api/v1/projects/{projectId}/roles/{role}
POST  /api/v1/projects/{projectId}/roles/{role}/reset
```

Requirements:

- validate allowed style and mode;
- reject capabilities not owned by the role;
- use optimistic locking;
- preserve previous revisions;
- publish a role-configuration event;
- show an effective behavior preview before saving.

## 14. Effective Behavior Preview

Example:

```text
Petruk can:
• Read project context
• Create plans and tasks
• Modify project files
• Work across repositories
• Run tests
• Propose pull requests

Petruk cannot:
• Approve its own work
• Merge pull requests
• Resolve Bagong findings
```

A policy-disabled control remains visible but locked with:

```text
Disabled by project policy
```

## 15. Workflow Integration

Workflows may require roles and may reduce capabilities.

```yaml
version: punakawan.workflow/v1

id: feature-delivery

roles:
  semar:
    required: true

  gareng:
    required: true

  petruk:
    required: true
    capabilities:
      create_pull_request: false

  bagong:
    required: true
```

A workflow must not increase permissions beyond the project role configuration.

```text
Project Petruk mode: Propose
Workflow Petruk mode: Execute
Effective mode: Propose
```

# Part II: Contradiction Registry

## 16. Purpose

The Contradiction Registry records disagreements between:

- Jira;
- Confluence;
- project metadata;
- workflows;
- knowledge;
- plans;
- source code;
- configuration;
- tests;
- API specifications.

Punakawan must not silently choose between conflicting sources.

## 17. Contradiction Model

```yaml
version: punakawan.contradiction/v1

id: contradiction-retry-limit-01
project_id: affiliate-platform

title: Conflicting retry limit
severity: material
status: needs_clarification

subject:
  type: configuration
  key: payout.retry.max_attempts

claims:
  - source:
      type: confluence
      ref: payout-policy
    statement: Maximum retry count is 3.
    evidence:
      - evidence-confluence-payout-policy

  - source:
      type: repository
      ref: affiliate-api/application.yaml
    statement: Maximum retry count is 5.
    evidence:
      - evidence-config-retry-limit

  - source:
      type: jira
      ref: TRF-1842
    statement: Maximum retry count should be 6.
    evidence:
      - evidence-jira-trf-1842

resolution:
  proposed_statement: Maximum retry count is 6.
  rationale: Jira issue is the approved change request.
  requires_human_confirmation: true
```

## 18. Statuses

```text
detected
triaged
needs_clarification
resolution_proposed
resolved
accepted_divergence
superseded
```

`accepted_divergence` documents intentionally different values rather than forcing artificial consistency.

## 19. Severity

```text
informational
minor
material
critical
```

| Severity | Default behavior |
|---|---|
| Informational | Record |
| Minor | Warn |
| Material | Clarify or resolve |
| Critical | Block |

Gareng's `blocking_risks` toggle controls whether Gareng may mark findings as blocking. Project policy may still force critical contradictions to block.

## 20. Detection

Use deterministic matching first:

- normalized keys;
- identifiers;
- API operation IDs;
- config keys;
- issue IDs;
- symbols;
- file paths;
- knowledge relations;
- BM25 candidate discovery.

Avoid requiring an embedding provider.

## 21. API

```http
GET  /api/v1/projects/{projectId}/contradictions
POST /api/v1/projects/{projectId}/contradictions
GET  /api/v1/projects/{projectId}/contradictions/{id}
POST /api/v1/projects/{projectId}/contradictions/{id}/propose-resolution
POST /api/v1/projects/{projectId}/contradictions/{id}/resolve
POST /api/v1/projects/{projectId}/contradictions/{id}/accept-divergence
```

## 22. Panel

Add:

```text
Project
└── Contradictions
```

List:

- severity;
- subject;
- source count;
- status;
- blocking;
- updated time.

Detail:

- side-by-side claims;
- source and evidence links;
- proposed resolution;
- affected plans, tasks, repositories, dossiers, and handoffs;
- resolve or accept-divergence actions.

# Part III: Cross-Repository Impact Graph

## 23. Purpose

The Impact Graph answers:

```text
What else is affected if this changes?
```

It supports:

- direct impact;
- transitive impact;
- cross-repository planning;
- missing test detection;
- ownership;
- deployment impact;
- Bagong coverage verification.

## 24. Node Types

```text
project
repository
source_symbol
api_operation
configuration_key
database_object
test
deployment_artifact
workflow
knowledge_record
plan
task
external_issue
team_owner
```

## 25. Edge Types

```text
contains
defines
calls
implements
consumes
tests
configures
deploys
depends_on
documented_by
owned_by
tracked_by
contradicts
derived_from
```

## 26. Evidence-Backed Edge

```yaml
from: repo:affiliate-ui
to: api:affiliate-api:getMerchantBadge
type: consumes
confidence: verified

evidence:
  - type: source_location
    ref: src/lib/api/merchant.ts:44

discovered_by:
  role: gareng
  method: openapi-client-reference
```

Confidence:

```text
observed
inferred
verified
disputed
```

## 27. Initial Graph Builders

1. Workspace and repository declarations
2. Git file and symbol inspection
3. OpenAPI operations and clients
4. Tests
5. Configuration keys
6. Deployment manifests
7. Project metadata
8. Knowledge relations
9. Workflows
10. Plans and tasks

Refresh incrementally when related sources change.

## 28. Impact Query

```json
{
  "subject": {
    "type": "api_operation",
    "id": "api:affiliate-api:getMerchantBadge"
  },
  "depth": 3,
  "include": [
    "repository",
    "test",
    "deployment_artifact",
    "team_owner"
  ]
}
```

Response includes:

- direct impact;
- transitive impact;
- affected repositories;
- affected tests;
- deployment artifacts;
- owners;
- missing coverage;
- related contradictions.

## 29. API

```http
GET  /api/v1/projects/{projectId}/impact/nodes
GET  /api/v1/projects/{projectId}/impact/nodes/{nodeId}
POST /api/v1/projects/{projectId}/impact/query
POST /api/v1/projects/{projectId}/impact/refresh
GET  /api/v1/projects/{projectId}/impact/changes/{changeId}
```

## 30. Panel

Add:

```text
Project
└── Impact
```

Initial UI should prioritize clear lists:

- affected repositories;
- affected tests;
- affected deployments;
- owners;
- missing coverage;
- related contradictions;
- related tasks and plans.

A graph visualization may be included, but it must not replace readable lists.

## 31. Role Responsibilities

### Semar

Defines scope and ensures impact analysis is included when needed.

### Gareng

Builds direct and transitive impact and identifies missing coverage.

### Petruk

Uses impact to create cross-repository plans and tasks and records intentional exclusions.

### Bagong

Verifies that known impact was handled or explicitly excluded with evidence.

# Part IV: Change Dossier

## 32. Purpose

The Change Dossier is the primary proof artifact.

It answers:

```text
What was requested?
What evidence defined it?
What contradictions existed?
What repositories were affected?
What plan was accepted?
What changed?
What was tested?
Did implementation match the plan?
What remains unresolved?
```

It must be:

- machine-readable;
- human-readable;
- versioned;
- project-scoped;
- evidence-backed;
- exportable.

## 33. Dossier Structure

```yaml
version: punakawan.change-dossier/v1

id: change-affiliate-retry-017
project_id: affiliate-platform
title: Add retry handling for affiliate payout processing
status: verified

objective:
  statement: Add bounded retry handling for transient payout failures.
  source_refs:
    - jira:TRF-1842

requirements:
  covered:
    - requirement-retry-transient-errors
  uncovered: []

contradictions:
  resolved:
    - contradiction-retry-limit-01
  unresolved: []

impact:
  repositories:
    - affiliate-api
    - affiliate-e2e
  excluded_repositories:
    - repository: affiliate-ui
      reason: No client-visible contract change.
  missing_coverage: []

plan:
  id: payout-retry-plan
  version: 4

tasks:
  completed:
    - task-71
    - task-72
    - task-73

implementation:
  changed_repositories:
    - affiliate-api
    - affiliate-e2e

verification:
  unit_tests: verified
  integration_tests: verified
  api_compatibility: verified
  security_review: supported

plan_conformance:
  implemented: 11
  partial: 0
  missing: 0
  deliberate_deviations:
    - item: Use a fixed retry interval.
      actual: Exponential backoff was used.
      rationale: Existing approved retry library supports it.
      approved: true

unresolved_risks:
  - Production metric cardinality requires monitoring.

rollback:
  verified: true
  procedure: Disable payout.retry.enabled.
```

## 34. Claim Model

```yaml
id: claim-api-compatible
type: compatibility
statement: Public API remains backward compatible.

producer:
  role: petruk

status: verified

evidence:
  - evidence-openapi-diff-41

verification:
  role: bagong
  result: verified
```

Rules:

- a role cannot verify its own claim;
- Petruk creates implementation claims;
- Gareng creates risk and feasibility claims;
- Semar creates completeness and coordination claims;
- Bagong verifies or disputes claims.

## 35. Evidence Model

```yaml
version: punakawan.evidence/v1

id: evidence-openapi-diff-41
type: api_compatibility

source:
  command: make api-compatibility
  repository: affiliate-api
  working_tree: worktree-task-73

result:
  status: passed
  exit_code: 0

artifacts:
  - path: evidence/openapi-diff.json
    sha256: abcdef123456
```

Evidence types:

```text
requirement_source
source_location
diff
test_result
build_result
api_compatibility
security_scan
dependency_scan
migration_check
screenshot
manual_confirmation
review_result
```

## 36. Plan Conformance

Bagong classifies each accepted plan item as:

```text
implemented
partially_implemented
missing
deliberately_changed
no_longer_applicable
unplanned
```

Rules:

- missing required items block completion;
- unexplained material deviations block verification;
- approved deviations are allowed;
- unplanned changes require classification and evidence.

## 37. API

```http
GET  /api/v1/projects/{projectId}/dossiers
POST /api/v1/projects/{projectId}/dossiers
GET  /api/v1/projects/{projectId}/dossiers/{dossierId}

POST /api/v1/projects/{projectId}/dossiers/{dossierId}/claims
POST /api/v1/projects/{projectId}/dossiers/{dossierId}/claims/{claimId}/verify
POST /api/v1/projects/{projectId}/dossiers/{dossierId}/claims/{claimId}/dispute

POST /api/v1/projects/{projectId}/dossiers/{dossierId}/evidence
POST /api/v1/projects/{projectId}/dossiers/{dossierId}/finalize

GET /api/v1/projects/{projectId}/dossiers/{dossierId}/export.md
GET /api/v1/projects/{projectId}/dossiers/{dossierId}/export.json
```

## 38. Panel

Add:

```text
Project
└── Change Dossiers
```

Detail tabs:

```text
Summary
Requirements
Contradictions
Impact
Plan and Tasks
Implementation
Evidence
Verification
History
```

Summary indicators:

```text
Requirements covered: 8 / 8
Plan conformance: 11 / 11
Repositories handled: 3 / 3
Open contradictions: 0
Verified claims: 14 / 15
Blocking findings: 1
```

## 39. Lifecycle

```text
draft
context_ready
planned
implementing
awaiting_verification
verified
disputed
completed
superseded
```

Flow:

```text
Semar creates dossier
  → Gareng adds contradiction, impact, and risk
  → Petruk links plan, tasks, implementation, and evidence
  → Bagong verifies claims and conformance
  → Semar finalizes after required approvals
```

# Part V: Cross-Agent Handoff Capsule

## 40. Purpose

The Handoff Capsule allows work to continue across:

- agent clients;
- model providers;
- sessions;
- machines;
- team members.

It must not depend on conversation transcript history.

## 41. Capsule Structure

```yaml
version: punakawan.handoff/v1

id: handoff-run-1842-03
project_id: affiliate-platform
run_id: run-1842

objective:
  statement: Add retry handling for affiliate payout processing.
  source_refs:
    - jira:TRF-1842

current_phase: implementation

accepted_plan:
  id: payout-retry-plan
  version: 4

role_configuration_revision: 7

completed_tasks:
  - task-71
  - task-72

current_task:
  id: task-73
  next_action: Add retry exhaustion integration test.

changed_repositories:
  - affiliate-api
  - affiliate-e2e

open_contradictions:
  - contradiction-retry-metric-tag

unresolved_risks:
  - Production metric cardinality is not yet measured.

impact_summary:
  required_repositories:
    - affiliate-api
    - affiliate-e2e
  excluded_repositories:
    - affiliate-ui

dossier:
  id: change-affiliate-retry-017
  status: implementing

evidence:
  - evidence-unit-tests-14
  - evidence-api-diff-21

created_by:
  role: semar
  agent_client: codex
```

The capsule references existing objects rather than copying full plans, knowledge, evidence, and diffs.

## 42. Resume Validation

Before resuming, validate:

- project exists;
- plan version still exists;
- role configuration revision is known;
- current task is still current;
- contradictions have not materially changed;
- repository state matches;
- evidence objects exist;
- required capabilities remain allowed;
- dossier is not superseded.

Statuses:

```text
resumable
refresh_required
blocked
superseded
invalid
```

Example:

```yaml
status: refresh_required

changes_since_handoff:
  - contradiction-retry-metric-tag was resolved.
  - role configuration changed from revision 7 to 8.

required_refresh:
  - reload role configuration
  - refresh contradiction summary
```

## 43. API and MCP

Panel API:

```http
GET  /api/v1/projects/{projectId}/handoffs
POST /api/v1/projects/{projectId}/handoffs
GET  /api/v1/projects/{projectId}/handoffs/{handoffId}
POST /api/v1/projects/{projectId}/handoffs/{handoffId}/validate
POST /api/v1/projects/{projectId}/handoffs/{handoffId}/resume
POST /api/v1/projects/{projectId}/handoffs/{handoffId}/supersede
```

MCP tools:

```text
create_handoff_capsule
get_handoff_capsule
validate_handoff_capsule
resume_from_handoff
```

`resume_from_handoff` returns only the smallest necessary verified context.

## 44. Panel

Add:

```text
Project
└── Handoffs
```

List:

- objective;
- phase;
- current task;
- source agent;
- status;
- created time;
- last validation.

Actions:

```text
Validate
Copy capsule ID
Export YAML
Resume
Supersede
```

# Part VI: Runtime, Storage, and Events

## 45. Suggested Storage

```text
.punakawan/
├── roles.yaml
├── workflows/
├── contradictions/
│   ├── index.yaml
│   └── records/
├── impact/
│   ├── nodes.jsonl
│   ├── edges.jsonl
│   └── snapshots/
├── dossiers/
│   └── <dossier-id>/
│       ├── manifest.yaml
│       ├── current.yaml
│       ├── claims.jsonl
│       ├── evidence/
│       └── versions/
└── handoffs/
    └── <handoff-id>.yaml
```

Canonical project knowledge remains in Dolt.

Use:

- YAML for configuration and manifests;
- append-only JSONL for events and claims;
- filesystem artifacts for evidence;
- Dolt where relational, versioned knowledge is beneficial.

## 46. Events

```text
project.roles_changed

contradiction.detected
contradiction.updated
contradiction.resolved

impact.node_changed
impact.edge_changed
impact.snapshot_updated

dossier.created
dossier.claim_added
dossier.claim_verified
dossier.claim_disputed
dossier.evidence_added
dossier.status_changed
dossier.finalized

handoff.created
handoff.validated
handoff.resumed
handoff.superseded
```

Events update Panel caches, SSE clients, project summaries, and audit history.

## 47. Role Configuration Resolver

```go
type RoleConfigResolver interface {
    Get(projectID string, role Role) (RoleConfig, error)
    Effective(projectID, workflowID string, role Role) (EffectiveRoleConfig, error)
}

type EffectiveRoleConfig struct {
    Enabled      bool
    Style        RoleStyle
    Mode         RoleMode
    Capabilities map[string]bool
}
```

## 48. Prompt Injection

Each role receives a compact block:

```text
Role configuration:
- Style: Strict
- Mode: Propose
- Enabled:
  - detect contradictions
  - analyze cross-repository impact
  - add findings to dossier
- Disabled:
  - run security checks
- You may propose durable changes but may not execute them.
```

## 49. Server-Side Enforcement

Prompts are not security controls.

Every MCP tool must enforce:

- role;
- mode;
- enabled capability;
- workflow restriction;
- project policy;
- approval requirement.

```text
Assist:
  read and analyze

Propose:
  read, analyze, and create reviewable proposals

Execute:
  execute enabled actions under policy and approval
```

## 50. Run Snapshot

Every run stores the role configuration revision and effective settings used.

Historical runs, dossiers, and handoffs must remain reproducible after configuration changes.

# Part VII: Panel Navigation and UX

## 51. Navigation

```text
Overview
Projects
  └── Project
      ├── Summary
      ├── Metadata
      ├── Roles
      ├── Workflows
      ├── Knowledge
      ├── Tasks
      ├── Plans
      ├── Contradictions
      ├── Impact
      ├── Change Dossiers
      ├── Handoffs
      ├── Sessions
      └── Health
Global Search
System
```

## 52. Role Save Flow

```text
User edits role
  → validate
  → show concise diff
  → confirm
  → save new revision
  → refresh behavior preview
```

Example diff:

```text
Gareng

Style:
  Balanced → Strict

Capabilities:
  Security checks: Enabled → Disabled

Effect:
  Gareng still detects contradictions and analyzes impact,
  but will not run configured security scanners.
```

## 53. Mobile

- one role card per row;
- large segmented controls;
- touch-friendly switches;
- collapsed behavior preview;
- sticky Save bar when dirty;
- no wide capability matrix.

## 54. Disabled State

```text
Bagong is disabled.

These workflows require Bagong:
• feature-delivery
• review-pr

Enable Bagong or edit those workflows before invoking them.
```

# Part VIII: Implementation Phases

## 55. Phase 0: Contracts

- define all schemas;
- add protocol types;
- add validation fixtures;
- add feature flags if needed.

Exit:

- schemas validate;
- generated types pass;
- no behavior change yet.

## 56. Phase 1: Role Configuration

- add roles store and defaults;
- add API;
- add Panel cards;
- add versioning;
- add effective resolver;
- inject prompt behavior;
- enforce capabilities server-side;
- snapshot settings in runs.

Exit:

- all roles configurable in Panel;
- disabled capability cannot execute;
- historical run retains configuration.

## 57. Phase 2: Contradiction Registry

- add store;
- add lifecycle and API;
- add Panel;
- add Gareng submission;
- integrate Semar blocking and clarification;
- link to plans, tasks, dossiers, and handoffs.

Exit:

- conflicts become durable records;
- material conflicts can block;
- accepted divergence works;
- history is preserved.

## 58. Phase 3: Impact Graph

- add graph store;
- add builders;
- add evidence-backed edges;
- add queries;
- add Gareng analysis;
- integrate Petruk planning;
- add Bagong verification;
- add Panel.

Exit:

- direct and transitive impact returned;
- affected repositories and tests visible;
- omitted known impact can be detected.

## 59. Phase 4: Change Dossier

- add dossier lifecycle;
- add claims and evidence;
- add requirement, contradiction, impact, plan, task, and implementation links;
- add conformance;
- add Bagong verification;
- add Markdown and JSON export;
- add PR summary.

Exit:

- one run produces a complete dossier;
- claims have producers and verifiers;
- blocking issues prevent verification;
- exports are useful outside Panel.

## 60. Phase 5: Handoff Capsule

- add capsule store;
- add create, validate, resume, supersede;
- add MCP tools;
- add Panel;
- add cross-agent resume test.

Exit:

- another client resumes without transcript;
- stale references are detected;
- capsule stays compact.

## 61. Phase 6: Hardening

- integrate features into workflows;
- add events and cache invalidation;
- add corruption handling;
- add audit history;
- add concurrency and security tests;
- add end-to-end fixture.

Exit:

- complete requirement-to-handoff scenario passes;
- role restrictions are enforced regardless of model behavior.

# Part IX: Detailed Backlog

## 62. Role Configuration

- `ROLE-001` Role configuration protocol
- `ROLE-002` Recommended defaults
- `ROLE-003` Roles YAML store
- `ROLE-004` Configuration history
- `ROLE-005` Effective config resolver
- `ROLE-006` Capability authorization
- `ROLE-007` Role settings API
- `ROLE-008` Panel role cards
- `ROLE-009` Behavior preview
- `ROLE-010` Workflow restriction integration
- `ROLE-011` Prompt injection
- `ROLE-012` Run snapshot
- `ROLE-013` Reset defaults
- `ROLE-014` Tests

## 63. Contradictions

- `CONTRA-001` Protocol
- `CONTRA-002` Store
- `CONTRA-003` Events
- `CONTRA-004` API
- `CONTRA-005` Resolution
- `CONTRA-006` Accepted divergence
- `CONTRA-007` Gareng submission
- `CONTRA-008` Semar blocking integration
- `CONTRA-009` Panel list
- `CONTRA-010` Panel detail
- `CONTRA-011` Entity links
- `CONTRA-012` Deterministic matching
- `CONTRA-013` Tests

## 64. Impact

- `IMPACT-001` Node and edge protocols
- `IMPACT-002` Graph store
- `IMPACT-003` Edge evidence
- `IMPACT-004` Repository builder
- `IMPACT-005` OpenAPI builder
- `IMPACT-006` Test builder
- `IMPACT-007` Configuration builder
- `IMPACT-008` Knowledge adapter
- `IMPACT-009` Workflow, plan, and task adapters
- `IMPACT-010` Direct query
- `IMPACT-011` Transitive query
- `IMPACT-012` Gareng tool
- `IMPACT-013` Petruk integration
- `IMPACT-014` Bagong verification
- `IMPACT-015` Panel
- `IMPACT-016` Incremental refresh
- `IMPACT-017` Tests

## 65. Dossier

- `DOSSIER-001` Protocol
- `DOSSIER-002` Store
- `DOSSIER-003` Lifecycle
- `DOSSIER-004` Claims
- `DOSSIER-005` Evidence
- `DOSSIER-006` Role contribution rules
- `DOSSIER-007` Requirement coverage
- `DOSSIER-008` Contradiction section
- `DOSSIER-009` Impact section
- `DOSSIER-010` Plan and task links
- `DOSSIER-011` Implementation evidence
- `DOSSIER-012` Conformance
- `DOSSIER-013` Bagong verification
- `DOSSIER-014` API
- `DOSSIER-015` Panel
- `DOSSIER-016` Markdown export
- `DOSSIER-017` JSON export
- `DOSSIER-018` PR summary
- `DOSSIER-019` Tests

## 66. Handoff

- `HANDOFF-001` Protocol
- `HANDOFF-002` Store
- `HANDOFF-003` Create
- `HANDOFF-004` Validate
- `HANDOFF-005` Resume
- `HANDOFF-006` Supersede
- `HANDOFF-007` Semar contribution
- `HANDOFF-008` Gareng contribution
- `HANDOFF-009` Petruk contribution
- `HANDOFF-010` Bagong contribution
- `HANDOFF-011` MCP tools
- `HANDOFF-012` Panel
- `HANDOFF-013` Cross-agent resume test
- `HANDOFF-014` Stale reference tests

# Part X: Testing and Acceptance

## 67. Unit Tests

Role configuration:

- style and mode validation;
- role-specific capability validation;
- workflow restriction;
- reset;
- optimistic locking.

Contradictions:

- severity;
- transitions;
- accepted divergence;
- duplicate detection;
- normalized source matching.

Impact:

- node and edge identity;
- traversal;
- cycles;
- confidence;
- evidence;
- disputed edges.

Dossiers:

- producer and verifier separation;
- evidence validation;
- conformance totals;
- blocking findings;
- export stability.

Handoffs:

- plan revision validation;
- role revision validation;
- task changes;
- contradiction changes;
- repository-state validation;
- resume classification.

## 68. End-to-End Scenario

Fixture repositories:

```text
repo-api
repo-ui
repo-e2e
```

Scenario:

```text
1. Semar starts feature-delivery.
2. Gareng detects conflicting retry values.
3. User resolves contradiction.
4. Gareng identifies API, UI, and E2E impact.
5. Petruk creates cross-repository plan and tasks.
6. Petruk implements API and E2E changes.
7. Bagong checks whether UI is affected or properly excluded.
8. Dossier becomes verified.
9. Semar creates a handoff capsule.
10. Another agent resumes and proposes the pull request.
```

## 69. Acceptance Criteria

### Roles

- all four roles configurable in Panel;
- only Enabled, Style, Mode, and relevant toggles are exposed;
- restrictions are enforced server-side;
- workflow cannot escalate permissions;
- run snapshots retain settings.

### Contradictions

- disagreements become durable records;
- material issues can block;
- accepted divergence supported;
- history retained;
- dossier and handoff references supported.

### Impact

- direct and transitive queries work;
- edges carry evidence or are marked inferred;
- repositories, tests, deployment, and ownership visible;
- Bagong verifies coverage;
- refresh is incremental.

### Dossier

- links requirements, contradictions, impact, plan, tasks, changes, and evidence;
- role cannot verify own claim;
- deviations visible;
- unresolved blocking findings prevent verification;
- Markdown and JSON export work.

### Handoff

- compact verified state;
- no transcript required;
- stale references detected;
- another agent resumes through MCP;
- superseded capsule cannot resume silently.

# 70. Definition of Done

```text
A Punakawan project exposes simple role settings in the Panel.

Semar, Gareng, Petruk, and Bagong each retain a clear responsibility,
an understandable Style and Mode, and a short list of relevant capabilities.

Gareng detects contradictions and calculates cross-repository impact.

Petruk uses those findings to create coordinated plans, tasks,
implementation changes, and evidence.

Bagong independently verifies impact coverage, plan conformance,
tests, and dossier claims.

Semar finalizes a proof-carrying Change Dossier and creates a compact
Cross-Agent Handoff Capsule.

A different MCP-compatible agent resumes the work without the original
conversation transcript.

Every protected action remains constrained by project policy,
workflow restrictions, role configuration, and human approval.
```
