# Punakawan Artifact Review and Plan Mutation Implementation Plan

**Status:** Proposed  
**Assumption:** Punakawan Panel v1 is already implemented  
**Roadmap position:** New Plan 1 of 2; implement before procedural knowledge and retrieval recipes  
**Primary purpose:** Let users review plans and procedural knowledge, submit anchored feedback, automatically trigger a Punakawan revision workflow, and accept a validated new immutable version  
**Canonical behavior:** Proposal-based mutation, never direct in-place editing  
**Supported artifacts in this plan:** Markdown plans  
**Deferred extension:** Retrieval recipe review and mutation are implemented in New Plan 2  
**Persistence:** Existing Punakawan files, durable knowledge, evidence, and BD tasks  
**New database:** None  

---

## 1. Target Experience

```text
1. User opens a Markdown plan.
2. User adds comments against exact sections, paragraphs, or fields.
3. User submits the review.
4. Panel creates an immutable revision request.
5. Punakawan automatically starts the matching revision workflow.
6. Punakawan reads the base artifact, comments, related knowledge, evidence,
   tasks, and governing policy.
7. Punakawan generates a complete proposed new version.
8. Panel shows the diff, validation, comment resolutions, and unresolved items.
9. User accepts, requests another revision, or rejects the proposal.
10. Only an accepted proposal becomes the next canonical version.
```

The panel must not rewrite the canonical artifact when the user presses Submit.
Submission creates a **revision request**, not a direct mutation. Apparently a
textarea is not a sufficient governance model after all.

---

## 2. Existing Foundation and Two-Plan Sequence

Punakawan Panel v1 is treated as completed. This plan does not rebuild:

- Workspace discovery.
- Session, task, knowledge, evidence, and approval views.
- Panel routing and application shell.
- SSE event delivery.
- Source health and partial-failure handling.
- Existing read-only API contracts.
- Secret redaction and local filesystem boundaries.

Only two new implementation plans follow:

```text
New Plan 1
  Artifact Review and Plan Mutation
  → anchored comments
  → immutable review submission
  → automatic Punakawan revision run
  → proposal diff and validation
  → explicit acceptance

New Plan 2
  Procedural Knowledge and Retrieval Recipes
  → reusable read procedures
  → Jira selector compilation and validation
  → guided teaching and correction
  → recipe-specific review and immutable versions
```

Plan 1 establishes the shared review protocol. Plan 2 reuses that protocol for
retrieval recipes and adds provider-specific validation.

---

## 3. Core Principle: Proposal-Based Mutation

```text
Canonical version N
  + submitted review snapshot
  → Punakawan revision run
  → proposed version N+1
  → validation
  → explicit user acceptance
  → canonical version N+1
```

Allowed panel mutations:

- Create and resume a draft review.
- Add, edit, move, or delete draft comments.
- Submit a review snapshot.
- Answer clarification questions.
- Request changes against a proposal.
- Accept or reject a proposal.
- Cancel an unsubmitted review.

Forbidden direct mutations:

- Overwrite canonical plan content.
- Rewrite historical versions.
- Change a retrieval recipe without recompilation and validation.
- Invoke arbitrary shell commands from review text.
- Bypass Punakawan policy or approval services.
- Accept a proposal built from an obsolete base version silently.

---

## 4. Shared Artifact Reference

```yaml
api_version: punakawan.dev/v1
kind: ArtifactReference

artifact:
  type: plan
  id: plan-panel
  version: 3
  revision_hash: sha256:...
  workspace_id: punakawan
  format: markdown
  canonical_location: .punakawan/plans/plan-panel/versions/3.md
```

Artifact type implemented in this plan:

```text
plan
```

The contracts should remain extensible, but `retrieval_recipe` is enabled only by New Plan 2 after its compiler and validation lifecycle exist.

Each artifact type must provide:

- Version reader.
- Stable anchor resolver.
- Proposal renderer.
- Diff generator.
- Validator.
- Acceptance handler.

---

## 5. Durable Review Model

### 5.1 Review Session

```yaml
api_version: punakawan.dev/v1
kind: ArtifactReview

metadata:
  id: review-plan-panel-001
  workspace_id: punakawan
  status: draft
  created_by: user
  created_at: 2026-07-23T13:00:00+07:00
  updated_at: 2026-07-23T13:08:12+07:00

artifact:
  type: plan
  id: plan-panel
  version: 3
  revision_hash: sha256:...

review:
  title: Panel architecture revision
  instruction: Apply all comments while preserving unaffected detail.
  comment_count: 4
```

Review states:

```text
draft
submitted
queued
revising
awaiting_clarification
proposal_ready
revision_requested
accepted
rejected
cancelled
failed
conflicted
```

### 5.2 Anchored Comment

```yaml
id: comment-003
review_id: review-plan-panel-001
author: user
status: open

anchor:
  kind: markdown_block
  block_id: panel.security.network-boundary
  heading_path:
    - Security Model
    - Network Boundary
  base_revision_hash: sha256:...
  quoted_text: "Default binding: 127.0.0.1 only"

body: >
  Add an optional authenticated LAN mode, while keeping loopback as the default.
```

Comment states:

```text
open
addressed
partially_addressed
rejected_by_agent
needs_clarification
obsolete
resolved_by_user
```

### 5.3 Revision Request

Submission creates an immutable snapshot:

