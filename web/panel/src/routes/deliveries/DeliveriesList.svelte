<script lang="ts">
  import { onMount } from "svelte";
  import {
    listDeliveries,
    getDeliveryView,
    cancelDelivery,
    type DeliveryOrchestration,
    type DeliveryView,
  } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Icon from "../../lib/components/Icon.svelte";
  import Button from "../../lib/components/Button.svelte";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";
  import {
    deliveryLabel,
    filterDeliveries,
    sortDeliveries,
    deliverySortOptions,
    isCancellableDelivery,
    type DeliverySortKey,
  } from "./deliveryList";

  interface DeliveryRow {
    orchestration: DeliveryOrchestration;
    view: DeliveryView | null;
    viewError: string | null;
  }

  let rows: DeliveryRow[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);
  // Only the very first load blanks the page for a spinner. Any panel event
  // re-runs load(), and swapping the list out for "Loading…" would unmount the
  // search box mid-keystroke, throwing away focus and caret position.
  let loaded = $state(false);

  let search = $state("");
  let sortKey: DeliverySortKey = $state("updated");

  // Both lists are already fully in memory (one view fetch per card happens at
  // load), so search and sort are pure derivations over them.
  const visible = $derived(sortDeliveries(filterDeliveries(rows, search), sortKey));

  // A delivery is an append-only event log: there is no remove, so the only
  // lifecycle action the panel can offer is cancelling one still in flight.
  //
  // Only the id is held, never the row object: a background refresh replaces
  // every row, and a captured row would post a stale expected_revision (and
  // could keep the dialog open for a delivery that has since finished).
  let pendingCancelId: string | null = $state(null);
  const pendingCancel = $derived.by(
    () => rows.find((r) => r.orchestration.id === pendingCancelId) ?? null,
  );
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);

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
      loaded = true;
    }
  }

  onMount(() => {
    load();
  });

  function open(id: string) {
    navigate(`/deliveries/${encodeURIComponent(id)}`);
  }

  function startCancel(row: DeliveryRow) {
    pendingCancelId = row.orchestration.id;
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
      await cancelDelivery(target.orchestration.id, {
        expected_revision: target.orchestration.revision,
      });
      pendingCancelId = null;
      await load();
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
</script>

<PageHeader
  title="Deliveries"
  description="Every multi-project delivery orchestration this Punakawan instance is running - progress, runnable lanes, and blockers across every project it touches."
/>

{#if loading && !loaded}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load deliveries" message={error} />
{:else if rows.length === 0}
  <EmptyStateCard title="No deliveries yet" message="Start a delivery orchestration to see it here." />
{:else}
  <div class="toolbar">
    <div class="field">
      <label for="delivery-search">Search deliveries</label>
      <input
        id="delivery-search"
        type="search"
        placeholder="Title, id, status, project, or task"
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
      {#each visible as row (row.orchestration.id)}
        <li>
          <div class="card">
            <!-- aria-label keeps the whole card body from being read out as one
                 enormous control name; the visible detail is still announced by
                 the elements themselves. -->
            <button
              type="button"
              class="open-area"
              aria-label={`Open delivery ${deliveryLabel(row.orchestration, row.view)}`}
              onclick={() => open(row.orchestration.id)}
            >
              <span class="row">
                <span class="title">
                  <span class="icon"><Icon name="git-branch" size={20} /></span>
                  <span class="heading">
                    <strong class="name">{deliveryLabel(row.orchestration, row.view)}</strong>
                    <span class="id">{row.orchestration.id}</span>
                  </span>
                </span>
                <StatusBadge
                  variant={statusVariants[row.orchestration.status] ?? "neutral"}
                  label={row.orchestration.status}
                />
              </span>
              {#if row.view}
                <span class="next-action">{row.view.next_action}</span>
                <span class="stats" aria-label="Delivery snapshot">
                  <span><strong>{row.view.projects.length}</strong> projects</span>
                  <span><strong>{row.view.lanes.length}</strong> lanes</span>
                  <span class:danger={row.view.blockers.length > 0}
                    ><strong>{row.view.blockers.length}</strong> blocked</span
                  >
                  <span><strong>{row.view.pending_questions.length}</strong> pending questions</span>
                </span>
              {/if}
            </button>
            <!-- Outside the button: an alert belongs in the document, not folded
                 into a control's accessible name. -->
            {#if !row.view && row.viewError}
              <p class="view-error" role="alert">Failed to load details: {row.viewError}</p>
            {/if}
            <div class="card-actions">
              {#if isCancellableDelivery(row.orchestration)}
                <Button
                  variant="danger"
                  size="sm"
                  onclick={() => startCancel(row)}
                  ariaLabel={`Cancel delivery ${deliveryLabel(row.orchestration, row.view)}`}
                >
                  Cancel
                </Button>
              {/if}
              <button type="button" class="open-hint" onclick={() => open(row.orchestration.id)}>
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
    label={deliveryLabel(pendingCancel.orchestration, pendingCancel.view)}
    orchestrationId={pendingCancel.orchestration.id}
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
  /* The card body is its own button so the Cancel action can sit beside it
     without nesting one button inside another. */
  .open-area {
    display: grid;
    gap: 0.55rem;
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
  /* The id stays visible (and selectable) as secondary text so it can still
     be copied for CLI use, without being what identifies the card. */
  .id {
    font-size: 0.72rem;
    color: var(--color-text-muted);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    overflow-wrap: anywhere;
  }
  .next-action {
    display: block;
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
</style>
