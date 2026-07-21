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

Punakawan calls Jira Cloud REST API v3 directly; it does not require or use
Rovo MCP. The installer accepts an unscoped personal API token, a scoped
personal token, or a scoped service-account token. Personal tokens also use
the Atlassian account email. Scoped tokens should include `read:jira-work`
and `write:jira-work`; every token remains limited by its account's Jira
project permissions.

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

### Jira authentication

The installer stores `ATLASSIAN_API_TOKEN`, `ATLASSIAN_HOST`, and, for a
personal token, `ATLASSIAN_EMAIL`. It also records whether the token is
scoped. Unscoped personal tokens call `https://<site>.atlassian.net`; scoped
personal and service-account tokens call
`https://api.atlassian.com/ex/jira/<cloudId>`.

HTTP 401/403 errors mean the direct token, configured mode/scopes, account
product access, or Jira project permissions need correction. See
[Atlassian's API-token guide](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
and [Jira REST v3 documentation](https://developer.atlassian.com/cloud/jira/platform/rest/v3/intro/).

## Development

```bash
make bootstrap   # install Go/TS dependencies
make build       # build all packages
make test        # run all tests
```

Manual development requires Go 1.26+, Node 20+, and pnpm.