```yaml
api_version: punakawan.dev/v1
kind: ArtifactRevisionRequest

metadata:
  id: revision-request-019
  review_id: review-plan-panel-001
  submitted_at: 2026-07-23T13:10:00+07:00
  submitted_by: user

base_artifact:
  type: plan
  id: plan-panel
  version: 3
  revision_hash: sha256:...

workflow:
  type: revise_plan_from_review
  auto_start: true
  require_final_acceptance: true
  max_revision_attempts: 5

comments:
  snapshot_hash: sha256:...
  count: 4
```

### 5.4 Revision Proposal

```yaml
api_version: punakawan.dev/v1
kind: ArtifactRevisionProposal

metadata:
  id: proposal-019-01
  review_id: review-plan-panel-001
  revision_request_id: revision-request-019
  attempt: 1
  status: ready

base:
  artifact_id: plan-panel
  version: 3
  revision_hash: sha256:...

proposed:
  version: 4
  content_hash: sha256:...
  content_location: .punakawan/reviews/review-plan-panel-001/proposals/1.md

results:
  addressed_comments: 3
  partially_addressed_comments: 1
  unresolved_comments: 0
  validation_status: passed
```

---

## 6. Stable Comment Anchoring

Line numbers are not durable enough. Use stable block identifiers.

### Markdown Plans

```markdown
<!-- pk:block:panel.security.network-boundary -->
## Network Boundary
```

Smaller reviewable blocks may also have identifiers:

```markdown
<!-- pk:block:panel.security.loopback-default -->
The panel binds to loopback by default.
```

Anchor resolution order:

```text
1. Exact block ID.
2. Exact anchored-block content hash.
3. Heading path plus quoted text.
4. Heading path plus limited fuzzy quoted-text match.
5. Mark as conflicted and require user re-anchoring.
```


---

## 7. Storage Layout

```text
.punakawan/
├── plans/
│   └── <plan-id>/
│       ├── current.yaml
│       └── versions/
│           ├── 1.md
│           ├── 2.md
│           └── 3.md
├── reviews/
│   └── <review-id>/
│       ├── review.yaml
│       ├── comments.jsonl
│       ├── submissions/
│       │   └── <request-id>.yaml
│       ├── proposals/
│       │   ├── 1.md
│       │   ├── 1.patch
│       │   └── 1.yaml
│       ├── clarifications.jsonl
│       └── evidence/
└── runs/
    └── <revision-run-id>/
```

For retrieval recipes:

- Canonical versions remain in durable knowledge.
- Review metadata and proposal evidence live under `.punakawan/reviews/`.
- A proposed recipe is not verified knowledge.
- Acceptance writes a new version through the knowledge repository service.
- Previous versions remain immutable.

No new database is required.

---

## 8. Automatic Retrigger Flow

```text
User submits review
  → ReviewService freezes the comment snapshot
  → idempotency key is generated
  → RevisionWorkflowDispatcher dispatches a typed run
  → BD parent and child tasks are created
  → Punakawan revises and validates the artifact
  → proposal and evidence are stored
  → review.proposal_ready is emitted
  → panel shows diff and resolutions
```

Core interface:

```go
type RevisionWorkflowDispatcher interface {
    Dispatch(
        ctx context.Context,
        request ArtifactRevisionRequest,
    ) (RunReference, error)
}
```

Workflow types:

```text
revise_plan_from_review
revise_retrieval_recipe_from_review
```

Do not trigger revisions by concatenating a shell command. The dispatcher uses the
in-process orchestrator or Punakawan's durable local run queue.

### Idempotency

A submission key is derived from:

```text
review ID
base revision hash
comment snapshot hash
submission sequence
```

Submitting twice must return the existing run, not create two competing agents
rewriting the same artifact with identical instructions.

---

## 9. Agent Revision Contract

Input:

```yaml
objective: Revise the artifact using the submitted review.

base_artifact:
  id: plan-panel
  version: 3
  revision_hash: sha256:...
  complete_content: true

review:
  request_id: revision-request-019
  comments: [...]

context:
  related_knowledge_ids: [...]
  related_task_ids: [...]
  related_evidence_ids: [...]
  governing_policy_ids: [...]

constraints:
  preserve_unaffected_content: true
  complete_artifact_required: true
  direct_canonical_write_forbidden: true
  unresolved_comments_must_be_reported: true
```

Required output:

```yaml
proposal:
  complete_artifact: ...
  machine_patch: ...
  change_summary: ...
  comment_resolutions:
    - comment_id: comment-001
      status: addressed
      explanation: ...
      changed_block_ids: [...]
```

The agent must produce a complete artifact proposal, not isolated replacement
snippets. Unaffected details must remain present.

---

## 10. Clarification and Revision Attempts

When a comment is materially ambiguous:

```text
revising
  → awaiting_clarification
  → user answers in panel
  → same run resumes from checkpoint
  → proposal_ready
```

Rules:

- Ask only for ambiguity that affects correctness or safety.
- Store clarification answers as review evidence.
- Do not change the immutable original submission.
- Attach the answer to the active revision attempt.
- Preserve completed work across clarification.
- Enforce a maximum clarification and revision-attempt count.

`Request changes` creates attempt 2 under the same review. Attempt 1 remains
available for comparison and audit.

---

## 11. Plan Validation

A plan proposal must pass:

### Structural checks

- Artifact ID remains stable.
- Version increments correctly.
- Block IDs are unique.
- Required major sections remain.
- Markdown fences are balanced.
- Heading hierarchy is valid.
- Backlog IDs are unique.
- Internal references resolve where possible.

### Consistency checks

