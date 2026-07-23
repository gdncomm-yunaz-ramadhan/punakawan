<script lang="ts">
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ThemeToggle from "../../lib/components/ThemeToggle.svelte";
  import AccentPicker from "../../lib/components/AccentPicker.svelte";
  import ResponsiveToolbar, { type ToolbarAction } from "../../lib/components/ResponsiveToolbar.svelte";
  import StickyActionBar from "../../lib/components/StickyActionBar.svelte";
  import Drawer from "../../lib/components/overlay/Drawer.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import BottomSheet from "../../lib/components/overlay/BottomSheet.svelte";
  import ReviewActivityChart from "../../lib/components/charts/ReviewActivityChart.svelte";
  import RevisionOutcomeChart from "../../lib/components/charts/RevisionOutcomeChart.svelte";
  import DurationChart from "../../lib/components/charts/DurationChart.svelte";
  import CommentResolutionChart from "../../lib/components/charts/CommentResolutionChart.svelte";
  import ChangeVolumeChart from "../../lib/components/charts/ChangeVolumeChart.svelte";
  import WorkflowGraphView from "../../lib/components/graphs/WorkflowGraphView.svelte";
  import TaskGraphView from "../../lib/components/graphs/TaskGraphView.svelte";
  import RelationGraphView from "../../lib/components/graphs/RelationGraphView.svelte";
  import VersionLineageGraphView from "../../lib/components/graphs/VersionLineageGraphView.svelte";
  import type { GraphEdge, GraphNode } from "../../lib/components/graphs/types";

  let drawerOpen = $state(false);
  let dialogOpen = $state(false);
  let sheetOpen = $state(false);
  let lastAction: string | null = $state(null);

  const toolbarActions: ToolbarAction[] = [
    { id: "approve", label: "Approve", onSelect: () => (lastAction = "Approve") },
    { id: "reject", label: "Reject", onSelect: () => (lastAction = "Reject") },
    { id: "comment", label: "Comment", onSelect: () => (lastAction = "Comment") },
    { id: "assign", label: "Assign", onSelect: () => (lastAction = "Assign") },
    { id: "flag", label: "Flag", onSelect: () => (lastAction = "Flag") },
  ];

  // Representative sample data for Phase 2's chart/graph components -
  // Chart.js and Cytoscape.js are lazy-loaded from inside ChartShell/
  // GraphCanvas only once this section actually mounts (i.e. only when
  // /showcase, or any other chart/graph-bearing route, is visited).
  const activityPoints = [
    { period: "Jul 17", reviewsCreated: 3, commentsAdded: 8, submissions: 2 },
    { period: "Jul 18", reviewsCreated: 5, commentsAdded: 11, submissions: 3 },
    { period: "Jul 19", reviewsCreated: 2, commentsAdded: 6, submissions: 1 },
    { period: "Jul 20", reviewsCreated: 6, commentsAdded: 14, submissions: 4 },
    { period: "Jul 21", reviewsCreated: 4, commentsAdded: 9, submissions: 2 },
  ];

  const revisionOutcomeCounts = { accepted: 14, rejected: 3, superseded: 2 };

  const durationBuckets = [
    { bucket: "< 1h", count: 12 },
    { bucket: "1-4h", count: 9 },
    { bucket: "4-24h", count: 5 },
    { bucket: "1-3d", count: 3 },
    { bucket: "> 3d", count: 1 },
  ];

  const commentResolutionSnapshots = [
    { period: "Week 1", open: 6, addressed: 4, resolved: 10, wontfix: 1 },
    { period: "Week 2", open: 3, addressed: 5, resolved: 14, wontfix: 0 },
    { period: "Week 3", open: 4, addressed: 2, resolved: 16, wontfix: 2 },
  ];

  const changeVolumePoints = [
    { revision: "v1", added: 120, removed: 12, modified: 34 },
    { revision: "v2", added: 45, removed: 60, modified: 18 },
    { revision: "v3", added: 22, removed: 5, modified: 41 },
  ];

  const workflowNodes: GraphNode[] = [
    { id: "wf-start", label: "Run started", type: "step" },
    { id: "wf-plan", label: "Plan task", type: "step" },
    { id: "wf-review", label: "Review", type: "step" },
    { id: "wf-gate", label: "Approval gate", type: "gate" },
    { id: "wf-merge", label: "Commit", type: "step" },
  ];
  const workflowEdges: GraphEdge[] = [
    { id: "wf-e1", source: "wf-start", target: "wf-plan", label: "then" },
    { id: "wf-e2", source: "wf-plan", target: "wf-review", label: "then" },
    { id: "wf-e3", source: "wf-review", target: "wf-gate", label: "then" },
    { id: "wf-e4", source: "wf-gate", target: "wf-merge", label: "then" },
  ];

  const taskNodes: GraphNode[] = [
    { id: "task-1", label: "Design schema", type: "task" },
    { id: "task-2", label: "Implement store", type: "task" },
    { id: "task-3", label: "Add CLI", type: "task" },
    { id: "task-4", label: "Write tests", type: "task" },
  ];
  const taskEdges: GraphEdge[] = [
    { id: "task-e1", source: "task-2", target: "task-1", type: "dependency", label: "depends on" },
    { id: "task-e2", source: "task-3", target: "task-2", type: "dependency", label: "depends on" },
    { id: "task-e3", source: "task-4", target: "task-2", type: "dependency", label: "depends on" },
  ];

  const relationNodes: GraphNode[] = [
    { id: "rec-a", label: "Retrieval recipe A", type: "artifact" },
    { id: "rec-b", label: "Retrieval recipe B", type: "artifact" },
    { id: "rec-c", label: "Knowledge record C", type: "artifact" },
  ];
  const relationEdges: GraphEdge[] = [
    { id: "rel-e1", source: "rec-a", target: "rec-b", type: "supersedes", label: "supersedes" },
    { id: "rel-e2", source: "rec-b", target: "rec-c", type: "derived_from", label: "derived from" },
  ];

  const versionNodes: GraphNode[] = [
    { id: "v1", label: "v1", type: "version" },
    { id: "v2", label: "v2", type: "version" },
    { id: "v3", label: "v3", type: "version" },
    { id: "v4", label: "v4", type: "version" },
  ];
  const versionEdges: GraphEdge[] = [
    { id: "ver-e1", source: "v2", target: "v1", label: "derived from" },
    { id: "ver-e2", source: "v3", target: "v2", label: "derived from" },
    { id: "ver-e3", source: "v4", target: "v2", label: "derived from" },
  ];
