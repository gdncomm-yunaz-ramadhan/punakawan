<script lang="ts">
  import MobileDataList from "../../src/lib/components/data/MobileDataList.svelte";
  import type { Column, RowAction } from "../../src/lib/components/data/types";

  interface Row {
    id: string;
    title: string;
    status: string;
    priority: string;
    owner: string;
    updated: string;
  }

  interface Props {
    rows: Row[];
    withAction?: boolean;
  }
  let { rows, withAction = false }: Props = $props();

  const columns: Column<Row>[] = [
    { key: "title", label: "Title", primary: true },
    { key: "status", label: "Status" },
    { key: "priority", label: "Priority" },
    { key: "owner", label: "Owner" },
    { key: "updated", label: "Updated" },
  ];

  const rowAction: RowAction<Row> | undefined = withAction
    ? { label: "Open", onSelect: (row) => (lastSelected = row.id) }
    : undefined;

  let lastSelected: string | null = $state(null);
</script>

<MobileDataList {columns} {rows} {rowAction} />
{#if lastSelected}
  <p data-testid="last-selected">{lastSelected}</p>
{/if}
