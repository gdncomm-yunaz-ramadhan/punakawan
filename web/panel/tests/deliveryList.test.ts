import { describe, expect, it } from "vitest";
import {
  backoffDelay,
  backoffDelaysMs,
  deliverySortOptions,
  filterDeliveries,
  isCancellableDelivery,
  shortDeliveryId,
  sortDeliveries,
  summarizeDeliveries,
  totalEstimatedCost,
  type DeliveryListRow,
} from "../src/routes/deliveries/deliveryList";
import type { DeliverySummary } from "../src/lib/api/client";

const longId = "c642fb0e8a69bdcae2be77e3ab";

function summary(over: Partial<DeliverySummary> = {}): DeliverySummary {
  return {
    id: longId,
    title: "Untitled delivery",
    status: "active",
    projects: [],
    usage: {
      input_tokens: 0,
      output_tokens: 0,
      cache_tokens: 0,
      tool_calls: 0,
      elapsed_ms: 0,
      estimated_costs: {},
      pricing_complete: false,
    },
    updated_at: "2026-08-10T00:00:00Z",
    cancellable: true,
    projection_revision: 1,
    ...over,
  } as DeliverySummary;
}

function row(over: Partial<DeliverySummary> = {}): DeliveryListRow {
  return { summary: summary(over) };
}

describe("backoffDelay", () => {
  it("follows the fixed schedule and caps at the last entry", () => {
    expect(backoffDelay(0)).toBe(backoffDelaysMs[0]);
    expect(backoffDelay(1)).toBe(backoffDelaysMs[1]);
    expect(backoffDelay(2)).toBe(backoffDelaysMs[2]);
    expect(backoffDelay(3)).toBe(backoffDelaysMs[3]);
    expect(backoffDelay(99)).toBe(backoffDelaysMs[backoffDelaysMs.length - 1]);
  });

  it("never returns a delay for a negative failure count", () => {
    expect(backoffDelay(-1)).toBe(backoffDelaysMs[0]);
  });
});

describe("shortDeliveryId", () => {
  it("truncates a long id", () => {
    expect(shortDeliveryId(longId)).toBe("c642fb0e8a…");
  });

  it("leaves an already-short id intact", () => {
    expect(shortDeliveryId("orc-1")).toBe("orc-1");
  });
});

describe("delivery lifecycle predicates", () => {
  it("passes through the summary's own cancellable flag", () => {
    expect(isCancellableDelivery(summary({ cancellable: true }))).toBe(true);
    expect(isCancellableDelivery(summary({ cancellable: false }))).toBe(false);
  });
});

describe("totalEstimatedCost", () => {
  it("sums every currency for sort purposes", () => {
    const s = summary({ usage: { ...summary().usage, estimated_costs: { USD: 1.5, EUR: 2 } } });
    expect(totalEstimatedCost(s)).toBeCloseTo(3.5);
  });

  it("is zero when no cost is known yet", () => {
    expect(totalEstimatedCost(summary())).toBe(0);
  });
});

describe("filterDeliveries", () => {
  const rows: DeliveryListRow[] = [
    row({ id: "orc-1", title: "Migrate billing", status: "active" }),
    row({
      id: "orc-2",
      title: "Refresh checkout",
      status: "completed",
      source: { kind: "jira", key: "PUN-99" },
      plan: { id: "plan-1", revision: 1, objective: "Ship checkout refresh" },
      projects: [{ id: "checkout", slug: "checkout" }],
    }),
  ];

  it("returns every row for an empty query", () => {
    expect(filterDeliveries(rows, "  ")).toHaveLength(2);
  });

  it("matches the title, the full id, and the status", () => {
    expect(filterDeliveries(rows, "Migrate")[0].summary.id).toBe("orc-1");
    expect(filterDeliveries(rows, "orc-2")[0].summary.id).toBe("orc-2");
    expect(filterDeliveries(rows, "completed")[0].summary.id).toBe("orc-2");
  });

  it("matches project slugs, the Jira key, and the plan objective", () => {
    expect(filterDeliveries(rows, "checkout")[0].summary.id).toBe("orc-2");
    expect(filterDeliveries(rows, "PUN-99")[0].summary.id).toBe("orc-2");
    expect(filterDeliveries(rows, "Ship checkout refresh")[0].summary.id).toBe("orc-2");
  });

  it("is case-insensitive and returns nothing when unmatched", () => {
    expect(filterDeliveries(rows, "migrate")).toHaveLength(1);
    expect(filterDeliveries(rows, "nope")).toEqual([]);
  });
});