- Goals and non-goals do not directly contradict.
- Delivery phases preserve declared dependencies.
- Acceptance criteria cover new behavior.
- Removed requirements are listed explicitly.
- New runtime dependencies are declared.
- Security restrictions are not weakened without an explicit reviewed comment.

### Review-compliance checks

- Every comment has a resolution.
- Addressed comments identify changed blocks.
- Rejected comments include an explanation.
- Unresolved comments block acceptance.
- Unrelated changes are highlighted.

---


## 12. Optimistic Concurrency and Rebase

Before acceptance:

```text
current canonical hash == proposal base hash
```

When false, mark the review `conflicted`.

Available actions:

- Rebase comments onto the latest canonical version.
- Compare old base, current canonical version, and proposal.
- Re-run Punakawan using the original comments against the latest version.
- Cancel the stale proposal.

Never silently overwrite the newer version.


---

## 13. Panel UI Modernization and Review Experience

The existing Panel v1 shell should be visually upgraded as part of this plan.
The result should remain an engineering tool, but it should no longer resemble a
collection of debugging pages that happened to discover CSS on a Friday afternoon.

The UI must be:

- Responsive from small mobile screens to large desktop monitors.
- Clean, modern, and information-dense without feeling cramped.
- Built from shared reusable components.
- Consistent across overview, workspace, plan review, active revision, proposal,
  evidence, and system pages.
- Themeable through semantic color tokens.
- Available in light and dark themes.
- Usable with keyboard, mouse, and touch.
- Accessible without depending on color, hover, charts, or graph geometry alone.

### 13.1 Visual Direction

Use a restrained technical visual language:

```text
Shape:
  rounded cards
  clear grouping
  thin borders
  very limited shadow
  generous but not wasteful spacing

Hierarchy:
  page title
  contextual summary
  actionable status
  detailed evidence

Motion:
  subtle state transitions
  updated-row highlight
  panel and drawer transitions
  chart and graph entrance only when useful
```

Avoid:

- Heavy gradients on every surface.
- Permanent glow effects.
- Oversized hero sections.
- Excessive glassmorphism.
- Decorative charts without an operational question.
- Tiny gray text apparently designed for engineers with eagle genetics.
- Different card treatments on every page.

### 13.2 Semantic Color System

Use CSS custom properties as the only source of semantic UI color.

Recommended default identity:

```text
Primary accent:
  indigo

Secondary accent:
  teal

Attention:
  amber

Danger:
  red

Success:
  green
```

Suggested light-theme tokens:

```css
:root {
  --color-bg: #f6f7fb;
  --color-surface: #ffffff;
  --color-surface-subtle: #eef1f6;
  --color-surface-raised: #ffffff;

  --color-text: #172033;
  --color-text-muted: #667085;
  --color-border: #dce2ea;
  --color-border-strong: #c7cfdb;

  --color-accent: #5b5bd6;
  --color-accent-hover: #4b4bc4;
  --color-accent-soft: #ececfe;
  --color-accent-contrast: #ffffff;

  --color-secondary: #0f9f9c;
  --color-success: #16875b;
  --color-warning: #b76e00;
  --color-danger: #c7374f;
  --color-info: #2878c7;

  --shadow-card: 0 1px 2px rgb(15 23 42 / 0.05);
  --radius-card: 16px;
}
```

Suggested dark-theme tokens:

```css
[data-theme="dark"] {
  --color-bg: #0c111b;
  --color-surface: #141b2a;
  --color-surface-subtle: #1b2435;
  --color-surface-raised: #192235;

  --color-text: #e8ecf4;
  --color-text-muted: #99a4b5;
  --color-border: #293449;
  --color-border-strong: #3a465d;

  --color-accent: #8b8cf6;
  --color-accent-hover: #a0a1ff;
  --color-accent-soft: #29294f;
  --color-accent-contrast: #101321;

  --color-secondary: #35c7c4;
  --color-success: #46bd86;
  --color-warning: #e5a940;
  --color-danger: #ee6b80;
  --color-info: #65a9ef;

  --shadow-card: 0 1px 2px rgb(0 0 0 / 0.28);
}
```

Rules:

- All component colors must use semantic tokens.
- Chart and graph palettes must derive from the same tokens.
- Status colors must include icons and labels.
- Borders remain visible in both themes.
- Accent colors must pass contrast checks for their usage.
- Destructive actions must not use the primary accent.
- The design should support future accent presets without rewriting components.

### 13.3 Theme and Accent Controls

Add a shared `ThemeToggle` with:

```text
Light
Dark
System
```

Behavior:

- Default to `System` for new installations.
- Resolve `System` through `prefers-color-scheme`.
- Persist the user's explicit selection locally.
- Apply the theme before the application paints to prevent flashing.
- Update Chart.js and Cytoscape.js colors when the theme changes.
- Respect `prefers-reduced-motion`.

Add an optional `AccentPicker` under panel appearance settings.

Initial presets:

```text
Indigo
Teal
Blue
Violet
Amber
```

The default remains Indigo. Accent selection affects:

- Primary buttons.
- Active navigation.
- Selected filters.
- Links.
- Focus rings.
- Primary chart series.
- Selected graph nodes.

It must not recolor warning, danger, or success semantics.

### 13.4 Responsive Layout System

Use a mobile-first layout with three primary ranges:

| Range | Width | Behavior |
|---|---:|---|
| Mobile | `< 640px` | One column, drawer navigation, bottom sheets, card-list tables |
| Tablet | `640px–1023px` | Two-column bento layout, compact sidebar or drawer |
| Desktop | `>= 1024px` | Persistent sidebar, multi-column bento, split review views |

