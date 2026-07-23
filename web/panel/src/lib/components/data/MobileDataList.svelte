<script lang="ts" generics="T extends { id: string | number }">
  import { getCellValue, type Column, type RowAction } from "./types";

  interface Props {
    columns: Column<T>[];
    rows: T[];
    rowAction?: RowAction<T>;
    emptyMessage?: string;
  }
  let { columns, rows, rowAction, emptyMessage = "No rows match these filters." }: Props = $props();

  // Mobile card layout (§13.7 "Mobile table transformation"): the first
  // primary-flagged column (or the first column) is the title, up to
  // three remaining columns show inline, and anything past that collapses
  // into an expandable "more" section per row.
  const primaryColumn = $derived(columns.find((c) => c.primary) ?? columns[0]);
  const secondaryColumns = $derived(columns.filter((c) => c.key !== primaryColumn?.key).slice(0, 3));
  const moreColumns = $derived(columns.filter((c) => c.key !== primaryColumn?.key).slice(3));

  let expanded: Record<string | number, boolean> = $state({});

  function toggle(id: string | number) {
    expanded = { ...expanded, [id]: !expanded[id] };
  }
</script>

<!--
  Card-row renderer DataTable delegates to below 640px (UI-011/UI-012,
  §13.7): title/primary field, up to three important fields inline, an
  expandable "more" section for the rest, and a visible primary action
  - never a raw <table> squeezed into a narrow viewport.
-->
{#if rows.length === 0}
  <p class="empty" data-testid="mobile-list-empty">{emptyMessage}</p>
{:else}
  <ul class="mobile-list" data-testid="mobile-data-list">
    {#each rows as row (row.id)}
      <li class="row-card">
        <div class="primary-row">
          <span class="primary-value">{primaryColumn ? getCellValue(row, primaryColumn) : ""}</span>
        </div>
        <div class="fields">
          {#each secondaryColumns as column (column.key)}
            <div class="field">
              <span class="field-label">{column.label}</span>
              <span class="field-value">{getCellValue(row, column)}</span>
            </div>
          {/each}
        </div>

        {#if moreColumns.length > 0}
          <button
            type="button"
            class="more-toggle"
            aria-expanded={!!expanded[row.id]}
            onclick={() => toggle(row.id)}
          >
            {expanded[row.id] ? "Show less" : "Show more"}
          </button>
          {#if expanded[row.id]}
            <div class="fields more-fields">
              {#each moreColumns as column (column.key)}
                <div class="field">
                  <span class="field-label">{column.label}</span>
                  <span class="field-value">{getCellValue(row, column)}</span>
                </div>
              {/each}
            </div>
          {/if}
        {/if}

        {#if rowAction}
          <button type="button" class="primary-action" onclick={() => rowAction!.onSelect(row)}>
            {rowAction.label}
          </button>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .empty {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    padding: 0.75rem 0;
  }
  .mobile-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.6rem;
  }
  .row-card {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: var(--color-surface);
    padding: 0.75rem 0.9rem;
    display: grid;
    gap: 0.5rem;
  }
  .primary-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .primary-value {
    font-weight: 600;
    color: var(--color-text);
  }
  .fields {
    display: grid;
    gap: 0.3rem;
  }
  .field {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.5rem;
    font-size: 0.85rem;
  }
  .field-label {
    color: var(--color-text-muted);
  }
  .field-value {
    color: var(--color-text);
    text-align: right;
  }
  .more-toggle {
    justify-self: start;
    background: none;
    border: none;
    color: var(--color-accent);
    font-size: 0.8rem;
    cursor: pointer;
    padding: 0.2rem 0;
    min-height: 36px;
  }
  .more-fields {
    border-top: 1px dashed var(--color-border);
    padding-top: 0.4rem;
  }
  .primary-action {
    border: 1px solid var(--color-accent);
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    border-radius: 6px;
    padding: 0.45rem 0.7rem;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    min-height: 44px;
  }
</style>