describe("summarizeDeliveries", () => {
  it("sums estimated cost per currency across deliveries", () => {
    const overview = summarizeDeliveries([
      summary({ usage: { ...summary().usage, estimated_costs: { USD: 1, EUR: 2 } } }),
      summary({ usage: { ...summary().usage, estimated_costs: { USD: 3, GBP: 5 } } }),
    ]);
    expect(overview.totalCosts).toEqual({ USD: 4, EUR: 2, GBP: 5 });
  });

  it("marks pricingComplete false when any one delivery has incomplete pricing", () => {
    const overview = summarizeDeliveries([
      summary({ usage: { ...summary().usage, pricing_complete: true } }),
      summary({ usage: { ...summary().usage, pricing_complete: false } }),
    ]);
    expect(overview.pricingComplete).toBe(false);
  });

  it("keeps pricingComplete true when every delivery's pricing is complete", () => {
    const overview = summarizeDeliveries([
      summary({ usage: { ...summary().usage, pricing_complete: true } }),
      summary({ usage: { ...summary().usage, pricing_complete: true } }),
    ]);
    expect(overview.pricingComplete).toBe(true);
  });

  it("sums tokens, tool calls, and elapsed time across deliveries", () => {
    const overview = summarizeDeliveries([
      summary({
        usage: { ...summary().usage, input_tokens: 100, output_tokens: 20, tool_calls: 3, elapsed_ms: 1_000 },
      }),
      summary({
        usage: { ...summary().usage, input_tokens: 50, output_tokens: 10, tool_calls: 2, elapsed_ms: 2_000 },
      }),
    ]);
    expect(overview.totalTokens).toBe(180);
    expect(overview.totalToolCalls).toBe(5);
    expect(overview.totalElapsedMs).toBe(3_000);
  });

  it("dedups project ids shared across deliveries", () => {
    const overview = summarizeDeliveries([
      summary({ projects: [{ id: "proj-a", slug: "proj-a" }] }),
      summary({
        projects: [
          { id: "proj-a", slug: "proj-a" },
          { id: "proj-b", slug: "proj-b" },
        ],
      }),
    ]);
    expect(overview.projectCount).toBe(2);
  });

  it("dedups plan ids shared across deliveries", () => {
    const overview = summarizeDeliveries([
      summary({ plan: { id: "plan-1", revision: 1, objective: "A" } }),
      summary({ plan: { id: "plan-1", revision: 2, objective: "A" } }),
      summary({ plan: { id: "plan-2", revision: 1, objective: "B" } }),
    ]);
    expect(overview.planCount).toBe(2);
  });

  it("counts only deliveries that currently have a session", () => {
    const overview = summarizeDeliveries([
      summary({ session: { status: "active", started_at: "2026-08-09T00:00:00Z" } }),
      summary({}),
      summary({ session: { status: "stopped", started_at: "2026-08-08T00:00:00Z" } }),
    ]);
    expect(overview.sessionCount).toBe(2);
  });
});

describe("sortDeliveries", () => {
  const rows: DeliveryListRow[] = [
    row({ id: "a", title: "Beta", updated_at: "2026-05-01T00:00:00Z" }),
    row({ id: "b", title: "Alpha", updated_at: "2026-04-01T00:00:00Z" }),
  ];
  const ids = (list: DeliveryListRow[]) => list.map((r) => r.summary.id);

  it("puts the most recently updated first", () => {
    expect(ids(sortDeliveries(rows, "updated"))).toEqual(["a", "b"]);
  });

  it("sorts by title", () => {
    expect(ids(sortDeliveries(rows, "title"))).toEqual(["b", "a"]);
  });

  it("sorts by estimated cost, highest first", () => {
    const costed: DeliveryListRow[] = [
      row({ id: "cheap", title: "A", usage: { ...summary().usage, estimated_costs: { USD: 1 } } }),
      row({ id: "pricey", title: "B", usage: { ...summary().usage, estimated_costs: { USD: 9 } } }),
    ];
    expect(ids(sortDeliveries(costed, "cost"))).toEqual(["pricey", "cheap"]);
  });

  it("groups by status and breaks ties by title", () => {
    const mixed: DeliveryListRow[] = [
      row({ id: "x", title: "Zulu", status: "pending" }),
      row({ id: "y", title: "Bravo", status: "active" }),
      row({ id: "z", title: "Alpha", status: "active" }),
    ];
    expect(ids(sortDeliveries(mixed, "status"))).toEqual(["z", "y", "x"]);
  });

  it("orders statuses by what needs attention, not alphabetically", () => {
    // Alphabetically this would be active, cancelled, completed, pending -
    // burying a still-pending delivery behind the finished ones.
    const mixed: DeliveryListRow[] = [
      row({ id: "cancelled", title: "A", status: "cancelled" }),
      row({ id: "completed", title: "A", status: "completed" }),
      row({ id: "pending", title: "A", status: "pending" }),
      row({ id: "active", title: "A", status: "active" }),
    ];
    expect(ids(sortDeliveries(mixed, "status"))).toEqual(["active", "pending", "completed", "cancelled"]);
  });

  it("sorts rows with an unparseable timestamp last instead of scrambling the order", () => {
    const broken: DeliveryListRow[] = [
      row({ id: "bad", title: "Bad", updated_at: "not-a-date" }),
      row({ id: "good", title: "Good", updated_at: "2026-05-01T00:00:00Z" }),
    ];
    expect(ids(sortDeliveries(broken, "updated"))).toEqual(["good", "bad"]);
  });

  it("does not mutate the input array", () => {
    const original = [...rows];
    sortDeliveries(rows, "title");
    expect(rows).toEqual(original);
  });

  it("offers a comparator for every advertised sort option", () => {
    for (const option of deliverySortOptions) {
      expect(sortDeliveries(rows, option.key)).toHaveLength(rows.length);
    }
  });
});
