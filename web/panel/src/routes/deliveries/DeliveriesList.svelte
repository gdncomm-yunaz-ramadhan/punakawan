<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { listDeliveries, getDeliveryDetail, cancelDelivery, type DeliverySummary } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Icon from "../../lib/components/Icon.svelte";
  import Button from "../../lib/components/Button.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";
  import {
    filterDeliveries,
    sortDeliveries,
    summarizeDeliveries,
    deliverySortOptions,
    isCancellableDelivery,
    backoffDelay,
    type DeliverySortKey,
    type DeliveryListRow,
    type DeliveriesOverview,
  } from "./deliveryList";

  const POLL_INTERVAL_MS = 10_000;

  let rows: DeliveryListRow[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);
  // Only the very first load blanks the page for a spinner. A background
  // poll refresh re-runs load() and must never clear rows while it does -
  // that would throw away scroll position and the search box's focus.
  let loaded = $state(false);

  let search = $state("");
  let sortKey: DeliverySortKey = $state("updated");

  const visible = $derived(sortDeliveries(filterDeliveries(rows, search), sortKey));
  const overview = $derived(summarizeDeliveries(rows.map((r) => r.summary)));
  let costBreakdownOpen = $state(false);

  let pendingCancelId: string | null = $state(null);
  const pendingCancel = $derived.by(() => rows.find((r) => r.summary.id === pendingCancelId) ?? null);
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);

  let pollTimer: ReturnType<typeof setTimeout> | undefined;
  let consecutiveFailures = 0;
  let stopped = false;

  async function load(showSpinner: boolean) {
    if (showSpinner) loading = true;
    try {
      const { items } = await listDeliveries();
      rows = items.map((summary): DeliveryListRow => ({ summary }));
      error = null;
      consecutiveFailures = 0;
    } catch (e) {
      consecutiveFailures += 1;
      // Never clear already-loaded rows on a refresh failure - only the
      // very first load surfaces the error state; a background poll
      // failure is silent (it will retry) unless nothing has ever loaded.
      if (!loaded) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (showSpinner) loading = false;
      loaded = true;
      scheduleNextPoll();
    }
  }

  function scheduleNextPoll() {
    if (stopped) return;
    clearTimeout(pollTimer);
    const delay = consecutiveFailures > 0 ? backoffDelay(consecutiveFailures - 1) : POLL_INTERVAL_MS;
    pollTimer = setTimeout(() => {
      if (document.hidden) {
        // Paused while the tab is hidden; resume as soon as it is visible
        // again rather than accumulating missed polls.
        scheduleNextPoll();
        return;
      }
      load(false);
    }, delay);
  }

  function onVisibilityChange() {
    if (!document.hidden) {
      clearTimeout(pollTimer);
      load(false);
    }
  }

  onMount(() => {
    load(true);
    document.addEventListener("visibilitychange", onVisibilityChange);
  });

  onDestroy(() => {
    stopped = true;
    clearTimeout(pollTimer);
    document.removeEventListener("visibilitychange", onVisibilityChange);
  });

  function open(id: string) {
    navigate(`/deliveries/${encodeURIComponent(id)}`);
  }

  function startCancel(row: DeliveryListRow) {
    pendingCancelId = row.summary.id;
    cancelError = null;
  }

  function closeCancel() {
    if (cancelling) return;
    pendingCancelId = null;
    cancelError = null;
  }

  async function confirmCancel() {
    const target = pendingCancel;
    if (!target) return;
    cancelling = true;
    cancelError = null;
    try {
      // The list's own summary carries no orchestration-scoped
      // optimistic-concurrency counter (only projection_revision, a
      // different number) - fetch the current detail immediately before
      // cancelling to get a fresh orchestration_revision rather than
      // guessing one.
      const detail = await getDeliveryDetail(target.summary.id);
      await cancelDelivery(target.summary.id, { expected_revision: detail.orchestration_revision });
      pendingCancelId = null;
      await load(false);
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  const statusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };

  function formatDate(value: string): string {
    return new Date(value).toLocaleString();
  }

  function formatDuration(ms: number): string {
    const minutes = Math.max(0, Math.floor(ms / 60_000));
    const hours = Math.floor(minutes / 60);
    return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
  }

  function formatCost(summary: DeliverySummary): string {
    const entries = Object.entries(summary.usage.estimated_costs ?? {});
    if (entries.length === 0) return "unknown";
    const formatted = entries
      .map(([currency, amount]) => new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amount))
      .join(" · ");
    return summary.usage.pricing_complete ? formatted : `${formatted} (partial)`;
  }

  function formatCosts(costs: Record<string, number>, pricingComplete: boolean): string {
    const entries = Object.entries(costs ?? {});
    if (entries.length === 0) return "No estimate";
    const formatted = entries
      .map(([currency, amount]) => new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amount))
      .join(" · ");
    return pricingComplete ? formatted : `${formatted} (partial - some usage has unknown pricing)`;
  }

  function sourceLabel(summary: DeliverySummary): string {
    if (!summary.source || summary.source.kind === "adhoc") return "Ad-hoc";
    return summary.source.key ?? "Jira";
  }
</script>

<PageHeader
  title="Deliveries"
  description="Every delivery this Punakawan instance is running - Jira or ad-hoc source, projects touched, plan, progress, and cost."
/>

