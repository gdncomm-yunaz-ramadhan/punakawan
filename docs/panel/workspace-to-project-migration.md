# Panel: workspace → project migration

The panel's primary UI entity is now the **project** (see
`punakawan-panel-project-performance-improvement-plan.md`). "Workspace" remains
the physical backing directory, but users and clients should address projects.

## Registry migration

`workspaces.yaml` is versioned (`punakawan.workspace-registry/v1`). On load:

- A file with an **empty/missing** version is treated as legacy and upgraded to
  `v1` in place; the upgrade is persisted on the next write.
- A file already at `v1` loads unchanged.
- A file with an **unknown/newer** version is rejected with
  `ErrUnsupportedRegistryVersion` rather than being silently coerced, so a
  registry written by a newer panel is never corrupted by an older one.

No manual migration step is required for existing `v1` or legacy files.

## API routes are project-scoped only

The bare `GET /api/v1/workspaces` and `GET /api/v1/workspaces/{workspaceId}`
list/detail routes this doc used to document as a deprecated alias are gone —
there is no workspace-scoped equivalent left for them, and no `Deprecation`
header to look for. Use `GET /api/v1/projects` and
`GET /api/v1/projects/{projectId}`.

The `/api/v1/workspaces/{id}/{sub}` sub-resource routes have likewise been
consolidated to their `/api/v1/projects/{id}/{sub}` equivalents — sessions,
tasks, task graphs, knowledge, and evidence are all reachable only project-
scoped now (Panel UI: `ProjectDetail.svelte`'s Sessions/Tasks/Knowledge tabs).
Project ids equal the registry workspace ids, so the `{workspaceId}` path
value on these routes is a project id in practice.

Approvals is the one exception: `GET /api/v1/workspaces/{workspaceId}/approvals`
still exists alongside `GET /api/v1/projects/{workspaceId}/approvals` — both
serve the same reader, and the Panel's `ApprovalsList.svelte` component is
shared between the top-level Approvals view and `ProjectDetail`'s Approvals
tab rather than duplicated, so there was nothing to consolidate here.

Project-scoped routes that already exist beyond the above:
`/projects/{id}/metadata`, `/projects/{id}/workflows`, `/projects/{id}/plans`,
`/projects/{id}/health`, `/projects/{id}/context-improvements`, and the
project-scoped artifact-review-resolution actions under
`/projects/{id}/reviews/{reviewId}/proposals/{proposalId}/{accept,reject}`.