Additional wide-screen behavior begins at `1440px`, but content should not spread
into vast empty deserts. Dense review content may use the width; prose should
retain a readable line length.

Core layout:

```text
Desktop
┌──────────────┬─────────────────────────────────────────┐
│ Sidebar      │ Top bar                                 │
│              ├─────────────────────────────────────────┤
│              │ Page content                            │
│              │ Bento grid / table / split review      │
└──────────────┴─────────────────────────────────────────┘

Mobile
┌────────────────────────────────────────────────────────┐
│ Compact top bar                                        │
├────────────────────────────────────────────────────────┤
│ One-column content                                     │
│ Cards / list rows / tabs                               │
├────────────────────────────────────────────────────────┤
│ Optional bottom navigation or action bar               │
└────────────────────────────────────────────────────────┘
```

Responsive rules:

- Sidebar becomes a drawer or compact bottom navigation on mobile.
- Three-pane proposal review becomes tabs on mobile.
- Detail drawers become bottom sheets on mobile.
- Toolbars wrap or collapse into an overflow menu.
- Primary actions remain visible through a sticky action bar when reviewing.
- Tables become a compact card list when columns would become unreadable.
- Graph controls remain outside the graph viewport.
- No essential action depends on hover.
- Touch targets are at least `44px`.
- Horizontal scrolling is reserved for diffs and genuinely dense matrices.
- Page padding scales from `12–16px` on mobile to `24–32px` on desktop.

### 13.5 Bento Grid

Use a reusable `BentoGrid` and `BentoCard` system for overview and summary pages.

Desktop grid:

```text
12 columns
minimum row height: 120px
gap: 16px
```

Tablet:

```text
6 columns
gap: 12px
```

Mobile:

```text
1 column
natural card height
gap: 12px
```

Supported card spans:

```text
small:   3 columns
medium:  4–6 columns
wide:    8–12 columns
tall:    2 rows
full:    full width
```

Cards must declare semantic size rather than embedding page-specific grid spans.

Recommended overview composition:

```text
Row 1
  Active revisions       metric card
  Reviews needing input  metric card
  Failed runs            metric card
  Accepted this week     metric card

Row 2
  Review activity trend  wide chart card
  Needs attention        medium status card

Row 3
  Current revisions      wide table card
  Artifact relations     medium graph card

Row 4
  Recent proposals       full table or timeline card
```

Bento cards should answer operational questions. They are not permission to put
every count in a decorative rectangle.

### 13.6 Shared Card Components

Required components:

```text
Card
BentoCard
MetricCard
StatusCard
ChartCard
GraphCard
TableCard
ReviewCard
DiffSummaryCard
EmptyStateCard
ErrorStateCard
SkeletonCard
```

`Card` owns:

- Surface color.
- Border.
- Radius.
- Internal spacing.
- Header and footer slots.
- Loading, empty, warning, and error states.

`BentoCard` adds:

- Semantic size.
- Optional metric header.
- Responsive span.
- Optional drill-down action.

Do not duplicate card CSS inside feature pages.

### 13.7 Shared Data Table

Create a reusable `DataTable` rather than building an unrelated table on every
page.

Capabilities:

- Typed column definitions.
- Sorting.
- Search and filter integration.
- Pagination or cursor loading.
- Column visibility.
- Sticky header where useful.
- Row selection where an action actually needs it.
- Status and badge renderers.
- Loading skeleton.
- Empty and error states.
- Keyboard-accessible row actions.
- Compact and comfortable density modes.
- Responsive card-row renderer.

Desktop table:

```text
sticky header
clear column alignment
row hover and keyboard focus
optional expandable detail
```

Mobile table transformation:

```text
Title or primary identifier
Status and priority
Two or three important fields
Expandable secondary details
Visible primary action
```

Do not squeeze an eight-column desktop table into a 360px viewport and call the
result responsive.

### 13.8 Charts

Use **Chart.js** behind a shared `ChartShell` and typed chart adapters.

Recommended supported chart types:

```text
Line
Bar
Stacked bar
Doughnut
```

Initial operational charts:

- Review submissions over time.
- Revision completion and failure trend.
- Median revision duration.
- Comment resolution distribution.
- Proposal outcomes.
- Change volume by artifact version.
- Task progress associated with active reviews.

Chart rules:

- A chart must answer a stated operational question.
- Every chart has a title, short description, and accessible textual summary.
- Tooltips use readable timestamps and labels.
- Legends are hidden when the series is obvious.
- Axis labels are shortened carefully, not silently discarded.
- Use responsive containers with an explicit minimum and maximum height.
- Theme colors come from CSS tokens.
- Disable or shorten animation for reduced-motion users.
- Lazy-load Chart.js only when a chart enters a page that uses it.
- Destroy chart instances when the component unmounts.
- Use `ResizeObserver` through the shared wrapper.
- Provide a table or metric fallback for essential information.

Example wrapper contract:

```ts
type PanelChartConfig = {
  type: 'line' | 'bar' | 'stacked-bar' | 'doughnut';
  title: string;
  description?: string;
  labels: string[];
  series: ChartSeries[];
  valueFormat?: 'count' | 'duration' | 'percentage' | 'bytes';
  emptyMessage: string;
};
```

### 13.9 Graph and Connector Visuals

Use **Cytoscape.js** behind a reusable `GraphCanvas` component.

