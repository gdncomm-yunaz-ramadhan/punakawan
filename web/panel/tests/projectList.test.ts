import { describe, expect, it } from "vitest";
import { filterProjects, projectSortOptions, sortProjects } from "../src/routes/projects/projectList";
import type { ProjectSummary } from "../src/lib/api/client";

function project(over: Partial<ProjectSummary> & { id: string }): ProjectSummary {
  return {
    name: "",
    description: "",
    path: "",
    pinned: false,
    primary: false,
    availability: "available",
    repository_count: 0,
    active_session_count: 0,
    metadata_count: 0,
    ...over,
  };
}

const projects: ProjectSummary[] = [
  project({ id: "checkout-svc", name: "Checkout", description: "Payment flow", path: "/srv/checkout" }),
  project({ id: "billing-api", name: "Billing", description: "Invoices and dunning", path: "/srv/billing" }),
  project({ id: "atlas", name: "Atlas", description: "Internal docs", path: "/home/dev/atlas" }),
];

const ids = (list: ProjectSummary[]) => list.map((p) => p.id);

describe("filterProjects", () => {
  it("returns every project for an empty or whitespace-only query", () => {
    expect(filterProjects(projects, "")).toHaveLength(3);
    expect(filterProjects(projects, "   ")).toHaveLength(3);
  });

  it("matches on name, id, description, and path", () => {
    expect(ids(filterProjects(projects, "Checkout"))).toEqual(["checkout-svc"]);
    expect(ids(filterProjects(projects, "billing-api"))).toEqual(["billing-api"]);
    expect(ids(filterProjects(projects, "dunning"))).toEqual(["billing-api"]);
    expect(ids(filterProjects(projects, "/home/dev"))).toEqual(["atlas"]);
  });

  it("is case-insensitive and ignores surrounding whitespace", () => {
    expect(ids(filterProjects(projects, "  ATLAS  "))).toEqual(["atlas"]);
  });

  it("returns an empty list when nothing matches", () => {
    expect(filterProjects(projects, "nonexistent")).toEqual([]);
  });

  it("tolerates projects whose optional text fields are empty", () => {
    const bare = [project({ id: "bare" })];
    expect(ids(filterProjects(bare, "bare"))).toEqual(["bare"]);
    expect(filterProjects(bare, "anything")).toEqual([]);
  });
});

describe("sortProjects", () => {
  it("sorts by display name, case-insensitively", () => {
    expect(ids(sortProjects(projects, "name"))).toEqual(["atlas", "billing-api", "checkout-svc"]);
  });

  it("sorts an unnamed project by its id, since that is what the card shows", () => {
    // "zeta" is displayed as "Alpha" (its name), so it sorts under A; "beta"
    // has no name and sorts under its id.
    const list = [project({ id: "beta" }), project({ id: "zeta", name: "Alpha" })];
    expect(ids(sortProjects(list, "name"))).toEqual(["zeta", "beta"]);
  });

  it("sorts counts highest-first", () => {
    const list = [
      project({ id: "low", repository_count: 1 }),
      project({ id: "high", repository_count: 7 }),
    ];
    expect(ids(sortProjects(list, "repositories"))).toEqual(["high", "low"]);
  });

  it("breaks equal counts by name so the order is stable", () => {
    const list = [
      project({ id: "c", name: "Cherry", repository_count: 2 }),
      project({ id: "a", name: "Apple", repository_count: 2 }),
      project({ id: "b", name: "Banana", repository_count: 2 }),
    ];
    expect(ids(sortProjects(list, "repositories"))).toEqual(["a", "b", "c"]);
  });

  it("does not mutate the input array", () => {
    const original = [...projects];
    sortProjects(projects, "name");
    expect(projects).toEqual(original);
  });

  it("offers a comparator for every advertised sort option", () => {
    for (const option of projectSortOptions) {
      expect(sortProjects(projects, option.key)).toHaveLength(projects.length);
    }
  });
});
