<script lang="ts">
  import { onMount } from "svelte";
  import { listApprovals, type ApprovalRecord } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";

  interface Props {
    workspaceId: string;
  }
  let { workspaceId }: Props = $props();

  let status = $state("");
  let records: ApprovalRecord[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);
  let copiedId: string | null = $state(null);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const res = await listApprovals(id, status || undefined);
      records = res.items;
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

  async function copy(command: string, id: string) {
    try {
      await navigator.clipboard.writeText(command);
      copiedId = id;
      setTimeout(() => {
        if (copiedId === id) copiedId = null;
      }, 1500);
    } catch {
      // Clipboard access can be denied by the browser; the command is
      // still visible to select and copy by hand.
    }
  }
</script>

<h1>Approvals</h1>
<p class="hint">
  The panel is read-only and cannot approve or deny anything itself. Each pending request shows the exact
  <code>punakawan approvals</code> command to run in a terminal to resolve it.
</p>

<label class="filter">
  Status
  <select bind:value={status} onchange={() => load(workspaceId)}>
    <option value="">Any</option>
    <option value="pending">Pending</option>
    <option value="approved">Approved</option>
    <option value="denied">Denied</option>
  </select>
</label>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load approvals: {error}</p>
{:else if records.length === 0}
  <p>No approval requests match these filters.</p>
{:else}
  <ul>
    {#each records as rec (rec.id)}
      <li class="row">
        <div class="row-head">
          <span class="op">{rec.operation}</span>
          <span class="status status-{rec.status}">{rec.status}</span>
          <span class="requested-by">requested by {rec.requested_by}</span>
        </div>
        {#if rec.target}<p class="target">{rec.target}</p>{/if}
        {#if rec.reason}<p class="reason">{rec.reason}</p>{/if}
        {#if rec.preview}<pre class="preview">{rec.preview}</pre>{/if}
        <p class="meta">created {rec.created_at}{#if rec.resolved_at} · resolved {rec.resolved_at} by {rec.approved_by}{/if}</p>
        {#if rec.approve_command && rec.deny_command}
          <div class="commands">
            <div class="command-row">
              <code>{rec.approve_command}</code>
              <button type="button" onclick={() => copy(rec.approve_command ?? "", rec.id + "-approve")}>
                {copiedId === rec.id + "-approve" ? "Copied" : "Copy"}
              </button>
            </div>
            <div class="command-row">
              <code>{rec.deny_command}</code>
              <button type="button" onclick={() => copy(rec.deny_command ?? "", rec.id + "-deny")}>
                {copiedId === rec.id + "-deny" ? "Copied" : "Copy"}
              </button>
            </div>
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  h1 {
    font-size: 1.2rem;
    margin-bottom: 0.2rem;
  }
  .hint {
    color: #666;
    font-size: 0.85rem;
    margin-top: 0;
  }
  .filter {
    display: inline-grid;
    gap: 0.2rem;
    font-size: 0.8rem;
    color: #444;
    margin-bottom: 1rem;
  }
  select {
    font-size: 0.85rem;
    padding: 0.3rem 0.4rem;
    border: 1px solid #ccc;
    border-radius: 6px;
  }
  ul {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.6rem;
  }
  .row {
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.6rem 0.8rem;
    display: grid;
    gap: 0.3rem;
  }
  .row-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .op {
    font-weight: 600;
  }
  .status {
    font-size: 0.75rem;
    padding: 0.05rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  .status-pending {
    background: #fff4e5;
    color: #9a6700;
  }
  .status-approved {
    background: #e6f4ea;
    color: #1e7d32;
  }
  .status-denied {
    background: #fdecea;
    color: #c62828;
  }
  .requested-by {
    margin-left: auto;
    font-size: 0.75rem;
    color: #666;
  }
  .target,
  .reason {
    margin: 0;
    font-size: 0.85rem;
    color: #333;
  }
  .preview {
    margin: 0;
    font-size: 0.75rem;
    background: #f7f7f7;
    padding: 0.4rem;
    border-radius: 4px;
    overflow-x: auto;
    white-space: pre-wrap;
  }
  .meta {
    margin: 0;
    font-size: 0.75rem;
    color: #888;
  }
  .commands {
    display: grid;
    gap: 0.3rem;
    margin-top: 0.2rem;
  }
  .command-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .command-row code {
    flex: 1;
    font-size: 0.78rem;
    background: #f0f0f0;
    padding: 0.3rem 0.5rem;
    border-radius: 4px;
    overflow-x: auto;
    white-space: pre;
  }
  .command-row button {
    font-size: 0.78rem;
    padding: 0.3rem 0.6rem;
    border: 1px solid #3949ab;
    background: white;
    color: #3949ab;
    border-radius: 6px;
    cursor: pointer;
  }
  .error {
    color: #b00020;
  }
</style>
