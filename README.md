# Punakawan

Go core + TypeScript adapter platform that turns documents and requirements into
verified knowledge, implementation plans, executable work items, code changes,
tests, and evidence.

See [`punakawan-go-typescript-detailed-plan.md`](./punakawan-go-typescript-detailed-plan.md)
for the full engineering plan, architecture, and milestone roadmap.

## Status

Jira-first MVP. Punakawan can read/search Jira and Confluence, run its
Semar/Gareng/Petruk/Bagong assessment and planning roles through a connected
MCP client, create a durable Beads task graph, synchronize non-duplicate Jira
subtasks, and update Jira estimates, worklogs, comments, and workflow status.

Punakawan does not bundle an LLM. The connected MCP client is the reasoning
engine; Punakawan supplies prompts, orchestration, persistence, adapters, and
approval gates.

## Install on macOS

The global installer installs missing prerequisites, builds Punakawan and its
Atlassian adapter, collects Jira credentials outside git-tracked projects, and
opens a wizard to integrate `punakawan` with Codex, Claude Code, both, another
STDIO MCP client, or no client yet:

```bash
./scripts/install.sh
```

The final wizard offers Codex, Claude Code, both, a generic STDIO MCP config,
or skip. To add or change clients later, rerun only the integration wizard:

```bash
./scripts/configure-agent.sh "$HOME/Library/Application Support/punakawan/run-mcp.sh"
```

For automated provisioning, set `PUNAKAWAN_AGENT_SELECTION` to `codex`,
`claude`, `both`, `generic`, or `skip`. Set `PUNAKAWAN_DRY_RUN=1` to preview
registration commands without changing client configuration.

Before setup, an Atlassian organization admin must enable API-token
authentication for the Rovo MCP server. The installer accepts either:

- a personal API token plus Atlassian account email; or
- a service-account API key without an email.

It also asks for the site host (for example `yourteam.atlassian.net`) and
derives the cloud ID automatically. No per-project Punakawan file is required;
an optional `.punakawan/workspace.yaml` can override global defaults.

## Jira MVP workflow

Open the agent client selected during installation in any git repository and
ask it to use Punakawan for a Jira issue, for example:

> Use Punakawan to read PAY-123, assess feasibility and risks with Semar and
> Gareng, produce an implementation plan with Petruk, create the Beads tasks
> and non-duplicate Jira subtasks, and set the original estimates.

The connected client can use these MCP surfaces:

- `call_adapter_operation` for Jira/Confluence reads and advanced operations;
- the `semar`, `gareng`, `petruk`, and `bagong` prompts plus their `submit_*`
  tools for durable assessment, planning, and review;
- `submit_task_graph` for executable Beads work items;
- `sync_jira_subtasks` for deduplicated Jira subtask creation; and
- `update_jira_task_progress` for estimates, worklogs, and comments.

The first adapter write in a run asks for inline human approval. One approval
covers every approval-required adapter write in that run. If the connected
client cannot show MCP elicitation, Punakawan returns the exact CLI fallback:

```bash
punakawan approvals list
punakawan approvals approve <id> --by <your-name>
```

## Development

```bash
make bootstrap   # install Go/TS dependencies
make build       # build all packages
make test        # run all tests
```

Manual development requires Go 1.26+, Node 20+, and pnpm.
