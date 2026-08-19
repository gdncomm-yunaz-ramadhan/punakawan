<script lang="ts">
  import { onMount, untrack } from "svelte";
  import {
    listProjectTasks,
    getProjectTask,
    getProjectTaskGraph,
    type TaskGraph,
    type TaskSummary,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import TasksBoard from "../tasks/TasksBoard.svelte";
  import TasksTable from "../tasks/TasksTable.svelte";
  import TaskDetailDrawer from "../tasks/TaskDetailDrawer.svelte";
  import TaskGraphView from "../../lib/components/graphs/TaskGraphView.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import type { GraphEdge, GraphNode } from "../../lib/components/graphs/types";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  type View = "board" | "table" | "graph";
  let view: View = $state("table");

  let tasks: TaskSummary[] = $state([]);
  let graph: TaskGraph | null = $state(null);
  let loading = $state(true);
  let error: string | null = $state(null);
  let selectedTaskId: string | null = $state(null);

  let statusFilter = $state("");
  let priorityFilter = $state("");
  let query = $state("");

  // Both endpoints are fetched together so the list and the dependency graph
  // can never disagree about which tasks exist, and so the initial load and a
  // live refresh stay in step with each other.
  async function fetchTasks(id: string) {
    const [tasksRes, graphRes] = await Promise.all([
      listProjectTasks(id, { status: statusFilter || undefined, priority: priorityFilter || undefined, query: query || undefined }),
      getProjectTaskGraph(id),
    ]);
    tasks = tasksRes.items ?? [];
    graph = graphRes;
  }

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      await fetchTasks(id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  // A live refresh keeps `loading` untouched so the board/table/graph the user
  // is looking at is not replaced by a spinner, and the chosen view and open
  // task drawer survive the update.
  async function refresh(id: string) {
    error = null;
    try {
      await fetchTasks(id);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  // Only task writes change this view's list, counts, and dependency edges.
  const refreshOn = new Set(["task.created", "task.updated", "task.blocked", "task.completed"]);

  onMount(() => {
    return onPanelEvent((evt) => {
      if (!refreshOn.has(evt.type)) return;
      refresh(projectId);
    });
  });
  // Single trigger for the first load and for a project change - effects run
  // after the first render too, so loading from onMount as well would fire the
  // same pair of requests twice per open. The two dropdown filters are read
  // here so they re-run the load; the free-text search deliberately is not,
  // because it updates on every keystroke and would then fire a request per
  // character - it refetches from its own change handler instead. load()'s own
  // reads are untracked so they cannot re-add that dependency implicitly.
  $effect(() => {
    const id = projectId;
    statusFilter;
    priorityFilter;
    untrack(() => load(id));
  });

  // Adapt the API task-graph shape to the presentation GraphNode/GraphEdge
  // the Cytoscape TaskGraphView consumes; board_status rides in node.data
  // so the canvas can tint by state.
  const nodes = $derived.by<GraphNode[]>(() => {
    const g = graph;
    if (!g) return [];
    return (g.nodes ?? []).map((n) => ({
      id: n.id,
      label: n.title || n.id,
      type: n.issue_type,
      data: { status: n.board_status },
    }));
  });
  const edges = $derived.by<GraphEdge[]>(() => {
    const g = graph;
    if (!g) return [];
    return (g.edges ?? []).map((e, i) => ({
      id: `${e.from}->${e.to}-${i}`,
      source: e.from,
      target: e.to,
      type: e.type,
    }));
  });

  const hasActiveFilter = $derived(Boolean(statusFilter || priorityFilter || query));
</script>

<section aria-label="Project tasks">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load tasks" message={error} />
  {:else if tasks.length === 0 && !hasActiveFilter}
    <EmptyStateCard
      title="No tasks yet"
      message="Tasks and their dependencies appear here once this project has an active workflow."
    />
  {:else}
    <div class="toolbar">
      <div class="views" role="tablist" aria-label="Task view">
        <button type="button" role="tab" aria-selected={view === "board"} class:active={view === "board"} onclick={() => (view = "board")}>
          Board
        </button>
        <button type="button" role="tab" aria-selected={view === "table"} class:active={view === "table"} onclick={() => (view = "table")}>
          Table
        </button>
        <button type="button" role="tab" aria-selected={view === "graph"} class:active={view === "graph"} onclick={() => (view = "graph")}>
          Dependency graph
        </button>
      </div>
      <div class="filters">
        <input
          type="search"
          placeholder="Search title or description"
          bind:value={query}
          onchange={() => load(projectId)}
        />
        <select bind:value={statusFilter}>
          <option value="">Any status</option>
          <option value="open">Open</option>
          <option value="in_progress">In progress</option>
          <option value="blocked">Blocked</option>
          <option value="deferred">Deferred</option>
          <option value="closed">Closed</option>
        </select>
        <select bind:value={priorityFilter}>
          <option value="">Any priority</option>
          <option value="0">P0</option>
          <option value="1">P1</option>
          <option value="2">P2</option>
          <option value="3">P3</option>
          <option value="4">P4</option>
        </select>
      </div>
      <span class="count">{tasks.length} task{tasks.length === 1 ? "" : "s"}</span>
    </div>

    {#if graph && (graph.cycles ?? []).length > 0}
      <p class="cycle-warning" role="alert">
        {(graph.cycles ?? []).length} dependency cycle(s) detected: {(graph.cycles ?? []).map((c) => c.join(" → ")).join("; ")}
      </p>
    {/if}

    {#if view === "board"}
      <TasksBoard {tasks} onselect={(id) => (selectedTaskId = id)} />
    {:else if view === "table"}
      <TasksTable {tasks} onselect={(id) => (selectedTaskId = id)} />
    {:else}
      <TaskGraphView {nodes} {edges} />
    {/if}
  {/if}

  {#if selectedTaskId}
    <TaskDetailDrawer
      taskId={selectedTaskId}
      fetchTask={(id) => getProjectTask(projectId, id)}
      onclose={() => (selectedTaskId = null)}
    />
  {/if}
</section>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }
  .views {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    background: var(--color-surface-subtle);
  }
  .views button {
    border: none;
    background: transparent;
    color: var(--color-text-muted);
    font: inherit;
    font-size: 0.85rem;
    padding: 0.35rem 0.8rem;
    border-radius: 6px;
    cursor: pointer;
    min-height: 34px;
  }
  .views button.active {
    background: var(--color-surface-raised);
    color: var(--color-text);
    font-weight: 600;
    box-shadow: var(--shadow-card);
  }
  .filters {
    display: flex;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .filters input,
  .filters select {
    font-size: 0.85rem;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
  }
  .count {
    font-size: 0.82rem;
    color: var(--color-text-muted);
  }
  .cycle-warning {
    margin: 0 0 0.75rem;
    background: var(--color-accent-soft);
    color: var(--color-danger);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
  }
</style>