Initial graph use cases:

- Plan and related-knowledge relationships.
- Review → revision run → proposal → accepted-version flow.
- BD task dependencies.
- Artifact version lineage.
- Comment-to-changed-block relationships.
- Later, retrieval-recipe relations from New Plan 2.

Graph features:

- Directed connectors with arrowheads.
- Edge labels.
- Node type, status, and selection states.
- Fit-to-view.
- Zoom controls.
- Reset layout.
- Search and focus.
- Click to open a detail drawer.
- Keyboard-accessible equivalent list.
- Deterministic layout where possible.
- Theme-aware colors and connector contrast.
- Reduced animation on mobile and reduced-motion systems.

Recommended layouts:

```text
breadthfirst:
  workflow and version lineage

grid:
  small deterministic sets

cose:
  exploratory relationship graphs
```

Rules:

- Do not render large graphs by default.
- Start from a focused subgraph.
- Collapse secondary relations.
- Cap visible nodes and offer explicit expansion.
- Show a list fallback with identical relationships.
- Lazy-load Cytoscape.js only on graph pages.
- Keep graph state in a feature store, not inside random page callbacks.
- Do not rely on connector color alone to identify relation type.

Example component contract:

```ts
type GraphNode = {
  id: string;
  label: string;
  type: string;
  status?: string;
  metadata?: Record<string, unknown>;
};

type GraphEdge = {
  id: string;
  source: string;
  target: string;
  relation: string;
  label?: string;
};
```

### 13.10 Review Experience

Artifact actions:

```text
Review
View versions
View related sessions
View evidence
```

#### Review mode

Desktop:

```text
Plan document | Anchored comment rail
```

Mobile:

```text
Plan document
  + floating Add Comment action
  + comments in bottom sheet
```

Review mode includes:

- Section and selected-text commenting.
- Draft comment sidebar or bottom sheet.
- General review instruction.
- Base version and revision hash.
- Comment status chips.
- Sticky Submit Review action.
- Unsaved-change indicator.
- Clear exit and resume behavior.

#### Active revision

Use a bento summary:

```text
Current phase
Elapsed time
Comments processed
Clarification needed
Validation state
Related BD task
```

Below the summary:

- Live timeline.
- Clarification card.
- Evidence table.
- Retryable failure card.
- Related task graph when useful.

#### Proposal review

Desktop:

```text
Comments | Artifact diff | Validation and resolutions
```

Tablet:

```text
Diff | Side panel for comments and validation
```

Mobile:

```text
Tabs:
  Diff
  Comments
  Validation
  Evidence

Sticky bottom actions:
  Request changes
  Reject
  Accept
```

Diff behavior:

- Side-by-side when sufficient width exists.
- Unified diff on mobile.
- Collapsible unchanged sections.
- Comment markers beside affected blocks.
- Added, removed, and modified summaries.
- Search inside diff.
- Jump to unresolved comment.

### 13.11 Shared Component Architecture

Recommended frontend structure:

```text
web/panel/src/lib/
├── components/
│   ├── layout/
│   │   ├── AppShell.svelte
│   │   ├── Sidebar.svelte
│   │   ├── MobileNavigation.svelte
│   │   ├── PageHeader.svelte
│   │   ├── ResponsiveToolbar.svelte
│   │   └── StickyActionBar.svelte
│   ├── cards/
│   │   ├── Card.svelte
│   │   ├── BentoGrid.svelte
│   │   ├── BentoCard.svelte
│   │   ├── MetricCard.svelte
│   │   ├── StatusCard.svelte
│   │   ├── ChartCard.svelte
│   │   └── GraphCard.svelte
│   ├── data/
│   │   ├── DataTable.svelte
│   │   ├── MobileDataList.svelte
│   │   ├── FilterBar.svelte
│   │   ├── Pagination.svelte
│   │   └── StatusBadge.svelte
│   ├── visualization/
│   │   ├── ChartShell.svelte
│   │   ├── GraphCanvas.svelte
│   │   ├── GraphControls.svelte
│   │   └── VisualizationFallback.svelte
│   ├── review/
│   │   ├── ArtifactViewer.svelte
│   │   ├── CommentComposer.svelte
│   │   ├── CommentRail.svelte
│   │   ├── ProposalDiff.svelte
│   │   ├── ResolutionList.svelte
│   │   └── ReviewTimeline.svelte
│   ├── overlay/
│   │   ├── Drawer.svelte
│   │   ├── BottomSheet.svelte
│   │   ├── Dialog.svelte
│   │   └── CommandMenu.svelte
│   ├── feedback/
│   │   ├── EmptyState.svelte
│   │   ├── ErrorState.svelte
│   │   ├── Skeleton.svelte
│   │   └── Toast.svelte
│   └── theme/
│       ├── ThemeToggle.svelte
│       └── AccentPicker.svelte
├── stores/
│   ├── theme.store.ts
│   ├── layout.store.ts
│   ├── review.store.ts
│   └── visualization.store.ts
├── theme/
│   ├── tokens.css
│   ├── light.css
│   ├── dark.css
│   └── density.css
└── visualization/
    ├── chart-adapter.ts
    ├── graph-adapter.ts
    ├── chart-formatters.ts
    └── graph-layouts.ts
```

Component rules:

- Feature pages compose components; they do not restyle their internals.
- Shared components expose semantic props rather than arbitrary CSS escape hatches.
- Complex components have Storybook-equivalent local preview fixtures or a
  component showcase route.
