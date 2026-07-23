<script lang="ts">
  import type { TaskSummary } from "../../lib/api/client";
  import TaskCard from "./TaskCard.svelte";

  interface Props {
    tasks: TaskSummary[];
    onselect: (id: string) => void;
  }
  let { tasks, onselect }: Props = $props();

  // bd has no "review" or "failed" issue status (only
  // open/in_progress/blocked/deferred/closed), so those two columns of
  // the plan's seven-column board are structurally always empty - an
  // honest gap in the underlying data model, kept visible rather than
  // dropping the columns and implying they don't exist.
  const COLUMNS: { key: string; label: string }[] = [
    { key: "pending", label: "Pending" },
    { key: "ready", label: "Ready" },
    { key: "active", label: "Active" },
    { key: "blocked", label: "Blocked" },
    { key: "review", label: "Review" },
    { key: "completed", label: "Completed" },
    { key: "failed", label: "Failed" },
  ];

  function columnTasks(key: string): TaskSummary[] {
    return tasks.filter((t) => t.board_status === key);
  }
</script>

<div class="board" role="list" aria-label="Task status board">
  {#each COLUMNS as col (col.key)}
    {@const items = columnTasks(col.key)}
    <div class="column" role="listitem" aria-label={col.label}>
      <h3>{col.label} <span class="count">{items.length}</span></h3>
      <div class="cards">
        {#each items as task (task.id)}
          <TaskCard {task} {onselect} />
        {:else}
          <p class="empty">No tasks.</p>
        {/each}
      </div>
    </div>
  {/each}
</div>

<style>
  .board {
    display: flex;
    gap: 0.75rem;
    overflow-x: auto;
    padding-bottom: 0.5rem;
  }
  .column {
    min-width: 220px;
    flex: 1;
  }
  h3 {
    font-size: 0.85rem;
    display: flex;
    justify-content: space-between;
    color: var(--color-text-muted);
    margin: 0 0 0.5rem;
  }
  .count {
    color: var(--color-text-muted);
  }
  .cards {
    display: grid;
    gap: 0.5rem;
  }
  .empty {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin: 0;
  }
</style>
