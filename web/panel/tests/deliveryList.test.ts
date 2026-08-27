import { describe, expect, it } from "vitest";
import {
  deliveryLabel,
  deliverySortOptions,
  filterDeliveries,
  isCancellableDelivery,
  shortDeliveryId,
  sortDeliveries,
  type DeliveryListRow,
} from "../src/routes/deliveries/deliveryList";
import type { DeliveryOrchestration, DeliveryView } from "../src/lib/api/client";

const longId = "c642fb0e8a69bdcae2be77e3ab";

function orchestration(over: Partial<DeliveryOrchestration> = {}): DeliveryOrchestration {
  return {
    id: longId,
    revision: 1,
    status: "active",
    unresolved_inputs: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-10T00:00:00Z",
    ...over,
  };
}

function view(over: Partial<DeliveryView> = {}): DeliveryView {
  return {
    orchestration: orchestration(),
    // Blank by default so each case opts into the title it wants to exercise,
    // and the fallback chain stays reachable for the cases that need it.
    title: "",
    projects: [],
    lanes: [],
    blockers: [],
    pending_questions: [],
    next_action: "",
    latest_seq: 0,
    newly_runnable_lane_ids: [],
    ...over,
  };
}

function row(o: Partial<DeliveryOrchestration>, v: Partial<DeliveryView> | null = null): DeliveryListRow {
  return { orchestration: orchestration(o), view: v === null ? null : view(v) };
}

describe("deliveryLabel", () => {
  it("prefers the backend title when it is present", () => {
    const label = deliveryLabel(orchestration({ title: "Migrate billing to v2" }), null);
    expect(label).toBe("Migrate billing to v2");
  });

  it("uses the loaded view's title when the orchestration record carries none", () => {
    const v = view({
      title: "Retire the legacy pricing endpoint",
      lanes: [{ lane_id: "l1", project_id: "billing", status: "runnable", parent_task_id: "PUN-12" }],
    });
    expect(deliveryLabel(orchestration(), v)).toBe("Retire the legacy pricing endpoint");
  });

  it("ignores a title that is empty or only whitespace", () => {
    expect(deliveryLabel(orchestration({ title: "   " }), null)).toBe(shortDeliveryId(longId));
    expect(deliveryLabel(orchestration({ title: "" }), null)).toBe(shortDeliveryId(longId));
  });

  it("falls back to the parent tasks the delivery is delivering", () => {
    const v = view({
      lanes: [
        { lane_id: "l1", project_id: "billing", status: "runnable", parent_task_id: "PUN-12" },
        { lane_id: "l2", project_id: "checkout", status: "waiting", parent_task_id: "PUN-13" },
      ],
    });
    expect(deliveryLabel(orchestration(), v)).toBe("PUN-12, PUN-13");
  });

  it("de-duplicates repeated parent tasks", () => {
    const v = view({
      lanes: [
        { lane_id: "l1", project_id: "billing", status: "runnable", parent_task_id: "PUN-12" },
        { lane_id: "l2", project_id: "checkout", status: "waiting", parent_task_id: "PUN-12" },
      ],
    });
    expect(deliveryLabel(orchestration(), v)).toBe("PUN-12");
  });

  it("summarises more than two references with a remainder count", () => {
    const v = view({
      lanes: ["a", "b", "c", "d"].map((t, i) => ({
        lane_id: `l${i}`,
        project_id: "p",
        status: "runnable" as const,
        parent_task_id: t,
      })),
    });
    expect(deliveryLabel(orchestration(), v)).toBe("a, b +2 more");
  });

  it("falls back to the project slugs when no lane carries a parent task", () => {
    const v = view({
      projects: [
        { project_id: "billing", lane_ids: ["l1"], counts_by_status: {} },
        { project_id: "checkout", lane_ids: ["l2"], counts_by_status: {} },
      ],
      lanes: [{ lane_id: "l1", project_id: "billing", status: "runnable" }],
    });
    expect(deliveryLabel(orchestration(), v)).toBe("billing, checkout");
  });

  it("falls back to a shortened id only when the view carries nothing usable", () => {
    expect(deliveryLabel(orchestration(), null)).toBe("c642fb0e8a…");
    expect(deliveryLabel(orchestration(), view())).toBe("c642fb0e8a…");
  });

  it("leaves an already-short id intact", () => {
    expect(deliveryLabel(orchestration({ id: "orc-1" }), null)).toBe("orc-1");
  });
});

describe("delivery lifecycle predicates", () => {
  it("allows cancelling only a delivery still in flight", () => {
    expect(isCancellableDelivery(orchestration({ status: "pending" }))).toBe(true);
    expect(isCancellableDelivery(orchestration({ status: "active" }))).toBe(true);
    expect(isCancellableDelivery(orchestration({ status: "completed" }))).toBe(false);
    expect(isCancellableDelivery(orchestration({ status: "cancelled" }))).toBe(false);
  });
});

describe("filterDeliveries", () => {
  const rows: DeliveryListRow[] = [
    row({ id: "orc-1", title: "Migrate billing", status: "active" }),
    row(
      { id: "orc-2", status: "completed", workflow_definition_id: "wf-nightly" },
      {
        projects: [{ project_id: "checkout", lane_ids: ["l1"], counts_by_status: {} }],
        lanes: [{ lane_id: "l1", project_id: "checkout", status: "accepted", parent_task_id: "PUN-99" }],
      },
    ),
  ];

  it("returns every row for an empty query", () => {
    expect(filterDeliveries(rows, "  ")).toHaveLength(2);
  });

  it("matches the title, the full id, and the status", () => {
    expect(filterDeliveries(rows, "Migrate")[0].orchestration.id).toBe("orc-1");
    expect(filterDeliveries(rows, "orc-2")[0].orchestration.id).toBe("orc-2");
    expect(filterDeliveries(rows, "completed")[0].orchestration.id).toBe("orc-2");
  });

  it("matches project ids, parent task ids, and the workflow definition", () => {
    expect(filterDeliveries(rows, "checkout")[0].orchestration.id).toBe("orc-2");
    expect(filterDeliveries(rows, "PUN-99")[0].orchestration.id).toBe("orc-2");
    expect(filterDeliveries(rows, "wf-nightly")[0].orchestration.id).toBe("orc-2");
  });

  it("is case-insensitive and returns nothing when unmatched", () => {
    expect(filterDeliveries(rows, "migrate")).toHaveLength(1);
    expect(filterDeliveries(rows, "nope")).toEqual([]);
  });
});

describe("sortDeliveries", () => {
  const rows: DeliveryListRow[] = [
    row({ id: "a", title: "Beta", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-05-01T00:00:00Z" }),
    row({ id: "b", title: "Alpha", created_at: "2026-03-01T00:00:00Z", updated_at: "2026-04-01T00:00:00Z" }),
  ];
  const ids = (list: DeliveryListRow[]) => list.map((r) => r.orchestration.id);

  it("puts the most recently updated first", () => {
    expect(ids(sortDeliveries(rows, "updated"))).toEqual(["a", "b"]);
  });

  it("puts the most recently created first", () => {
    expect(ids(sortDeliveries(rows, "created"))).toEqual(["b", "a"]);
  });

  it("sorts by the rendered label, not the raw id", () => {
    expect(ids(sortDeliveries(rows, "title"))).toEqual(["b", "a"]);
  });

  it("groups by status and breaks ties by label", () => {
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
