<p align="center">
  <img src="assets/punakawan-colored.png" alt="Punakawan" width="300" />
</p>

<h1 align="center">Punakawan</h1>

Punakawan is a lean MCP orchestration server for durable software delivery. A
connected coding agent does the reasoning and implementation; Punakawan owns
only the coordination state needed to move from project and workflow to plan
and delivery.

## Core model

- **Project** identifies a repository and its default branch.
- **Workflow** is a reusable, role-aware delivery definition.
- **Plan** is an immutable, reviewable revision of intended work.
- **Delivery** records execution state, clarification questions, workers,
  commits, pull requests, verification, review, Jira activity, and timeline
  events.

Semar orchestrates, Gareng challenges risk and feasibility, Petruk plans and
implements, and Bagong independently reviews the result. A connected agent
executes complete, authorized work without asking for confirmation; it only
returns `needs_input` when required context is missing or a decision has
more than one defensible outcome. An adapter operation declaring
`side_effect: true` is meant to route that write through schema validation,
a durable outbox, and an audit trail on the way to the target system - it
never means the write is held for a human to confirm.

A delivery's identity comes from one of two sources. A Jira source is keyed
by `(provider, tenant, canonical issue key)`: starting the same Jira issue
again reuses its existing non-cancelled delivery and adds a new session to
it, and only starts a new delivery once the previous one is cancelled. An
ad-hoc source has no external key at all - every ad-hoc start creates a new
delivery unless the caller supplies the exact resume token an earlier call
returned. Every plan a delivery produces (a high-level plan, and one per
attached project) is an immutable `(plan_id, revision)`; a delivery's link
always names the exact revision it was created against, and that same link
is queryable from the project side too, so a plan's history never
retroactively changes what an existing delivery says it did.

## Quick start

```text
install -> setup credentials/hooks -> start daemon/panel -> doctor -> start_delivery
```

```bash
bash scripts/install.sh          # or scripts/install.ps1 on Windows
punakawan setup                  # credentials + lifecycle hooks
punakawan daemon start           # or: punakawan panel --workspace /absolute/path/to/project
punakawan doctor                 # verify every check before trusting the install
```

Then call `start_delivery` (Jira key or ad-hoc prompt) from a connected agent.

## MCP tools

The public surface is intentionally small:

```text
upsert_project              list_projects
save_workflow               get_workflow              list_workflows
invoke_workflow             plan_save                 plan_get
start_delivery              get_delivery              log_delivery_work
answer_delivery_question    cancel_delivery
```

`invoke_workflow` resolves a Workflow into a Plan and Delivery. Coding agents
own ordinary file, shell, Git, worktree, test, and pull-request operations.
Punakawan does not duplicate those tools.

## Install

Clone the repository, then run the installer for your platform.

macOS:

```bash
git clone https://github.com/ygrip/punakawan.git
cd punakawan
bash scripts/install.sh
```

Windows PowerShell:

```powershell
git clone https://github.com/ygrip/punakawan.git
Set-Location punakawan
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

The installers detect Go, Node.js, and pnpm; install missing prerequisites
through Homebrew or winget; build the panel assets; and install `punakawan`
and `punakawand` into a user-local directory on `PATH`. If automatic setup
fails, the installer prints the exact manual command and documentation link.

They also detect Codex and Claude Code, replace any existing user-level
`punakawan` MCP registration, and register the installed binary as
`punakawan mcp serve`. Restart detected clients after installation. Missing or
failed clients do not undo the binary installation; the installer prints the
exact manual registration command instead.

A generic MCP configuration is always written for other clients:

- macOS: `~/Library/Application Support/punakawan/mcp-config.json`
- Windows: `%APPDATA%\punakawan\mcp-config.json`

Both installers also build the Atlassian and GitHub adapters and deploy each
one, together with its production dependencies, below the same directory
(`.../punakawan/adapters/atlassian`, `.../punakawan/adapters/github`) - never
inside this checkout. Global adapter wiring is recorded in that directory's
`config.yaml`, always naming the deployed copy; re-running the installer
replaces the deployed files and refreshes that entry, never leaving a stale
path behind. Set `PUNAKAWAN_DATA_DIR` before installing to relocate all of
this - the storage kernel, the adapter trust file, the telemetry spool, and
everything above - to a prefix of your choosing; the built binary resolves
the exact same directory at runtime, so nothing here can point back at a
clone that later moves or disappears.

Use `--dry-run` on macOS or `-DryRun` on Windows to preview every action.
Open a new shell after installation, then verify on macOS:

```bash
command -v punakawan punakawand
punakawan --help
```

Or on Windows:

```powershell
Get-Command punakawan, punakawand
punakawan --help
```

Manual source installation requires Go 1.26+, Node.js 20+, and pnpm:

```bash
make bootstrap
make panel-build
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install ./cmd/punakawan ./cmd/punakawand
```

For a client not detected by the installer, configure the installed server
over STDIO:

```json
{
  "mcpServers": {
    "punakawan": {
      "command": "punakawan",
      "args": ["mcp", "serve"]
    }
  }
}
```

If the MCP client does not inherit your shell `PATH`, use the absolute path to
the installed `punakawan` binary as `command`.

The MCP server may start outside a project. Use `upsert_project` to register
repository identity before starting delivery work.

## Credentials, hooks, and doctor

```bash
punakawan setup   # resolve, validate, and durably save credentials + hooks
punakawan doctor  # verify storage, the daemon, adapters, hooks, and panel assets
```

`setup` resolves `ATLASSIAN_HOST`, `ATLASSIAN_EMAIL` (skipped for a scoped
service-account token), `ATLASSIAN_API_TOKEN`, and `GITHUB_TOKEN` from the
environment or the durable global credential file, prompting for anything
still missing when run interactively. Each value is checked with a real
authenticated read (Atlassian site/user metadata, GitHub `GET /user`) before
it is saved as a reference in that host-owned credential file - never
plaintext in a project's own `workspace.yaml`. `setup` never opens a
credentialed subshell: a value exported only into one interactive session is
not durable. It also installs Codex and Claude Code lifecycle telemetry
hooks at the user level, so every project benefits without per-project setup.

`doctor --json` reports the real state of every dependency a working install
needs:

- `storage`/`daemon`: the storage kernel opens, and the daemon is reachable.
- `adapters.{atlassian,github}.{entrypoint,handshake,credentials,connectivity}`:
  the deployed adapter is trusted and present, its process completes an
  `initialize` handshake, its credentials are present, and a live
  authenticated read against the provider succeeds.
- `telemetry.{codex,claude_code}`: `missing` (no hook configuration at all),
  `incomplete` (hooks installed, but a live probe into the spool/database
  could not be confirmed - typically because the client has not yet trusted
  the hook, which both clients require by their own security model), or
  `complete` (a probe event reached the spool and the database). Only
  `complete` supports "guaranteed telemetry" for that client.
- `panel_assets`/`workflow_storage`: the embedded panel bundle is present,
  and workflow definitions persist durably even with no project in scope.

No credential value is ever printed by either command.

## Panel

```bash
punakawan panel --workspace /absolute/path/to/project
```

The loopback-only panel focuses on Projects, Deliveries, and Settings. Project
detail contains Summary, Plans, Workflows, Knowledge, and Settings; role policy
and diagnostics live under Settings. Delivery detail is the main human-facing
audit artifact. The panel uses plain request/response loading and has no live
push channel.

## CLI

The binary keeps only operational commands:

```text
punakawan workspace ...
punakawan doctor
punakawan mcp serve
punakawan panel ...
punakawan daemon ...
```

Project delivery is driven through MCP, not duplicated as a generic CLI.

## Development

```bash
make build
make package
make test
make lint
make panel-test
```

This repository uses `bd` for task tracking. Run `bd prime` for the local
workflow.

## Name

The Punakawan—Semar, Gareng, Petruk, and Bagong—are wayang companions who
advise, question, translate hard truths, and keep the hero honest. Punakawan
serves that same role for a coding agent without replacing it.
