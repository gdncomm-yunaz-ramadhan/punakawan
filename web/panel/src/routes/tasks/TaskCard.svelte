<script lang="ts">
  import type { TaskSummary } from "../../lib/api/client";

  interface Props {
    task: TaskSummary;
    onselect: (id: string) => void;
  }
  let { task, onselect }: Props = $props();

  const priorityLabels: Record<number, string> = { 0: "P0", 1: "P1", 2: "P2", 3: "P3", 4: "P4" };
</script>

<button type="button" class="card" class:stale={task.stale} onclick={() => onselect(task.id)}>
  <div class="row">
    <span class="id">{task.id}</span>
    <span class="priority">{priorityLabels[task.priority] ?? `P${task.priority}`}</span>
  </div>
  <p class="title">{task.title}</p>
  <div class="row meta">
    {#if task.external_ref}<span class="chip">{task.external_ref}</span>{/if}
    {#if task.dependencies?.length}<span class="chip">{task.dependencies.length} dep(s)</span>{/if}
    {#if task.stale}<span class="chip stale-chip">Stale</span>{/if}
  </div>
  {#if task.blocking_reasons?.length}
    <p class="blocking">{task.blocking_reasons[0]}</p>
  {/if}
  <p class="updated">Updated {new Date(task.updated_at).toLocaleDateString()}</p>
</button>

<style>
  .card {
    width: 100%;
    text-align: left;
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.6rem 0.7rem;
    background: white;
    cursor: pointer;
    font: inherit;
    display: grid;
    gap: 0.25rem;
  }
  .card:hover {
    border-color: #3949ab;
  }
  .card.stale {
    border-left: 3px solid #9a6700;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.4rem;
  }
  .id {
    font-size: 0.75rem;
    color: #666;
  }
  .priority {
    font-size: 0.7rem;
    font-weight: 600;
    color: #3949ab;
  }
  .title {
    margin: 0;
    font-size: 0.9rem;
  }
  .meta {
    justify-content: flex-start;
    flex-wrap: wrap;
  }
  .chip {
    font-size: 0.7rem;
    background: #f0f0f0;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
  }
  .stale-chip {
    background: #fff4e5;
    color: #9a6700;
  }
  .blocking {
    margin: 0;
    font-size: 0.75rem;
    color: #c62828;
  }
  .updated {
    margin: 0;
    font-size: 0.7rem;
    color: #999;
  }
</style>
