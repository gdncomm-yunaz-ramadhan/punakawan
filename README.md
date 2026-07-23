<p align="center">
  <img src="assets/punakawan-colored.png" alt="Punakawan" width="300" />
</p>

<h1 align="center">Punakawan</h1>

<p align="center">
  <em>A Go core + TypeScript adapter platform that turns documents and requirements into
  verified knowledge, implementation plans, executable work items, code changes,
  tests, and evidence — driven by whatever LLM agent you already use.</em>
</p>

---

## What is Punakawan?

Punakawan is an **MCP (Model Context Protocol) server**. It plugs into an agent
client you already run — Claude Code, Codex, or any STDIO MCP client — and gives
that agent a disciplined workflow for taking a Jira issue (or a raw requirement)
from *"read this ticket"* all the way to *"assessed, planned, decomposed into
tracked work, code changed, tested, reviewed, and evidenced."*

Crucially, **Punakawan does not bundle an LLM and never reasons on its own**
([ADR-0016](punakawan-go-typescript-detailed-plan.md)). The connected MCP client
is the reasoning engine. Punakawan supplies the prompts, orchestration,
persistence, adapters, and approval gates — it is the *trusted data and
provenance boundary*. It validates and durably stores whatever structured result
the agent submits, and it enforces one human approval per run before any
external write (a Jira comment, a transition, a subtask) actually happens.

Think of it as the difference between an LLM that *talks* about doing the work
and a system that makes the work **durable, reviewable, and safe** across
sessions, machines, and teammates.

## The four Punakawan

<table>
  <tr>
    <td align="center" width="25%"><img src="assets/semar.png" width="120" alt="Semar" /><br/><b>Semar</b></td>
    <td align="center" width="25%"><img src="assets/gareng.png" width="120" alt="Gareng" /><br/><b>Gareng</b></td>
    <td align="center" width="25%"><img src="assets/petruk.png" width="120" alt="Petruk" /><br/><b>Petruk</b></td>
    <td align="center" width="25%"><img src="assets/bagong.png" width="120" alt="Bagong" /><br/><b>Bagong</b></td>
  </tr>
  <tr>
    <td align="center"><b>Orchestrator</b><br/>Interprets intent, gathers context from repos/Jira/Confluence, builds the dossier, decides which roles run, and merges their findings.</td>
    <td align="center"><b>Risk &amp; feasibility</b><br/>Requirement completeness, feasibility, compatibility, security, privacy, reliability, performance.</td>
    <td align="center"><b>Planner</b><br/>Challenges scope, finds simpler alternatives, weighs architecture options, and produces the implementation plan.</td>
    <td align="center"><b>Independent review</b><br/>Reviews the diff, test evidence, API compatibility, migrations, E2E flows, and unresolved tasks — separately from the planners.</td>
  </tr>
</table>

Each role is an MCP **prompt** (in [`prompts/`](prompts/)) paired with a
`submit_*` tool that validates and persists its structured output. The agent
plays the role; Punakawan keeps the record.

### The inspiration

The name comes from the **Punakawan** (also *Punokawan*) — the four
clown-servant characters of Javanese and Indonesian *wayang* (shadow-puppet
theatre): **Semar** and his sons **Gareng**, **Petruk**, and **Bagong**. In the
stories they are comic figures, but they are also the wisest characters on
stage: humble companions who advise the noble heroes, translate hard truths, and
keep everyone honest. That is exactly the role this tool plays for a developer —
not a hero replacing you, but four trusted advisors who assess, plan, and review
the work while *you* stay in charge.

## Why and when to use it

Use Punakawan when you want an agent to work a real ticket **end to end** and you
care that the result is trustworthy:

- **You want durable, multi-session work.** The assessment, plan, task graph,
  and review findings persist in a local Dolt-backed store and a Beads task
  graph — a later session or a teammate picks up where you left off instead of
  starting from zero.
- **You want a safety gate on external writes.** Every Jira/Confluence write
  (comments, transitions, subtasks, estimates, worklogs, attachments) is
  approval-gated per run. One human approval covers the run; nothing hits your
  system of record without it.
- **You want separation of planning and review.** Petruk plans; Bagong reviews
  independently. Gareng pressure-tests feasibility and risk. The structure is
  the point.
- **You want token-efficient context.** Jira reads request only planning fields,
  JQL searches cap results, ADF is flattened to plain text, and raw REST
  envelopes are omitted by default — so the model spends context on substance.

**When *not* to reach for it:** a throwaway one-line change, or a task with no
ticket, no review, and no need for a durable trail. Punakawan is scaffolding for
work that deserves scaffolding.

## How it works

Punakawan sits between your agent client and your systems of record. The agent
reasons; Punakawan validates, persists, gates writes, and keeps the output
token-efficient.