- Every component supports loading, empty, error, light, and dark states where relevant.
- Visualization libraries remain hidden behind adapters.
- External-library objects never leak into page-level API contracts.

### 13.12 Performance and Bundle Discipline

External visualization libraries are allowed, but should not punish every page.

Requirements:

- Dynamically import Chart.js and Cytoscape.js.
- Split visualization code into separate chunks.
- Render charts and graphs only when visible.
- Reuse normalized data models.
- Avoid rebuilding graph layouts for unrelated state changes.
- Virtualize or cursor-load large tables.
- Debounce resize and filter operations.
- Preserve page interaction while a graph layout is calculated.
- Set a maximum default node count.
- Use skeletons for delayed visualizations.
- Track bundle size in CI.
- Fail or warn when visualization chunks exceed agreed budgets.

Suggested budgets:

```text
Base panel route:
  under 250 KB compressed JavaScript, excluding optional visualization chunks

Chart chunk:
  under 120 KB compressed target

Graph chunk:
  under 300 KB compressed target

Initial mobile interaction:
  no chart or graph dependency required before the user opens one
```

Actual budgets should be validated against the current build toolchain rather than
treated as sacred numbers carved into a particularly anxious stone tablet.

### 13.13 Accessibility

In addition to the existing panel requirements:

- Charts include a text summary and optional data table.
- Graphs include a relation list fallback.
- Table sorting exposes `aria-sort`.
- Mobile bottom sheets trap focus and restore it on close.
- Theme toggle has an accessible label and current state.
- Accent selection is not represented by color alone.
- Diff additions and removals include text and symbols.
- Live revision updates use polite live regions.
- Touch controls remain keyboard accessible.
- Focus rings use the active accent token with adequate contrast.
- Reduced-motion preference disables nonessential chart and graph animation.

---


## 14. Mutation API

### Reviews

```http
POST   /api/v1/artifacts/{type}/{id}/reviews
GET    /api/v1/reviews/{reviewId}
PATCH  /api/v1/reviews/{reviewId}
DELETE /api/v1/reviews/{reviewId}
```

### Comments

```http
POST   /api/v1/reviews/{reviewId}/comments
PATCH  /api/v1/reviews/{reviewId}/comments/{commentId}
DELETE /api/v1/reviews/{reviewId}/comments/{commentId}
```

### Submission and proposal actions

```http
POST /api/v1/reviews/{reviewId}/submit
POST /api/v1/reviews/{reviewId}/clarifications/{clarificationId}/answer
POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/request-changes
POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/accept
POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/reject
POST /api/v1/reviews/{reviewId}/cancel
```

### Inspection

```http
GET /api/v1/reviews/{reviewId}/timeline
GET /api/v1/reviews/{reviewId}/proposals
GET /api/v1/reviews/{reviewId}/proposals/{proposalId}
GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/diff
GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/validation
```

---

## 15. Mutation Security

Once mutation exists, loopback binding alone is insufficient.

Startup session flow:

```text
1. Generate an ephemeral local panel session secret.
2. Open browser with a one-time bootstrap token.
3. Exchange it for a SameSite=Strict, HttpOnly session cookie.
4. Invalidate the bootstrap token.
5. Require CSRF tokens for every mutation.
```

Requirements:

- Same-origin and Host validation.
- No mutation through GET.
- Expected artifact and review revisions on writes.
- Explicit user action for submit and accept.
- Short session lifetime.
- Session invalidated when the panel stops.
- No credentials in durable URLs.
- Secret redaction before content reaches the browser.
- Complete audit evidence for submission and acceptance.

Authenticated LAN collaboration is a separate future mode requiring TLS and
explicit configuration. It is not implemented by changing the bind address to
`0.0.0.0` and hoping the office network has good manners.

---

## 16. BD and Workflow Integration

On submission:

```text
Parent:
Revise <artifact> from review <review-id>

Children:
1. Load base artifact and review snapshot
2. Resolve related knowledge and evidence
3. Apply review comments
4. Validate proposed artifact
5. Generate diff and resolution report
6. Await user acceptance
7. Commit accepted canonical version
```

The parent remains blocked at `Await user acceptance` until the user acts.

`Request changes` creates another attempt task under the same parent. Rejection
closes the attempt without changing the canonical artifact.

---

## 17. Event Model

```text
review.created
review.comment_added
review.comment_updated
review.submitted
review.queued
review.revision_started
review.clarification_requested
review.clarification_answered
review.proposal_ready
review.proposal_validation_failed
review.changes_requested
review.proposal_accepted
review.proposal_rejected
review.conflicted
review.failed
artifact.version_created
```

Events carry references and status summaries, not full sensitive artifact bodies.

---

## 18. Failure and Recovery

- **Panel stops after submission:** durable request and BD task remain; reconnect on restart.
- **Revision run fails:** show failed phase, checkpoint, evidence, and retry action.
- **Artifact changes during review:** mark conflict and require rebase.
- **Anchor disappears:** attempt fallback resolution, otherwise request re-anchoring.
- **Validation fails:** preserve proposal as evidence but block acceptance.
- **Maximum attempts reached:** stop automatic retries and require a new user submission.

---

## 19. Implementation Phases

### Phase 0: UI Foundation and Artifact Versioning

- Define semantic theme tokens.
- Add light, dark, and system theme resolution.
- Add accent presets.
- Build shared `Card`, `BentoGrid`, `BentoCard`, `DataTable`, layout, overlay,
  feedback, and theme components.
