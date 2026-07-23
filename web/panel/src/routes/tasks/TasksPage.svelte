<script lang="ts">
  import { onMount } from "svelte";
  import { getTaskGraph, listTasks, type TaskGraph, type TaskSummary } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import TasksBoard from "./TasksBoard.svelte";
  import TasksTable from "./TasksTable.svelte";
  import TaskGraphView from "./TaskGraph.svelte";
  import TaskDetailDrawer from "./TaskDetailDrawer.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  type View = "board" | "table" | "graph";
  let view: View = $state("board");

  let tasks: TaskSummary[] = $state([]);
  let graph: TaskGraph | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let selectedTaskId: string | null = $state(null);

  let statusFilter = $state("");
  let priorityFilter = $state("");
  let query = $state("");

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const [tasksRes, graphRes] = await Promise.all([
        listTasks(id, { status: statusFilter || undefined, priority: priorityFilter || undefined, query: query || undefined }),
        getTaskGraph(id),
      ]);
      tasks = tasksRes.items;
      graph = graphRes;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load(workspaceId);
    return onPanelEvent(() => load(workspaceId));
  });
  $effect(() => {
    load(workspaceId);
  });
</script>

<div class="toolbar">
  <div class="views" role="tablist" aria-label="Task view">
    <button type="button" class:active={view === "board"} onclick={() => (view = "board")}>Board</button>
    <button type="button" class:active={view === "table"} onclick={() => (view = "table")}>Table</button>
    <button type="button" class:active={view === "graph"} onclick={() => (view = "graph")}>Dependency Graph</button>
  </div>
  <div class="filters">
    <input type="search" placeholder="Search title or description" bind:value={query} onchange={() => load(workspaceId)} />
    <select bind:value={statusFilter} onchange={() => load(workspaceId)}>
      <option value="">Any status</option>
      <option value="open">Open</option>
      <option value="in_progress">In progress</option>
      <option value="blocked">Blocked</option>
      <option value="deferred">Deferred</option>
      <option value="closed">Closed</option>
    </select>
    <select bind:value={priorityFilter} onchange={() => load(workspaceId)}>
      <option value="">Any priority</option>
      <option value="0">P0</option>
      <option value="1">P1</option>
      <option value="2">P2</option>
      <option value="3">P3</option>
      <option value="4">P4</option>
    </select>
  </div>
</div>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load tasks: {error}</p>
{:else if view === "board"}
  <TasksBoard {tasks} onselect={(id) => (selectedTaskId = id)} />
{:else if view === "table"}
  <TasksTable {tasks} onselect={(id) => (selectedTaskId = id)} />
{:else if view === "graph" && graph}
  <TaskGraphView {graph} onselect={(id) => (selectedTaskId = id)} />
{/if}

{#if selectedTaskId}
  <TaskDetailDrawer {workspaceId} taskId={selectedTaskId} onclose={() => (selectedTaskId = null)} />
{/if}

<style>
  .toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }
  .views {
    display: flex;
    gap: 0.3rem;
  }
  .views button {
    border: 1px solid #ddd;
    background: white;
    padding: 0.3rem 0.7rem;
    border-radius: 6px;
    font-size: 0.85rem;
    cursor: pointer;
  }
  .views button.active {
    background: #e8eaf6;
    border-color: #3949ab;
    color: #3949ab;
  }
  .filters {
    display: flex;
    gap: 0.4rem;
  }
  .filters input,
  .filters select {
    font-size: 0.85rem;
    padding: 0.3rem 0.5rem;
    border: 1px solid #ccc;
    border-radius: 6px;
  }
  .error {
    color: #b00020;
  }
</style>