```mermaid
flowchart LR
  You([You]) -->|prompt| Agent[LLM agent client<br/>Claude Code / Codex]
  Agent <-->|MCP / STDIO| PK[Punakawan core - Go]

  subgraph Roles [Role prompts + submit_* tools]
    Semar[Semar - orchestrate]
    Gareng[Gareng - risk]
    Petruk[Petruk - plan]
    Bagong[Bagong - review]
  end

  PK --- Roles
  PK -->|normalize| ADP[TS adapters] -->|REST v3| Jira[(Jira / Confluence)]
  PK -->|BM25F| KN[(Knowledge store<br/>Dolt + Bleve)]
  PK -->|task graph| BD[(Beads)]
  PK -->|compress cmd output| RTK[RTK]
  PK -->|embedded| Panel[[Panel UI]]
  PK -.->|approval gate| You
```

### Example workflow

Working a Jira ticket end to end. Each external write waits on **one** human
approval per run; the agent runs its shell/dev commands through **RTK** to keep
command output compact.

```mermaid
sequenceDiagram
  actor U as You
  participant A as LLM agent
  participant P as Punakawan
  participant J as Jira

  U->>A: "Use Punakawan to work PAY-123"
  A->>P: call_adapter_operation (getJiraIssue, compact)
  P->>J: REST v3 read
  J-->>P: planning fields only
  P-->>A: compact issue (no raw envelope)
  A->>P: semar/gareng prompts -> submit_jira_assessment
  A->>P: petruk prompt -> submit_petruk_plan
  A->>P: submit_task_graph (durable Beads work items)
  A->>P: sync_jira_subtasks (deduped) [WRITE]
  P-->>A: approval required (run-scoped)
  A->>U: Approve / Deny?
  U-->>A: Approve
  A->>P: respond_to_adapter_approval -> retry
  P->>J: create subtasks + set estimates
  Note over A,P: implement -> run_tests (RTK-compressed) -> bagong review -> commit_task
```

Concretely, ask your agent:

> Use Punakawan to read PAY-123, assess feasibility and risks with Semar and
> Gareng, produce an implementation plan with Petruk, create the Beads tasks and
> non-duplicate Jira subtasks, and set the original estimates.