- Add responsive application-shell behavior.
- Define reviewable artifact contracts.
- Add immutable plan versions and `current.yaml`.
- Add stable Markdown block IDs.
- Define review, comment, request, proposal, and resolution schemas.
- Add a component showcase route and visual fixtures.

**Exit:**

- Existing panel routes use the shared theme and shell.
- Light and dark themes render without contrast or flash issues.
- Core components work at mobile, tablet, and desktop widths.
- Plans load by ID and version; previous versions cannot be overwritten.

### Phase 1: Responsive Dashboard and Data Presentation

- Convert overview and summary pages to the shared bento grid.
- Add reusable metric, status, table, chart, and graph cards.
- Add responsive table-to-card-list behavior.
- Add consistent filters, toolbars, empty states, and skeletons.
- Add mobile navigation, drawers, bottom sheets, and sticky action bars.
- Add density controls where large technical datasets need them.

**Exit:**

- Overview and workspace summaries are clear on 360px mobile and desktop.
- No essential table requires page-level horizontal scrolling.
- Shared cards and tables replace feature-specific duplicates.

### Phase 2: Charts and Graph Visualizations

- Add the Chart.js adapter and `ChartShell`.
- Add operational review and revision charts.
- Add the Cytoscape.js adapter and `GraphCanvas`.
- Add artifact, task, revision, and version-lineage graph views.
- Add accessible text and table fallbacks.
- Add lazy loading, resize handling, theme updates, and reduced-motion behavior.
- Add visualization chunk budgets to CI.

**Exit:**

- Charts and graphs are responsive, theme-aware, and optional at route load.
- Every visualization has an accessible nonvisual equivalent.
- Large graphs start from focused, bounded datasets.

### Phase 3: Draft Plan Review

- Add authenticated mutation session.
- Add responsive review mode and anchored comments.
- Persist drafts.
- Add desktop comment rail and mobile bottom sheet.
- Add comment status chips, unsaved state, and sticky Submit Review action.
- Add optimistic draft revision.

**Exit:**

- A user can annotate and resume a plan from desktop or mobile.
- Canonical content remains unchanged.

### Phase 4: Submit and Retrigger

- Freeze the review snapshot.
- Add idempotent submission.
- Dispatch `revise_plan_from_review`.
- Expand BD tasks.
- Add active-revision bento summary.
- Add live timeline, clarification cards, evidence table, status charts, and
  restart recovery.

**Exit:**

- Submit starts exactly one durable revision run.
- Current status remains understandable on mobile and desktop.

### Phase 5: Proposal Review and Acceptance

- Require complete artifact proposals.
- Add responsive block-aware diff.
- Add resolution report and validation summary.
- Add side-by-side desktop and unified mobile diff modes.
- Add request-changes, reject, conflict, rebase, and accept flows.
- Add sticky mobile proposal actions.
- Add version lineage graph.

**Exit:**

- A user can fully review and accept a proposal from desktop or mobile.
- Accepted proposals create immutable plan version N+1.

### Phase 6: Hardening

- Add security, replay, concurrency, stale-anchor, and recovery tests.
- Add visual regression coverage for light, dark, mobile, tablet, and desktop.
- Add keyboard, touch, accessibility, and reduced-motion tests.
- Add chart, graph, and large-table performance tests.
- Add bundle-size enforcement.
- Add user documentation and troubleshooting.

**Exit:**

- Mutation is local, authenticated, versioned, recoverable, accessible, visually
  consistent, and responsive.

---


## 20. Initial Backlog

### UI Foundation

- `UI-001` Define semantic light and dark theme tokens.
- `UI-002` Add system-theme detection and no-flash initialization.
- `UI-003` Add shared `ThemeToggle`.
- `UI-004` Add accent presets and `AccentPicker`.
- `UI-005` Add responsive `AppShell`, sidebar, mobile navigation, and page header.
- `UI-006` Add `ResponsiveToolbar` and `StickyActionBar`.
- `UI-007` Add drawer, dialog, and mobile bottom-sheet primitives.
- `UI-008` Add component showcase and visual fixtures.

### Cards and Data Presentation

- `UI-010` Add shared `Card`.
- `UI-011` Add `BentoGrid` and semantic `BentoCard` sizes.
- `UI-012` Add metric, status, chart, graph, table, review, and diff-summary cards.
- `UI-013` Add typed reusable `DataTable`.
- `UI-014` Add responsive `MobileDataList`.
- `UI-015` Add shared filters, pagination, status badges, skeletons, and empty states.
- `UI-016` Add compact and comfortable density modes.
- `UI-017` Migrate existing panel summaries to shared bento and table components.

### Charts and Graphs

- `UI-020` Add dynamic Chart.js adapter.
- `UI-021` Add `ChartShell` and theme-aware formatters.
- `UI-022` Add review activity and revision outcome charts.
- `UI-023` Add duration, comment-resolution, and change-volume charts.
- `UI-024` Add dynamic Cytoscape.js adapter.
- `UI-025` Add `GraphCanvas`, controls, and focused subgraph loading.
- `UI-026` Add workflow, task, relation, and version-lineage graphs.
- `UI-027` Add chart data-table and graph relation-list fallbacks.
- `UI-028` Add visualization chunk budgets and performance telemetry.

### Artifact Foundation

- `REVIEW-001` Define reviewable artifact contract.
- `REVIEW-002` Add immutable plan storage.
- `REVIEW-003` Add current-version pointer.
- `REVIEW-004` Add stable Markdown block IDs.
- `REVIEW-005` Add artifact revision hash service.

