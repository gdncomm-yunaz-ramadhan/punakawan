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

## Deprecated API aliases

The canonical read routes are now under `/api/v1/projects`:

| Deprecated alias | Canonical successor |
|---|---|
| `GET /api/v1/workspaces` | `GET /api/v1/projects` |
| `GET /api/v1/workspaces/{workspaceId}` | `GET /api/v1/projects/{projectId}` |

The deprecated routes still work but respond with:

```
Deprecation: true
Link: <…successor…>; rel="successor-version"
```

Project ids equal the registry workspace ids, so a client can switch by
swapping the path prefix. The `/api/v1/workspaces/{id}/{sub}` sub-resource
routes (sessions, tasks, knowledge, evidence, approvals) are **not** deprecated
— they have no `/projects` equivalent yet and remain the way to reach those.

Project-scoped equivalents that already exist: `/projects/{id}/metadata`,
`/projects/{id}/workflows`, `/projects/{id}/plans`, `/projects/{id}/health`,
and the project-scoped artifact review protocol under
`/projects/{id}/artifacts` and `/projects/{id}/reviews`.
