<script lang="ts">
  import { onMount } from "svelte";
  import { getHealth, refreshHealth, type Availability, type HealthResponse } from "../../lib/api/client";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let data: HealthResponse | null = $state(null);
  let loading = $state(true);
  let error: string | null = $state(null);
  // A forced refresh is a mutation (POST) distinct from the initial load,
  // so it gets its own busy flag + error slot: a failed refresh must not
  // wipe the health table already on screen.
  let refreshing = $state(false);
  let refreshError: string | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      data = await getHealth(projectId);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function refresh() {
    refreshing = true;
    refreshError = null;
    try {
      data = await refreshHealth(projectId);
    } catch (e) {
      refreshError = e instanceof Error ? e.message : String(e);
    } finally {
      refreshing = false;
    }
  }

  onMount(load);

  function formatTime(iso: string): string {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  }
</script>

<section aria-label="Project health">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load health" message={error} />
  {:else if data}
    <div class="toolbar">
      <div class="status-line">
        {#if data.stale}
          <span class="stale" role="status" data-testid="stale-indicator">
            <span aria-hidden="true">⚠</span> Showing a cached snapshot — may be stale
          </span>
        {:else}
          <span class="fresh" data-testid="fresh-indicator">Up to date</span>
        {/if}
      </div>
      <button
        type="button"
        class="btn primary"
        onclick={refresh}
        disabled={refreshing}
        data-testid="refresh-health"
      >
        {refreshing ? "Refreshing…" : "Refresh"}
      </button>
    </div>

    {#if refreshError}
      <p class="error" role="alert" data-testid="refresh-error">Refresh failed: {refreshError}</p>
    {/if}

    {#if data.health.length === 0}
      <EmptyStateCard
        title="No health sources"
        message="This project has no data sources reporting health yet."
      />
    {:else}
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">Source</th>
              <th scope="col">Availability</th>
              <th scope="col">Message</th>
              <th scope="col">Checked</th>
            </tr>
          </thead>
          <tbody>
            {#each data.health as h (h.source)}
              <tr>
                <td class="source">{h.source}</td>
                <td><StatusBadge availability={h.availability as Availability} /></td>
                <td class="message">{h.message ?? "—"}</td>
                <td class="checked">{formatTime(h.checked_at)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</section>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
  }
  .status-line {
    font-size: 0.85rem;
  }
  .stale {
    color: var(--color-warning);
    font-weight: 600;
  }
  .fresh {
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .btn {
    font: inherit;
    font-weight: 600;
    font-size: 0.82rem;
    padding: 0.4rem 0.8rem;
    min-height: 40px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-strong);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    border-color: var(--color-accent);
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .btn.primary {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .table-scroll {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }
  th,
  td {
    text-align: left;
    padding: 0.55rem 0.65rem;
    border-bottom: 1px solid var(--color-border);
    vertical-align: top;
  }
  th {
    color: var(--color-text-muted);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  td.source {
    font-weight: 600;
    white-space: nowrap;
  }
  td.message {
    color: var(--color-text-muted);
    word-break: break-word;
  }
  td.checked {
    color: var(--color-text-muted);
    white-space: nowrap;
  }
</style>
