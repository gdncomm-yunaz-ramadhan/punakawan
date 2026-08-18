// Human-readable labelling plus client-side search/sort for deliveries.
//
// A delivery's own id is an opaque hash, so the panel never shows it as the
// primary identifier. The backend is growing an optional `title`; until every
// stored orchestration has one, the label falls back to whatever the already
// loaded view can describe the delivery by, and only uses a shortened id when
// there is nothing else at all.

import type { DeliveryOrchestration, DeliveryView } from "../../lib/api/client";

export type DeliverySortKey = "updated" | "created" | "title" | "status";

export interface DeliverySortOption {
  key: DeliverySortKey;
  label: string;
}

export const deliverySortOptions: DeliverySortOption[] = [
  { key: "updated", label: "Recently updated" },
  { key: "created", label: "Recently created" },
  { key: "title", label: "Title (A–Z)" },
  { key: "status", label: "Status" },
];

// Cancelling is only meaningful while a delivery can still hand out lanes;
// completed and cancelled ones are already final.
export function isCancellableDelivery(orchestration: DeliveryOrchestration): boolean {
  return orchestration.status === "pending" || orchestration.status === "active";
}

export function shortDeliveryId(id: string): string {
  return id.length > 10 ? `${id.slice(0, 10)}…` : id;
}

function distinct(values: (string | undefined)[]): string[] {
  return [...new Set(values.filter((v): v is string => !!v))];
}

// Reads as a phrase rather than a truncated list: two names in full, then a
// count for the rest, so a wide delivery doesn't overflow the card.
function joinCapped(values: string[]): string {
  if (values.length <= 2) return values.join(", ");
  return `${values.slice(0, 2).join(", ")} +${values.length - 2} more`;
}

export function deliveryLabel(orchestration: DeliveryOrchestration, view: DeliveryView | null): string {
  const title = orchestration.title?.trim();
  if (title) return title;

  // The parent tasks are what a delivery was actually opened to deliver, so
  // they identify it better than the set of projects it happens to span.
  const parentTasks = distinct(view?.lanes.map((l) => l.parent_task_id) ?? []);
  if (parentTasks.length > 0) return joinCapped(parentTasks);

  const projects = distinct(view?.projects.map((p) => p.project_id) ?? []);
  if (projects.length > 0) return joinCapped(projects);

  return shortDeliveryId(orchestration.id);
}

export interface DeliveryListRow {
  orchestration: DeliveryOrchestration;
  view: DeliveryView | null;
}

// Searches the label a reader sees plus the identifiers they might paste in:
// the full id, the status, and the projects and parent tasks involved.
export function filterDeliveries<T extends DeliveryListRow>(rows: T[], query: string): T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => {
    const haystack = [
      deliveryLabel(row.orchestration, row.view),
      row.orchestration.id,
      row.orchestration.status,
      row.orchestration.workflow_definition_id,
      ...(row.view?.projects.map((p) => p.project_id) ?? []),
      ...(row.view?.lanes.map((l) => l.parent_task_id) ?? []),
    ];
    return haystack.some((field) => (field ?? "").toLowerCase().includes(needle));
  });
}

// Sorting by status alphabetically would bury a pending delivery behind the
// cancelled ones, so the order is what needs attention first.
const statusOrder = ["active", "pending", "completed", "cancelled"];

function statusRank(status: string): number {
  const i = statusOrder.indexOf(status);
  return i === -1 ? statusOrder.length : i;
}

export function sortDeliveries<T extends DeliveryListRow>(rows: T[], key: DeliverySortKey): T[] {
  const labelOf = (row: T) => deliveryLabel(row.orchestration, row.view);
  const byLabel = (a: T, b: T) => labelOf(a).localeCompare(labelOf(b), undefined, { sensitivity: "base" });
  // Invalid or missing timestamps sort last rather than throwing off the
  // whole ordering with NaN comparisons.
  const time = (value: string | undefined) => {
    const ms = Date.parse(value ?? "");
    return Number.isNaN(ms) ? -Infinity : ms;
  };
  const newestFirst = (pick: (row: T) => string | undefined) => (a: T, b: T) =>
    time(pick(b)) - time(pick(a)) || byLabel(a, b);

  const comparators: Record<DeliverySortKey, (a: T, b: T) => number> = {
    updated: newestFirst((row) => row.orchestration.updated_at),
    created: newestFirst((row) => row.orchestration.created_at),
    title: byLabel,
    status: (a, b) => statusRank(a.orchestration.status) - statusRank(b.orchestration.status) || byLabel(a, b),
  };

  return [...rows].sort(comparators[key]);
}
