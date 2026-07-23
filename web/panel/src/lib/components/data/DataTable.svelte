<script lang="ts" module>
  export type Density = "compact" | "comfortable";
</script>

<script lang="ts" generics="T extends { id: string | number }">
  import { onMount } from "svelte";
  import { getCellValue, type Column, type RowAction, type SortDirection } from "./types";
  import Pagination from "./Pagination.svelte";
  import MobileDataList from "./MobileDataList.svelte";

  interface Props {
    columns: Column<T>[];
    rows: T[];
    selectable?: boolean;
    density?: Density;
    page?: number;
    pageSize?: number;
    onPageChange?: (page: number) => void;
    onPageSizeChange?: (pageSize: number) => void;
    onSelectionChange?: (selectedIds: Array<string | number>) => void;
    rowAction?: RowAction<T>;
    loading?: boolean;
    error?: string | null;
    emptyMessage?: string;
    // Below this content-box width (px), DataTable renders
    // MobileDataList instead of a <table> (§13.7's mobile transformation
    // rule). Overridable so tests can force either branch deterministically
    // without a real layout engine.
    mobileBreakpoint?: number;
    // Test seam: when provided, this observed width is used instead of a
    // real ResizeObserver measurement (jsdom has no layout engine).
    forceWidth?: number;
  }
  let {
    columns,
    rows,
    selectable = false,
    density = "comfortable",
    page = 1,
    pageSize = 10,
    onPageChange,
    onPageSizeChange,
    onSelectionChange,
    rowAction,
    loading = false,
    error = null,
    emptyMessage = "No rows match these filters.",
    mobileBreakpoint = 640,
    forceWidth,
  }: Props = $props();

  let containerEl: HTMLDivElement | undefined;
  let observedWidth = $state<number | null>(null);
  // Tracks columns the user has explicitly hidden via the column-visibility
  // menu, rather than snapshotting the full visible set once - so newly
  // added/removed columns in the `columns` prop stay visible by default.
  let hiddenColumnKeys: Set<string> = $state(new Set());
  let columnMenuOpen = $state(false);
  let selectedIds: Set<string | number> = $state(new Set());
  let sortKey: string | null = $state(null);
  let sortDirection: SortDirection = $state("asc");

  const isMobile = $derived((forceWidth ?? observedWidth ?? Infinity) < mobileBreakpoint);
  const visibleColumns = $derived(columns.filter((c) => !hiddenColumnKeys.has(c.key)));

  const sortedRows = $derived.by(() => {
    if (!sortKey) return rows;
    const column = columns.find((c) => c.key === sortKey);
    if (!column) return rows;
    const copy = [...rows];
    copy.sort((a, b) => {
      const av = getCellValue(a, column);
      const bv = getCellValue(b, column);
      const an = Number(av);
      const bn = Number(bv);
      let cmp: number;
      if (!Number.isNaN(an) && !Number.isNaN(bn) && av !== "" && bv !== "") {
        cmp = an - bn;
      } else {
        cmp = av.localeCompare(bv);
      }
      return sortDirection === "asc" ? cmp : -cmp;
    });
    return copy;
  });

  const totalPages = $derived(Math.max(Math.ceil(sortedRows.length / pageSize), 1));
  const pagedRows = $derived(sortedRows.slice((page - 1) * pageSize, (page - 1) * pageSize + pageSize));

  function toggleSort(column: Column<T>) {
    if (!column.sortable) return;
    if (sortKey === column.key) {
      sortDirection = sortDirection === "asc" ? "desc" : "asc";
    } else {
      sortKey = column.key;
      sortDirection = "asc";
    }
  }

  function toggleColumn(key: string) {
    const next = new Set(hiddenColumnKeys);
    if (next.has(key)) {
      next.delete(key);
    } else {
      next.add(key);
    }
    hiddenColumnKeys = next;
  }

  function toggleRowSelection(id: string | number) {
    const next = new Set(selectedIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    selectedIds = next;
    onSelectionChange?.([...next]);
  }

  function toggleSelectAll() {
    if (selectedIds.size === pagedRows.length && pagedRows.length > 0) {
      selectedIds = new Set();
    } else {
      selectedIds = new Set(pagedRows.map((r) => r.id));
    }
    onSelectionChange?.([...selectedIds]);
  }

  function handlePageChange(next: number) {
    onPageChange?.(next);
  }

  onMount(() => {
    if (forceWidth !== undefined || !containerEl || typeof ResizeObserver === "undefined") {
      return;
    }
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) observedWidth = entry.contentRect.width;
    });
    observer.observe(containerEl);
    observedWidth = containerEl.getBoundingClientRect().width;
    return () => observer.disconnect();
  });
</script>

