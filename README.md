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
- **Delivery** records execution state, questions, approvals, workers, commits,
  pull requests, verification, review, Jira activity, and timeline events.

Semar orchestrates, Gareng challenges risk and feasibility, Petruk plans and
implements, and Bagong independently reviews the result.

## MCP tools

The public surface is intentionally small:

```text
upsert_project              list_projects
save_workflow               get_workflow              list_workflows
invoke_workflow             plan_save                 plan_get
start_delivery              get_delivery              log_delivery_work
answer_delivery_question    cancel_delivery           approve_project_delivery
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
