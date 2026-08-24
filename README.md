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
start_delivery              get_delivery
answer_delivery_question    cancel_delivery           approve_project_delivery
```

`invoke_workflow` resolves a Workflow into a Plan and Delivery. Coding agents
own ordinary file, shell, Git, worktree, test, and pull-request operations.
Punakawan does not duplicate those tools.

## Build and connect

Requirements: Go 1.26+, Node.js 20+, and pnpm.

```bash
make bootstrap
make build
make package
```

Configure your MCP client to run the built server over STDIO:

```json
{
  "mcpServers": {
    "punakawan": {
      "command": "/absolute/path/to/dist/punakawan",
      "args": ["mcp", "serve"]
    }
  }
}
```

The MCP server may start outside a project. Use `upsert_project` to register
repository identity before starting delivery work.

## Panel

```bash
dist/punakawan panel --workspace /absolute/path/to/project
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
