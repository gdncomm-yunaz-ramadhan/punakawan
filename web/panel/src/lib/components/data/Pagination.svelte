<script lang="ts">
  interface Props {
    currentPage: number;
    totalPages: number;
    pageSize: number;
    pageSizeOptions?: number[];
    onPageChange: (page: number) => void;
    onPageSizeChange?: (pageSize: number) => void;
  }
  let {
    currentPage,
    totalPages,
    pageSize,
    pageSizeOptions = [10, 25, 50, 100],
    onPageChange,
    onPageSizeChange,
  }: Props = $props();

  function goTo(page: number) {
    const clamped = Math.min(Math.max(page, 1), Math.max(totalPages, 1));
    if (clamped !== currentPage) onPageChange(clamped);
  }

  function onPageSizeSelect(e: Event) {
    const value = Number((e.target as HTMLSelectElement).value);
    onPageSizeChange?.(value);
  }
</script>

<!--
  Page number controls + page-size selector (UI-011/§13.7), shared by
  DataTable rather than duplicated pagination UI logic per page. Uses
  callback props (onPageChange/onPageSizeChange), matching the
  onSelect/onclose convention already used by ResponsiveToolbar/Drawer.
-->
<nav class="pagination" aria-label="Pagination">
  <button type="button" onclick={() => goTo(currentPage - 1)} disabled={currentPage <= 1}> Previous </button>
  <span class="page-status" aria-live="polite">Page {currentPage} of {Math.max(totalPages, 1)}</span>
  <button type="button" onclick={() => goTo(currentPage + 1)} disabled={currentPage >= totalPages}> Next </button>

  {#if onPageSizeChange}
    <label class="page-size">
      Rows per page
      <select value={pageSize} onchange={onPageSizeSelect}>
        {#each pageSizeOptions as option (option)}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </label>
  {/if}
</nav>

<style>
  .pagination {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
    font-size: 0.85rem;
  }
  button {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.35rem 0.7rem;
    cursor: pointer;
    min-height: 36px;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  button:hover:not(:disabled) {
    border-color: var(--color-border-strong);
  }
  .page-status {
    color: var(--color-text-muted);
  }
  .page-size {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    margin-left: auto;
    color: var(--color-text-muted);
  }
  .page-size select {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.3rem 0.4rem;
    background: var(--color-surface);
    color: var(--color-text);
  }
</style>
