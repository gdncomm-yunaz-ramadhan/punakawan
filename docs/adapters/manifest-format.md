# Adapter Manifest Format

Every Punakawan adapter ships a manifest — a YAML file describing the
adapter's identity, transport, capabilities, and permissions. The Go core
supervisor reads this manifest before it will start, grant capabilities to,
or execute operations against an adapter process.

The canonical schema for this file is
[`protocol/adapter.schema.json`](../../protocol/adapter.schema.json)
(JSON Schema draft 2020-12). This document explains the format for adapter
authors in plain language; the schema is the source of truth for validation.
A worked example lives at
[`examples/atlassian-playwright/adapter-manifest.example.yaml`](../../examples/atlassian-playwright/adapter-manifest.example.yaml).

## Fields

### `id`

A stable, unique identifier for the adapter, e.g. `atlassian-cloud`. Must
match `^[a-z0-9]+(-[a-z0-9]+)*$` — lowercase alphanumeric segments joined by
single hyphens. This is the identifier the Go core uses to refer to the
adapter internally (process naming, capability grants, evidence records).

### `name`

A human-readable display name for the adapter, e.g. `Atlassian Cloud
Adapter`. Any non-empty string. Used in logs, approval prompts, and
documentation surfaces — not parsed programmatically.

### `version`

The adapter's own semantic version, e.g. `0.1.0`. Must match
`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$` (semver `MAJOR.MINOR.PATCH` with an
optional pre-release suffix). This versions the adapter implementation
itself, independent of the protocol version below.

### `protocol`

The protocol version the adapter speaks. Must be the exact string
`punakawan.adapter/v1` (this is the only accepted value today). The Go core
supervisor uses this to confirm it and the adapter agree on the wire
protocol before attempting `initialize`.

### `runtime`

The runtime the adapter process runs under. Must be `node` (the only
accepted value today). This tells the Go core supervisor how to start the
adapter process.

### `provides`

A non-empty array of capability tags describing what kind of adapter this
is, e.g.:

```yaml
provides:
  - knowledge-source
  - issue-tracker
  - documentation-system
```

These are free-form strings identifying the roles the adapter fulfills
(what kind of provider it is), which the Go core and workflow layer use to
select adapters for a given task.

### `permissions`

The full set of external access the adapter is permitted. This is the
adapter's declared blast radius — the Go core supervisor grants exactly
these capabilities to the adapter process and nothing more, as part of
the "grant capabilities" lifecycle step (see below). `permissions` requires
all three of `network`, `filesystem`, and `secrets`, even if empty.

#### `permissions.network.hosts`

An array of host patterns the adapter is allowed to reach over the
network, e.g.:

```yaml
permissions:
  network:
    hosts:
      - "*.atlassian.net"
```

An empty array means no network access is granted. Wildcards (e.g.
`*.atlassian.net`) are used to scope a whole domain rather than enumerating
individual hosts.

#### `permissions.filesystem.read` / `permissions.filesystem.write`

Two arrays of filesystem paths (or path patterns) the adapter may read
from and write to, respectively. Both are required, and either may be
empty (`[]`) to declare no filesystem access of that kind. An adapter that
only talks to remote services over the network — such as the Atlassian
Cloud adapter — declares both as empty.

#### `permissions.secrets`

An array of secret names the adapter needs injected at runtime, e.g.:

```yaml
permissions:
  secrets:
    - ATLASSIAN_ACCESS_TOKEN
```

The Go core supervisor resolves these names against its own secret store
and injects only the named secrets into the adapter process — the adapter
never has broader access to the secret store itself.

### `operations`

A map of operation name to operation definition. At least one operation is
required. Operation names are adapter-defined (e.g. `jira.search`,
`confluence.update`) and are what the Go core supervisor invokes via the
`execute` protocol message.

Each operation entry has:

#### `operations.<name>.side_effect`

A required boolean. `false` means the operation is read-only / safe to run
without side effects (e.g. `jira.search`, `confluence.read`). `true` means
the operation changes state in the target system (e.g. `jira.create`,
`confluence.update`).

#### `operations.<name>.approval`

An optional field. The only accepted value is the string `required`. When
present, the Go core supervisor must obtain approval (via the
`approval_request` protocol message) before invoking that operation.
Operations with `side_effect: true` are expected to also declare
`approval: required`, so a state-changing call is never executed
unattended.

## How the Go core supervisor uses the manifest

The manifest is consulted at each stage of the adapter lifecycle described
in plan §5.2:

```text
Go Core
  ├── starts adapter process
  ├── sends initialize
  ├── validates manifest
  ├── grants capabilities
  ├── executes operation
  ├── receives structured result
  ├── records evidence and events
  └── stops or reuses adapter
```

1. **Starts adapter process** — the supervisor reads `runtime` to know how
   to launch the adapter (currently always a `node` process).
2. **Sends `initialize`** and **validates manifest** — the supervisor
   confirms `protocol` matches the version it speaks, and validates the
   manifest document against `protocol/adapter.schema.json` before trusting
   anything else in it. A manifest that fails schema validation, or whose
   `protocol` does not match, is rejected and the adapter is not started
   further.
3. **Grants capabilities** — the supervisor grants exactly the access
   listed under `permissions` (network hosts, filesystem read/write paths,
   named secrets) to the adapter process. Nothing outside the declared
   `permissions` is made available.
4. **Executes operation** — when a workflow needs one of the capabilities
   listed in `provides`, the supervisor sends an `execute` message naming
   one of the keys under `operations`. If that operation's
   `approval: required` is set, the supervisor issues an `approval_request`
   and waits before executing.
5. **Receives structured result** — the adapter returns its result over the
   same protocol.
6. **Records evidence and events** — the supervisor records `evidence` and
   `event` messages tied to the operation invocation, associating them with
   the adapter `id` and `version` from the manifest for provenance.
7. **Stops or reuses adapter** — the supervisor either issues `shutdown` or
   keeps the process warm for a subsequent `execute` call, depending on
   scheduling policy.

## Validating a manifest

A manifest YAML file is valid if and only if it validates against
[`protocol/adapter.schema.json`](../../protocol/adapter.schema.json). Per
plan §5.5, that JSON Schema file is canonical — Go structs, TypeScript
interfaces/Zod validators, protocol documentation, and example payloads are
generated from it, and CI rejects changes where generated code is stale.
This document does not redefine or extend the schema; if the two ever
disagree, the schema wins.
