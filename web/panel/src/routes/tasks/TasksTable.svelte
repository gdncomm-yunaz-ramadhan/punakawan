<script lang="ts">
  import type { TaskSummary } from "../../lib/api/client";

  interface Props {
    tasks: TaskSummary[];
    onselect: (id: string) => void;
  }
  let { tasks, onselect }: Props = $props();
</script>

{#if tasks.length === 0}
  <p>No tasks match these filters.</p>
{:else}
  <table>
    <thead>
      <tr>
        <th scope="col">ID</th>
        <th scope="col">Title</th>
        <th scope="col">Status</th>
        <th scope="col">Priority</th>
        <th scope="col">Dependencies</th>
        <th scope="col">External ref</th>
        <th scope="col">Updated</th>
      </tr>
    </thead>
    <tbody>
      {#each tasks as t (t.id)}
        <tr class="row" onclick={() => onselect(t.id)}>
          <td>{t.id}</td>
          <td>{t.title}</td>
          <td>
            <span class="status status-{t.board_status}">{t.board_status}</span>
            {#if t.stale}<span class="stale-tag">stale</span>{/if}
          </td>
          <td>P{t.priority}</td>
          <td>{t.dependencies?.length ?? 0}</td>
          <td>{t.external_ref ?? "—"}</td>
          <td>{new Date(t.updated_at).toLocaleDateString()}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9rem;
  }
  th {
    text-align: left;
    color: #666;
    font-weight: 500;
    font-size: 0.8rem;
    padding: 0.4rem 0.6rem;
    border-bottom: 1px solid #ddd;
  }
  td {
    padding: 0.5rem 0.6rem;
    border-bottom: 1px solid #eee;
  }
  .row {
    cursor: pointer;
  }
  .row:hover {
    background: #f7f7f7;
  }
  .status {
    font-size: 0.75rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  .status-ready {
    background: #e6f4ea;
    color: #1e7d32;
  }
  .status-blocked {
    background: #fdecea;
    color: #c62828;
  }
  .status-active {
    background: #e8eaf6;
    color: #3949ab;
  }
  .stale-tag {
    margin-left: 0.35rem;
    font-size: 0.7rem;
    color: #9a6700;
  }
</style>