{#if loading && !loaded}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load deliveries" message={error} />
{:else if rows.length === 0}
  <EmptyStateCard title="No deliveries yet" message="Start a delivery to see it here." />
{:else}
  <BentoGrid>
    <MetricCard size="small" columns={3} label="Total cost" value={formatCosts(overview.totalCosts, overview.pricingComplete)}>
      {#snippet cornerAction()}
        <button type="button" aria-label="Cost breakdown" onclick={() => (costBreakdownOpen = true)}>
          <Icon name="info" size={16} />
        </button>
      {/snippet}
    </MetricCard>
    <MetricCard size="small" columns={3} label="Plans" value={overview.planCount} />
    <MetricCard size="small" columns={3} label="Projects" value={overview.projectCount} />
    <MetricCard size="small" columns={3} label="Sessions" value={overview.sessionCount} />
  </BentoGrid>

  <Dialog open={costBreakdownOpen} title="Cost breakdown" onclose={() => (costBreakdownOpen = false)}>
    <dl class="breakdown">
      <dt>Total cost</dt><dd>{formatCosts(overview.totalCosts, overview.pricingComplete)}</dd>
      <dt>Elapsed time</dt><dd>{formatDuration(overview.totalElapsedMs)}</dd>
      <dt>Tokens spent</dt><dd>{overview.totalTokens.toLocaleString()}</dd>
      <dt>Tool calls</dt><dd>{overview.totalToolCalls.toLocaleString()}</dd>
    </dl>
  </Dialog>

  <div class="toolbar">
    <div class="field">
      <label for="delivery-search">Search deliveries</label>
      <input
        id="delivery-search"
        type="search"
        placeholder="Title, Jira key, project, or plan objective"
        bind:value={search}
        autocomplete="off"
      />
    </div>
    <div class="field">
      <label for="delivery-sort">Sort by</label>
      <select id="delivery-sort" bind:value={sortKey}>
        {#each deliverySortOptions as option (option.key)}
          <option value={option.key}>{option.label}</option>
        {/each}
      </select>
    </div>
  </div>

  {#if visible.length === 0}
    <EmptyStateCard
      title="No deliveries match your search"
      message={`Nothing matches “${search}”. Try a shorter search, or clear it to see all ${rows.length} deliveries.`}
    />
  {:else}
    <ul class="deliveries" aria-label="Deliveries">
      {#each visible as row (row.summary.id)}
        {@const s = row.summary}
        <li>
          <div class="card">
            <button
              type="button"
              class="open-area"
              aria-label={`Open delivery ${s.title}`}
              onclick={() => open(s.id)}
            >
              <span class="row">
                <span class="title">
                  <span class="icon"><Icon name="git-branch" size={20} /></span>
                  <span class="heading">
                    <strong class="name">{s.title}</strong>
                    <span class="source">{sourceLabel(s)}</span>
                  </span>
                </span>
                <StatusBadge variant={statusVariants[s.status] ?? "neutral"} label={s.status} />
              </span>

              {#if s.projects.length}
                <span class="projects">{s.projects.map((p) => p.slug).join(", ")}</span>
              {/if}

              <span class="stats" aria-label="Delivery usage">
                <span><strong>{(s.usage.input_tokens + s.usage.output_tokens).toLocaleString()}</strong> tokens</span>
                <span><strong>{s.usage.tool_calls.toLocaleString()}</strong> tool calls</span>
                <span><strong>{formatDuration(s.usage.elapsed_ms)}</strong> elapsed</span>
                <span><strong>{formatCost(s)}</strong> cost</span>
              </span>
              <span class="updated">Updated {formatDate(s.updated_at)}</span>
            </button>
            <div class="card-actions">
              {#if isCancellableDelivery(s)}
                <Button variant="danger" size="sm" onclick={() => startCancel(row)} ariaLabel={`Cancel delivery ${s.title}`}>
                  Cancel
                </Button>
              {/if}
              <button type="button" class="open-hint" onclick={() => open(s.id)}>
                Open delivery <span aria-hidden="true">→</span>
              </button>
            </div>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
{/if}

{#if pendingCancel}
  <DeliveryCancelDialog
    open={true}
    label={pendingCancel.summary.title}
    orchestrationId={pendingCancel.summary.id}
    busy={cancelling}
    error={cancelError}
    onclose={closeCancel}
    onconfirm={confirmCancel}
  />
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
    height: 100%;
    min-width: 0;
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: grid;
    gap: 0.55rem;
    background: var(--surface-card-bg, var(--color-surface));
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
  .open-area {
    display: grid;
    gap: 0.5rem;
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    text-align: left;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }
  .open-area:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .card-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
    align-items: flex-end;
    margin-bottom: 1rem;
  }
  .field {
    display: grid;
    gap: 0.3rem;
    min-width: 0;
  }
  .toolbar .field:first-child {
    flex: 1 1 16rem;
  }
  .field label {
    font-size: 0.78rem;
    font-weight: 650;
    color: var(--color-text-muted);
  }
  .field input,
  .field select {
    font: inherit;
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 0.45rem 0.6rem;
    min-height: 38px;
    min-width: 0;
  }
  .field input:focus-visible,
  .field select:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
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
  .heading {
    display: grid;
    gap: 0.1rem;
    min-width: 0;
  }
  .name {
    font-size: 1rem;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .source {
    font-size: 0.72rem;
    color: var(--color-text-muted);
    overflow-wrap: anywhere;
  }
  .projects {
    display: block;
    color: var(--color-text);
    font-size: 0.85rem;
    overflow-wrap: anywhere;
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
  .updated {
    font-size: 0.72rem;
    color: var(--color-text-muted);
  }
  .open-hint {
    margin-left: auto;
    padding: 0;
    border: 0;
    background: none;
    color: var(--color-accent);
    font-family: inherit;
    font-size: 0.76rem;
    font-weight: 650;
    cursor: pointer;
  }
  .open-hint:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .breakdown {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.9rem;
  }
  .breakdown dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .breakdown dd {
    margin: 0;
    color: var(--color-text);
    font-weight: 600;
  }
</style>
