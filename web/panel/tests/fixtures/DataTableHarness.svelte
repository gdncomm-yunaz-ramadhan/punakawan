<script lang="ts">
  import DataTable, { type Density } from "../../src/lib/components/data/DataTable.svelte";
  import type { Column, RowAction } from "../../src/lib/components/data/types";

  interface Row {
    id: string;
    title: string;
    status: string;
    priority: number;
    updated: string;
  }

  interface Props {
    rows: Row[];
    pageSize?: number;
    forceWidth?: number;
    selectable?: boolean;
    loading?: boolean;
    error?: string | null;
    density?: Density;
    withAction?: boolean;
    /**
     * Render DataTable the way every production call site does - rows and
     * columns only, no page/onPageChange. The wired branch below is what
     * the original pagination test exercised, which is why an unwired
     * Next button could stay broken with the suite green.
     */
    unwired?: boolean;
  }
  let {
    rows,
    pageSize = 10,
    forceWidth,
    selectable = false,
    loading = false,
    error = null,
    density = "comfortable",
    withAction = false,
    unwired = false,
  }: Props = $props();

  const columns: Column<Row>[] = [
    { key: "title", label: "Title", sortable: true, primary: true },
    { key: "status", label: "Status", sortable: true },
    { key: "priority", label: "Priority", sortable: true, align: "right" },
    { key: "updated", label: "Updated" },
  ];

  let page = $state(1);
  let currentPageSize = $state(pageSize);
  let lastSelected: string | null = $state(null);
  let lastSelectionIds: Array<string | number> = $state([]);

  const rowAction: RowAction<Row> | undefined = withAction
    ? { label: "Open", onSelect: (row) => (lastSelected = row.id) }
    : undefined;
</script>

{#if unwired}
  <DataTable {columns} {rows} {selectable} {density} {loading} {error} {forceWidth} {pageSize} />
{:else}
<DataTable
  {columns}
  {rows}
  {selectable}
  {density}
  {loading}
  {error}
  {forceWidth}
  page={page}
  pageSize={currentPageSize}
  onPageChange={(p) => (page = p)}
  onPageSizeChange={(s) => {
    currentPageSize = s;
    page = 1;
  }}
  onSelectionChange={(ids) => (lastSelectionIds = ids)}
  rowAction={rowAction}
/>
{/if}

<p data-testid="current-page">{page}</p>
{#if lastSelected}
  <p data-testid="last-selected">{lastSelected}</p>
{/if}
<p data-testid="selection-count">{lastSelectionIds.length}</p>