<!--
  Centerpiece shared table (UI-011, §13.7): typed columns, sorting,
  pagination (delegated to Pagination so the UI isn't duplicated),
  column visibility, sticky header, optional row selection, status/badge
  cell rendering (via column.render), loading/empty/error states,
  keyboard-accessible row actions, compact/comfortable density, and a
  mobile card-list fallback (delegated to MobileDataList) below
  `mobileBreakpoint` - per the plan's explicit rule against squeezing a
  wide desktop table into a narrow viewport.
-->
<div class="data-table" bind:this={containerEl} data-density={density}>
  <div class="toolbar">
    <div class="column-toggle">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={columnMenuOpen}
        onclick={() => (columnMenuOpen = !columnMenuOpen)}
      >
        Columns
      </button>
      {#if columnMenuOpen}
        <div class="column-menu" role="menu">
          {#each columns as column (column.key)}
            <label class="column-option">
              <input
                type="checkbox"
                checked={!hiddenColumnKeys.has(column.key)}
                onchange={() => toggleColumn(column.key)}
              />
              {column.label}
            </label>
          {/each}
        </div>
      {/if}
    </div>
  </div>

  {#if loading}
    <div class="skeleton" role="status" aria-label="Loading" data-testid="data-table-skeleton">
      {#each Array(4) as _, i (i)}
        <span class="skeleton-row"></span>
      {/each}
    </div>
  {:else if error}
    <p role="alert" class="error-message" data-testid="data-table-error">{error}</p>
  {:else if isMobile}
    <MobileDataList columns={visibleColumns} rows={pagedRows} {rowAction} {emptyMessage} />
  {:else if sortedRows.length === 0}
    <p class="empty-message" data-testid="data-table-empty">{emptyMessage}</p>
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            {#if selectable}
              <th class="select-col">
                <input
                  type="checkbox"
                  aria-label="Select all rows on this page"
                  checked={pagedRows.length > 0 && selectedIds.size === pagedRows.length}
                  onchange={toggleSelectAll}
                />
              </th>
            {/if}
            {#each visibleColumns as column (column.key)}
              <th scope="col" class="align-{column.align ?? 'left'}">
                {#if column.sortable}
                  <button type="button" class="sort-button" onclick={() => toggleSort(column)}>
                    {column.label}
                    {#if sortKey === column.key}
                      <span aria-hidden="true">{sortDirection === "asc" ? "▲" : "▼"}</span>
                    {/if}
                  </button>
                {:else}
                  {column.label}
                {/if}
              </th>
            {/each}
            {#if rowAction}
              <th scope="col" class="actions-col">Actions</th>
            {/if}
          </tr>
        </thead>
        <tbody>
          {#each pagedRows as row (row.id)}
            <tr>
              {#if selectable}
                <td class="select-col">
                  <input
                    type="checkbox"
                    aria-label="Select row"
                    checked={selectedIds.has(row.id)}
                    onchange={() => toggleRowSelection(row.id)}
                  />
                </td>
              {/if}
              {#each visibleColumns as column (column.key)}
                <td class="align-{column.align ?? 'left'}">{getCellValue(row, column)}</td>
              {/each}
              {#if rowAction}
                <td class="actions-col">
                  <button type="button" class="row-action" onclick={() => rowAction!.onSelect(row)}>
                    {rowAction.label}
                  </button>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  {#if !loading && !error && sortedRows.length > 0}
    <div class="pagination-row">
      <Pagination currentPage={page} {totalPages} {pageSize} onPageChange={handlePageChange} {onPageSizeChange} />
    </div>
  {/if}
</div>

<style>
  .data-table {
    display: grid;
    gap: 0.6rem;
  }
  .toolbar {
    display: flex;
    justify-content: flex-end;
  }
  .column-toggle {
    position: relative;
  }
  .column-toggle > button {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.35rem 0.65rem;
    font-size: 0.8rem;
    cursor: pointer;
    min-height: 36px;
  }
  .column-menu {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: var(--shadow-card);
    padding: 0.5rem;
    z-index: 5;
    min-width: 160px;
    display: grid;
    gap: 0.3rem;
  }
  .column-option {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.85rem;
    color: var(--color-text);
  }

  .table-scroll {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9rem;
  }
  thead th {
    position: sticky;
    top: 0;
    background: var(--color-surface);
    text-align: left;
    color: var(--color-text-muted);
    font-weight: 500;
    font-size: 0.8rem;
    border-bottom: 1px solid var(--color-border);
    z-index: 1;
  }
  th,
  td {
    padding: 0.6rem 0.6rem;
  }
  .data-table[data-density="compact"] th,
  .data-table[data-density="compact"] td {
    padding: 0.3rem 0.5rem;
  }
  td {
    border-bottom: 1px solid var(--color-border);
    color: var(--color-text);
  }
  tbody tr:hover {
    background: var(--color-surface-subtle);
  }
  tbody tr:focus-within {
    background: var(--color-accent-soft);
  }
  .align-left {
    text-align: left;
  }
  .align-right {
    text-align: right;
  }
  .align-center {
    text-align: center;
  }
  .sort-button {
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    font-weight: inherit;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    cursor: pointer;
    padding: 0;
    min-height: 36px;
  }
  .select-col {
    width: 2.5rem;
  }
  .actions-col {
    width: 1%;
    white-space: nowrap;
  }
  .row-action {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    min-height: 36px;
  }
  .row-action:hover {
    border-color: var(--color-border-strong);
  }

  .empty-message {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    padding: 0.75rem 0;
  }
  .error-message {
    color: var(--color-danger);
    font-size: 0.85rem;
    padding: 0.75rem 0;
  }

  .skeleton {
    display: grid;
    gap: 0.5rem;
  }
  .skeleton-row {
    display: block;
    height: 1.5rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
  }
  @media (prefers-reduced-motion: no-preference) {
    .skeleton-row {
      animation: pulse 1.1s ease-in-out infinite;
    }
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }

  .pagination-row {
    display: flex;
    justify-content: flex-end;
  }
</style>
