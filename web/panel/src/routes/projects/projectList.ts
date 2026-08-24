// Client-side search/sort for the projects list. The list endpoint returns
// every project in one shot and the count stays small (a Punakawan instance
// tracks workspaces, not tickets), so filtering here keeps typing instant and
// avoids a request per keystroke.

import type { ProjectSummary } from "../../lib/api/client";

export type ProjectSortKey = "name" | "repositories";

export interface ProjectSortOption {
  key: ProjectSortKey;
  label: string;
}

// ProjectSummary carries no timestamp, so there is no "recently updated"
// option to offer here. The count fields are the closest thing the payload
// has to an activity signal, and each is useful on its own.
export const projectSortOptions: ProjectSortOption[] = [
  { key: "name", label: "Name (A–Z)" },
  { key: "repositories", label: "Most repositories" },
];

function displayName(p: ProjectSummary): string {
  return p.name || p.id;
}

// Matches the fields a human would actually type: the name and id they know
// the project by, plus the description and path they might recognise it from.
export function filterProjects(projects: ProjectSummary[], query: string): ProjectSummary[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return projects;
  return projects.filter((p) =>
    [p.name, p.id, p.description, p.path].some((field) => (field ?? "").toLowerCase().includes(needle)),
  );
}

export function sortProjects(projects: ProjectSummary[], key: ProjectSortKey): ProjectSummary[] {
  // Name is the tie-breaker for every count sort so equal counts still come
  // out in a stable, predictable order rather than registry order.
  const byName = (a: ProjectSummary, b: ProjectSummary) =>
    displayName(a).localeCompare(displayName(b), undefined, { sensitivity: "base" });

  const descBy = (pick: (p: ProjectSummary) => number) => (a: ProjectSummary, b: ProjectSummary) =>
    pick(b) - pick(a) || byName(a, b);

  const comparators: Record<ProjectSortKey, (a: ProjectSummary, b: ProjectSummary) => number> = {
    name: byName,
    repositories: descBy((p) => p.repository_count),
  };

  return [...projects].sort(comparators[key]);
}