</script>

<!--
  Dev/QA tool (UI-008): one instance of every new Phase 0 UI-foundation
  component, in both themes, so a human can visually verify them without
  reading source. Not part of the operational nav flow.
-->
<PageHeader
  title="Component Showcase"
  description="One instance of every new UI-foundation component (theme, accent, overlays, toolbars). Dev/QA use only."
/>

<section aria-labelledby="theme-heading">
  <h2 id="theme-heading">Theme</h2>
  <ThemeToggle />
</section>

<section aria-labelledby="accent-heading">
  <h2 id="accent-heading">Accent</h2>
  <AccentPicker />
</section>

<section aria-labelledby="toolbar-heading">
  <h2 id="toolbar-heading">ResponsiveToolbar</h2>
  <ResponsiveToolbar actions={toolbarActions} visibleCount={2} />
  {#if lastAction}
    <p class="muted" data-testid="last-action">Last action: {lastAction}</p>
  {/if}
</section>

<section aria-labelledby="overlay-heading">
  <h2 id="overlay-heading">Overlays</h2>
  <div class="button-row">
    <button type="button" onclick={() => (drawerOpen = true)}>Open Drawer</button>
    <button type="button" onclick={() => (dialogOpen = true)}>Open Dialog</button>
    <button type="button" onclick={() => (sheetOpen = true)}>Open BottomSheet</button>
  </div>
</section>

<Drawer open={drawerOpen} title="Drawer example" onclose={() => (drawerOpen = false)}>
  <p>This is a Drawer primitive: side-anchored, closes on backdrop click or Escape.</p>
</Drawer>

<Dialog open={dialogOpen} title="Dialog example" onclose={() => (dialogOpen = false)}>
  <p>This is a Dialog primitive: centered, closes on backdrop click or Escape.</p>
</Dialog>

<BottomSheet open={sheetOpen} title="Bottom sheet example" onclose={() => (sheetOpen = false)}>
  <p>This is a BottomSheet primitive: bottom-anchored with rounded top corners and a drag-handle affordance.</p>
</BottomSheet>

<section aria-labelledby="sticky-heading">
  <h2 id="sticky-heading">StickyActionBar</h2>
  <p class="muted">Sticks to the bottom of the viewport below 640px width; renders inline above that.</p>
  <StickyActionBar>
    <button type="button" onclick={() => (lastAction = "Sticky primary")}>Primary action</button>
    <button type="button" onclick={() => (lastAction = "Sticky secondary")}>Secondary</button>
  </StickyActionBar>
</section>

<section aria-labelledby="charts-heading">
  <h2 id="charts-heading">Charts (Chart.js, lazy-loaded)</h2>
  <p class="muted">
    Chart.js is dynamically imported the moment this section mounts. Each chart has a "View as data table"
    toggle exposing the same data as an accessible table.
  </p>

  <h3>Review activity</h3>
  <ReviewActivityChart points={activityPoints} />

  <h3>Revision outcomes</h3>
  <RevisionOutcomeChart counts={revisionOutcomeCounts} />

  <h3>Cycle duration distribution</h3>
  <DurationChart buckets={durationBuckets} />

  <h3>Comment resolution over time</h3>
  <CommentResolutionChart snapshots={commentResolutionSnapshots} />

  <h3>Change volume per revision</h3>
  <ChangeVolumeChart points={changeVolumePoints} />
</section>

<section aria-labelledby="graphs-heading">
  <h2 id="graphs-heading">Graphs (Cytoscape.js, lazy-loaded)</h2>
  <p class="muted">
    Cytoscape.js is dynamically imported the moment this section mounts. Each graph view has a relation-list
    fallback (visually hidden by default) enumerating every edge currently in view.
  </p>

  <h3>Workflow graph</h3>
  <WorkflowGraphView nodes={workflowNodes} edges={workflowEdges} />

  <h3>Task dependency graph</h3>
  <TaskGraphView nodes={taskNodes} edges={taskEdges} />

  <h3>Relation graph</h3>
  <RelationGraphView nodes={relationNodes} edges={relationEdges} />

  <h3>Version lineage</h3>
  <VersionLineageGraphView nodes={versionNodes} edges={versionEdges} />
</section>

<style>
  section {
    margin-bottom: 1.75rem;
  }
  h2 {
    font-size: 0.95rem;
    margin: 0 0 0.5rem;
    color: var(--color-text);
  }
  h3 {
    font-size: 0.85rem;
    margin: 1rem 0 0.4rem;
    color: var(--color-text-muted);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .button-row {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .button-row button {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.4rem 0.75rem;
    cursor: pointer;
    min-height: 44px;
  }
</style>