Watch it happen in the [Panel](#the-panel), and approve the single write gate
when prompted.

## The Panel

Punakawan ships a local, loopback-only **visual tracker**:

```bash
punakawan panel
```

<p align="center">
  <img src="assets/panel-overview.png" alt="Punakawan Panel — Overview" width="820" />
</p>

It renders an overview of sessions, the Beads task graph and dependencies,
knowledge records, pending approvals, and a review mode for diffs and plans —
theme-aware (light/dark), keyboard-accessible, and served entirely from the Go
binary (the Svelte frontend is embedded via `go:embed`). Nothing leaves your
machine; the listener binds to loopback and mutating routes are session- and
CSRF-gated.

The UI is one design system — theme, accent, cards, charts, tables, and the
review flow all share the same tokens (light and dark):

<p align="center">
  <img src="assets/panel-showcase.png" alt="Punakawan Panel — component/theme system" width="820" />
</p>

## Architecture in one line

Go core (orchestration, persistence, approval gates, MCP surface) + TypeScript
adapters (Atlassian normalization) + a connected LLM agent (the reasoning
engine) + Dolt-backed knowledge and Beads task graph (durable state) + an
embedded Svelte panel (visibility).

- **MCP surface:** `internal/mcpserver` exposes ~46 tools — `call_adapter_operation`
  for Jira/Confluence, the `semar`/`gareng`/`petruk`/`bagong` prompts and their
  `submit_*` tools, `submit_task_graph`, `sync_jira_subtasks`,
  `update_jira_task_progress`, `search_knowledge`, and the workflow pipeline.
- **Knowledge search:** BM25F over a Bleve index with a technical tokenizer that
  preserves identifiers, and first-class indexing of **CVE / GHSA / Sonar-rule**
  identifiers (`internal/search`).
- **Sync model:** issues live in a local Dolt DB; `bd dolt push/pull` syncs under
  `refs/dolt/data` on your git remote. See [`AGENTS.md`](AGENTS.md) and the beads
  [SYNC_CONCEPTS](https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md).

See [`punakawan-go-typescript-detailed-plan.md`](punakawan-go-typescript-detailed-plan.md)
for the full engineering plan, architecture, and milestone roadmap.

## Tech &amp; inspiration

| Layer | Tech |
|-------|------|
| Core | **Go 1.26+** — MCP server, orchestration, approval gates, panel server |
| Adapters | **TypeScript / Node 20+** (pnpm workspaces) — Atlassian normalization boundary |
| Panel UI | **Svelte + Vite + TypeScript**, embedded via `go:embed` |
| Knowledge store | **Dolt** (versioned SQL); **Bleve** for BM25F search |
| Task graph | **Beads (bd)** — durable, syncable issue tracker |
| Protocol | **MCP (Model Context Protocol)** over STDIO; JSON-Schema-generated Go structs + TS/Zod types |
| Token efficiency | **RTK (Rust Token Killer)** — compresses command output; installed by default and urged on the agent |
| Integrations | **Jira Cloud REST v3** and **Confluence** direct (no Rovo MCP); roadmap: Sonar, Trivy, OSV |

The reasoning is **BYO-LLM**: Punakawan is deliberately model-agnostic.

### Token efficiency &amp; RTK

Model context is the scarce resource, so Punakawan spends it deliberately:

- **Compact by default** — Jira reads request only planning fields, JQL results
  are capped, ADF is flattened to plain text, and raw REST envelopes are omitted
  unless you ask for `includeRaw: true`.
- **RTK-native** — **RTK (Rust Token Killer)** is installed by
  the setup script and wired in as Punakawan's command-output compressor: dev and
  test commands Punakawan runs are routed through RTK so their output costs 60–90%
  fewer tokens. Punakawan also **urges the connected agent to run its own shell
  commands through RTK** (via the MCP server instructions), so the savings extend
  to everything the agent does in the session. If `rtk` is not on `PATH`,
  Punakawan degrades gracefully to raw output.
- **Bounded knowledge** — search results, task context, and dossiers are
  scope-filtered and capped rather than dumping the whole store into context.

## Install on macOS

The global installer installs missing prerequisites, builds Punakawan and its
Atlassian adapter, collects Jira credentials outside git-tracked projects,
optionally installs security scanners (Trivy / OSV / Sonar), and opens a wizard
to integrate `punakawan` with Codex, Claude Code, both, another STDIO MCP
client, or no client yet:

```bash
./scripts/install.sh
```

The final wizard offers Codex, Claude Code, both, a generic STDIO MCP config,
or skip. To add or change clients later, rerun only the integration wizard:

```bash
./scripts/configure-agent.sh "$HOME/Library/Application Support/punakawan/run-mcp.sh"
```

For automated provisioning, set `PUNAKAWAN_AGENT_SELECTION` to `codex`,
`claude`, `both`, `generic`, or `skip`. Set `PUNAKAWAN_INSTALL_SCANNERS` to
`yes`/`no` to control the optional scanner step non-interactively, and
`PUNAKAWAN_DRY_RUN=1` to preview registration/brew commands without changing
anything.

Punakawan calls Jira Cloud REST API v3 directly; it does not require or use
Rovo MCP. The installer accepts an unscoped personal API token, a scoped
personal token, or a scoped service-account token. Personal tokens also use
the Atlassian account email. Scoped tokens should include `read:jira-work`
and `write:jira-work`; every token remains limited by its account's Jira
project permissions. It also asks for the site host (for example
`yourteam.atlassian.net`) and derives the cloud ID automatically. No per-project
Punakawan file is required; an optional `.punakawan/workspace.yaml` can override
global defaults.

### Other platforms

- **macOS** — fully supported via `scripts/install.sh` (Homebrew-based).
- **Linux** — supported by building from source (`make bootstrap && make build`).
  The installer script is macOS-only, but the Go core, TS adapters, and panel
  are portable; `os.UserConfigDir()` resolves config to `~/.config/punakawan`.
  A native Linux installer is not yet provided — register the MCP launcher with
  your client manually (see `scripts/configure-agent.sh`).
- **Windows** — **not yet supported.** The tool supervisor uses POSIX
  process-group termination (`internal/tools/supervisor.go`) and will not compile
  under `GOOS=windows` in its current form. Config resolution (`%AppData%`) and
  the panel are already portable; Windows support is tracked as follow-up work
  (a Windows supervisor backend + a PowerShell installer). WSL2 works today.

### Jira authentication

The installer stores `ATLASSIAN_API_TOKEN`, `ATLASSIAN_HOST`, and, for a
personal token, `ATLASSIAN_EMAIL`, and records whether the token is scoped.
Unscoped personal tokens call `https://<site>.atlassian.net`; scoped personal
and service-account tokens call `https://api.atlassian.com/ex/jira/<cloudId>`.

HTTP 401/403 errors mean the direct token, configured mode/scopes, account
product access, or Jira project permissions need correction. See
[Atlassian's API-token guide](https://support.atlassian.com/atlassian-account/docs/manage-api-tokens-for-your-atlassian-account/)
and [Jira REST v3 documentation](https://developer.atlassian.com/cloud/jira/platform/rest/v3/intro/).

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

Jira responses are compact by default: issue reads request only planning
fields, JQL searches return at most 20 summary rows unless `maxResults` is
set, ADF descriptions are flattened to plain text, and raw REST envelopes are
omitted. Pass `fields` when a specific Jira field (such as a site's story-point
custom field) is needed. Pass `includeRaw: true` only for diagnostics; it
intentionally costs substantially more model context.

### Approvals

The first adapter write in a run asks for inline human approval. One approval
covers every approval-required adapter write in that run. If the connected
client cannot show MCP elicitation, Punakawan tells the agent to show explicit
**Approve** and **Deny** options. Only after the human chooses may the agent
call `respond_to_adapter_approval` and retry an approved write. The CLI remains
available:

```bash
punakawan approvals list
punakawan approvals approve <id> --by <your-name>
punakawan approvals deny <id> --by <your-name>
```

## Development

```bash
make bootstrap   # install Go/TS dependencies
make build       # build all packages
make test        # run all tests
make panel-build # build the Svelte panel and embed it into the binary
```

Manual development requires Go 1.26+, Node 20+, and pnpm. This project tracks
work with **bd (beads)** — run `bd prime` for the workflow and see
[`AGENTS.md`](AGENTS.md).

## License

[MIT](LICENSE).