### Persistence and Security

- `REVIEW-010` Define review and comment schemas.
- `REVIEW-011` Define request and proposal schemas.
- `REVIEW-012` Add comments JSONL repository.
- `REVIEW-013` Add proposal and evidence storage.
- `REVIEW-014` Add bootstrap-token exchange.
- `REVIEW-015` Add session cookie and CSRF protection.
- `REVIEW-016` Add optimistic concurrency.
- `REVIEW-017` Add mutation audit events.

### Plan Review and Revision

- `REVIEW-020` Add responsive plan review mode.
- `REVIEW-021` Add section and selected-text comments.
- `REVIEW-022` Add desktop comment rail and mobile comment bottom sheet.
- `REVIEW-023` Add idempotent Submit Review action.
- `REVIEW-024` Add revision workflow dispatcher.
- `REVIEW-025` Add `revise_plan_from_review`.
- `REVIEW-026` Add clarification loop.
- `REVIEW-027` Add restart recovery.
- `REVIEW-028` Add block-aware responsive Markdown diff.
- `REVIEW-029` Add plan validator.
- `REVIEW-030` Add proposal review UI.
- `REVIEW-031` Add request-changes and rejection.
- `REVIEW-032` Add conflict and rebase.
- `REVIEW-033` Add acceptance and version creation.
- `REVIEW-034` Add review status bento summary and timeline.
- `REVIEW-035` Add proposal version-lineage graph.

### Hardening

- `REVIEW-050` Add duplicate-submit and replay tests.
- `REVIEW-051` Add concurrent-review tests.
- `REVIEW-052` Add stale-anchor tests.
- `REVIEW-053` Add mutation-session security tests.
- `REVIEW-054` Add failure-recovery tests.
- `REVIEW-055` Add accessibility audit.
- `REVIEW-056` Add responsive visual regression tests.
- `REVIEW-057` Add touch and mobile viewport tests.
- `REVIEW-058` Add chart and graph performance tests.
- `REVIEW-059` Add bundle-size enforcement.

---


## 21. Acceptance Criteria

### Visual System

- Light, dark, and system themes are available.
- Theme selection persists locally and applies before first paint.
- Accent presets update interactive emphasis without changing semantic status colors.
- All pages use shared semantic color tokens.
- Core components render consistently across themes.
- Both themes meet WCAG AA contrast requirements for normal usage.

### Responsive Experience

- Primary workflows are usable at `360px`, `768px`, `1024px`, and wide desktop widths.
- Sidebar navigation becomes an appropriate mobile navigation pattern.
- Three-pane proposal review becomes a tabbed mobile interface.
- Detail drawers become bottom sheets on mobile.
- Sticky actions keep Submit, Accept, Reject, and Request Changes available without
  obscuring content.
- No essential action requires hover.
- Touch targets are at least `44px`.
- Dense desktop tables transform into readable mobile card rows.

### Cards and Tables

- Overview and revision summary pages use the shared bento grid.
- Bento cards use semantic sizes and respond without page-specific layout hacks.
- Shared cards support loading, empty, warning, and error states.
- All major tabular views use the reusable `DataTable`.
- Sorting, filtering, pagination, and keyboard actions are consistent.
- Large data sets do not block interaction.

### Charts and Graphs

- Chart.js is accessed only through the shared chart adapter.
- Cytoscape.js is accessed only through the shared graph adapter.
- Visualization libraries are lazy-loaded.
- Charts and graphs update correctly when the theme changes.
- Every chart provides a textual summary or tabular fallback.
- Every graph provides an equivalent relation list.
- Graphs cap or progressively expand visible nodes.
- Reduced-motion users do not receive nonessential visualization animation.
- Visualization chunks remain within agreed CI budgets.

### Plans

- User can comment on a plan, section, paragraph, or selected text.
- Draft comments survive restart.
- Submit creates an immutable snapshot and exactly one revision run.
- Punakawan uses the exact base artifact version and comment snapshot.
- The panel shows progress and clarification questions.
- Punakawan returns a complete proposal.
- Every comment has a resolution.
- User can inspect a block-aware diff on desktop and mobile.
- User can accept, reject, or request changes.
- Acceptance creates a new immutable version and preserves the old version.

### Safety

- Panel never directly overwrites canonical content.
- Mutation requires an authenticated local session and CSRF token.
- Duplicate submissions are idempotent.
- Stale revisions cannot be accepted silently.
- Arbitrary code execution is impossible through comments.
- Accepted changes are attributable to a review, proposal, and user action.

---


## 22. Definition of Done

```text
A user opens the responsive Punakawan panel on desktop or mobile,
uses a clean bento-based dashboard with reusable cards, tables, charts,
and focused connector graphs,
selects a light or dark theme with a consistent accent,
opens a versioned plan,
adds anchored review comments,
submits them,
Punakawan automatically starts one durable revision workflow,
shows progress through responsive summaries and timelines,
produces a complete validated proposal,
explains how every comment was handled,
shows an accessible responsive diff,
and creates a new immutable canonical version only after user acceptance.

The implementation uses shared components and visualization adapters,
keeps optional libraries out of the initial route,
supports keyboard and touch,
and preserves equivalent nonvisual access to chart and graph information.

New Plan 2 later extends this accepted review protocol to retrieval recipes.
That extension adds recompilation, provider testing, and result comparison
before a recipe version can affect future workflows.
```
