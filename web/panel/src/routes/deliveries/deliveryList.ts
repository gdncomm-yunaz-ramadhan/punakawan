// Client-side search/sort for the delivery list, over the compact
// DeliverySummary rows the list endpoint returns directly - no per-card
// detail fetch, no lane/blocked/pending-question concepts.

import type { DeliverySummary } from "../../lib/api/client";

// backoffDelaysMs is the live-refresh retry schedule every poll/watch
// loop in this route shares: 1s/2s/5s/10s on consecutive transport
// errors, capped at the last entry, reset to the first entry on any
// successful response.
export const backoffDelaysMs = [1000, 2000, 5000, 10000] as const;

export function backoffDelay(failureCount: number): number {
  const index = Math.min(failureCount, backoffDelaysMs.length - 1);
  return backoffDelaysMs[Math.max(index, 0)];
}

export type DeliverySortKey = "updated" | "title" | "status" | "cost";

export interface DeliverySortOption {
  key: DeliverySortKey;
  label: string;
}

export const deliverySortOptions: DeliverySortOption[] = [
  { key: "updated", label: "Recently updated" },
  { key: "title", label: "Title (A–Z)" },
  { key: "status", label: "Status" },
  { key: "cost", label: "Estimated cost" },
];

// Cancelling is only meaningful while a delivery can still hand out work;
// the summary itself already says so (deliveryprojection.isCancellable),
// so this is a plain passthrough rather than a second status check here.
export function isCancellableDelivery(summary: DeliverySummary): boolean {
  return summary.cancellable;
}

export function shortDeliveryId(id: string): string {
  return id.length > 10 ? `${id.slice(0, 10)}…` : id;
}

// totalEstimatedCost sums every currency in estimated_costs into one
// number for sort purposes only - it is never shown to a reader as a
// single amount, since summing across currencies is not a real total.
export function totalEstimatedCost(summary: DeliverySummary): number {
  return Object.values(summary.usage.estimated_costs ?? {}).reduce((sum, v) => sum + v, 0);
}

export interface DeliveryListRow {
  summary: DeliverySummary;
}

// Searches the label a reader sees plus the identifiers they might paste
// in: title, id, Jira key, project slugs, and plan objective.
export function filterDeliveries<T extends DeliveryListRow>(rows: T[], query: string): T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => {
    const s = row.summary;
    const haystack = [
      s.title,
      s.id,
      s.status,
      s.source?.key,
      s.plan?.objective,
      ...s.projects.map((p) => p.slug),
    ];
    return haystack.some((field) => (field ?? "").toLowerCase().includes(needle));
  });
}

// Sorting by status alphabetically would bury a pending delivery behind
// the cancelled ones, so the order is what needs attention first.
const statusOrder = ["active", "pending", "completed", "cancelled"];

function statusRank(status: string): number {
  const i = statusOrder.indexOf(status);
  return i === -1 ? statusOrder.length : i;
}

export function sortDeliveries<T extends DeliveryListRow>(rows: T[], key: DeliverySortKey): T[] {
  const titleOf = (row: T) => row.summary.title;
  const byTitle = (a: T, b: T) => titleOf(a).localeCompare(titleOf(b), undefined, { sensitivity: "base" });
  // Invalid or missing timestamps sort last rather than throwing off the
  // whole ordering with NaN comparisons.
  const time = (value: string | undefined) => {
    const ms = Date.parse(value ?? "");
    return Number.isNaN(ms) ? -Infinity : ms;
  };

  const comparators: Record<DeliverySortKey, (a: T, b: T) => number> = {
    updated: (a, b) => time(b.summary.updated_at) - time(a.summary.updated_at) || byTitle(a, b),
    title: byTitle,
    status: (a, b) => statusRank(a.summary.status) - statusRank(b.summary.status) || byTitle(a, b),
    cost: (a, b) => totalEstimatedCost(b.summary) - totalEstimatedCost(a.summary) || byTitle(a, b),
  };

  return [...rows].sort(comparators[key]);
}
