<script lang="ts">
  import { onMount } from "svelte";
  import { listDeliveries, getDeliveryView, type DeliveryOrchestration, type DeliveryView } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Icon from "../../lib/components/Icon.svelte";

  interface DeliveryRow {
    orchestration: DeliveryOrchestration;
    view: DeliveryView | null;
    viewError: string | null;
  }

  let rows: DeliveryRow[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  // listDeliveries only returns the bare orchestration records (id,
  // status, revision, timestamps) - the project/lane rollup and next
  // action shown per card live in DeliveryView, so each orchestration's
  // view is fetched alongside it. Orchestration counts are expected to
  // stay small (this is "how many multi-project deliveries are in
  // flight", not a general task list), so one view fetch per card is
  // cheap; a failed individual fetch degrades that one card instead of
  // the whole list.
  async function load() {
    loading = true;
    error = null;
    try {
      const { items } = await listDeliveries();
      rows = await Promise.all(
        items.map(async (orchestration): Promise<DeliveryRow> => {
          try {
            const view = await getDeliveryView(orchestration.id);
            return { orchestration, view, viewError: null };
          } catch (e) {
            return { orchestration, view: null, viewError: e instanceof Error ? e.message : String(e) };
          }
        }),
      );
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load();
    return onPanelEvent(load);
  });

  function open(id: string) {
    navigate(`/deliveries/${encodeURIComponent(id)}`);
  }

  const statusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };
</script>

<PageHeader
  title="Deliveries"
  description="Every multi-project delivery orchestration this Punakawan instance is running - progress, runnable lanes, and blockers across every project it touches."
/>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load deliveries" message={error} />
{:else if rows.length === 0}
  <EmptyStateCard title="No deliveries yet" message="Start a delivery orchestration to see it here." />
{:else}
  <ul class="deliveries" aria-label="Deliveries">
    {#each rows as row (row.orchestration.id)}
      <li>
        <button type="button" class="card" onclick={() => open(row.orchestration.id)}>
          <div class="row">
            <span class="title">
              <span class="icon"><Icon name="git-branch" size={20} /></span>
              <strong class="name">{row.orchestration.id}</strong>
            </span>
            <StatusBadge variant={statusVariants[row.orchestration.status] ?? "neutral"} label={row.orchestration.status} />
          </div>
          {#if row.view}
            <p class="next-action">{row.view.next_action}</p>
            <span class="stats" aria-label="Delivery snapshot">
              <span><strong>{row.view.projects.length}</strong> projects</span>
              <span><strong>{row.view.lanes.length}</strong> lanes</span>
              <span class:danger={row.view.blockers.length > 0}><strong>{row.view.blockers.length}</strong> blocked</span>
              <span><strong>{row.view.pending_approvals.length}</strong> pending approvals</span>
              <span><strong>{row.view.pending_questions.length}</strong> pending questions</span>
            </span>
          {:else if row.viewError}
            <p class="view-error" role="alert">Failed to load details: {row.viewError}</p>
          {/if}
          <span class="open-hint">Open delivery <span aria-hidden="true">→</span></span>
        </button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  ul.deliveries {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    grid-template-columns: 1fr;
    gap: 1rem;
  }
  @media (min-width: 640px) {
    ul.deliveries {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (min-width: 1024px) {
    ul.deliveries {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
  }
  ul.deliveries li {
    display: flex;
    min-width: 0;
  }
  .card {
    position: relative;
    overflow: hidden;
    width: 100%;
    min-width: 0;
    text-align: left;
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: grid;
    gap: 0.55rem;
    background: var(--surface-card-bg, var(--color-surface));
    cursor: pointer;
    font: inherit;
    color: var(--color-text);
  }
  .card:hover {
    border-color: var(--color-accent);
    box-shadow: var(--shadow-md);
    transform: translateY(-2px);
  }
  @media (prefers-reduced-motion: no-preference) {
    .card {
      transition: transform 150ms ease, box-shadow 150ms ease, border-color 150ms ease;
    }
  }
  .card:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    justify-content: space-between;
  }
  .title {
    display: inline-flex;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
  }
  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.3rem;
    height: 2.3rem;
    border-radius: 10px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-accent) 14%, transparent);
    flex-shrink: 0;
  }
  .name {
    font-size: 1rem;
    min-width: 0;
    overflow-wrap: anywhere;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .next-action {
    margin: 0;
    color: var(--color-text);
    font-size: 0.88rem;
  }
  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    padding-top: 0.3rem;
    border-top: 1px solid var(--color-border);
  }
  .stats > span {
    padding: 0.2rem 0.42rem;
    border-radius: 6px;
    background: var(--color-surface-subtle);
    color: var(--color-text-muted);
    font-size: 0.72rem;
  }
  .stats strong {
    color: var(--color-text);
    font-variant-numeric: tabular-nums;
  }
  .stats .danger,
  .stats .danger strong {
    color: var(--color-danger);
  }
  .view-error {
    margin: 0;
    color: var(--color-danger);
    font-size: 0.8rem;
  }
  .open-hint {
    justify-self: end;
    color: var(--color-accent);
    font-size: 0.76rem;
    font-weight: 650;
  }
</style>
