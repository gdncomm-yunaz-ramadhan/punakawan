<script lang="ts">
  import type { TaskSummary } from "../../lib/api/client";
  import DataTable from "../../lib/components/data/DataTable.svelte";
  import type { Column, RowAction } from "../../lib/components/data/types";

  interface Props {
    tasks: TaskSummary[];
    onselect: (id: string) => void;
  }
  let { tasks, onselect }: Props = $props();

  let page = $state(1);
  let pageSize = $state(10);

  const priorityLabels: Record<number, string> = { 0: "P0", 1: "P1", 2: "P2", 3: "P3", 4: "P4" };

  const columns: Column<TaskSummary>[] = [
    { key: "id", label: "ID", primary: true },
    { key: "title", label: "Title", sortable: true },
    { key: "board_status", label: "Status", sortable: true, render: (t) => (t.stale ? `${t.board_status} (stale)` : t.board_status) },
    { key: "priority", label: "Priority", sortable: true, render: (t) => priorityLabels[t.priority] ?? `P${t.priority}` },
    { key: "dependencies", label: "Dependencies", align: "right", render: (t) => String(t.dependencies?.length ?? 0) },
    { key: "external_ref", label: "External ref", render: (t) => t.external_ref ?? "—" },
    { key: "updated_at", label: "Updated", sortable: true, render: (t) => new Date(t.updated_at).toLocaleDateString() },
  ];

  const rowAction: RowAction<TaskSummary> = {
    label: "Open",
    onSelect: (task) => onselect(task.id),
  };
</script>

<!--
  Migrated (UI-011/UI-017) to the shared DataTable instead of a bespoke
  <table>: gains sorting, pagination, column visibility, sticky header,
  and a mobile card-list fallback for free, and its status/priority
  formatting now goes through DataTable's column.render + row actions
  rather than one-off markup.
-->
<DataTable
  {columns}
  rows={tasks}
  {page}
  {pageSize}
  onPageChange={(p) => (page = p)}
  onPageSizeChange={(s) => {
    pageSize = s;
    page = 1;
  }}
  {rowAction}
  emptyMessage="No tasks match these filters."
/>
