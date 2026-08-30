<script lang="ts">
  import { getDeliveryDetail, watchDeliveryDetail, cancelDelivery, type DeliveryDetail } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import TableCard from "../../lib/components/cards/TableCard.svelte";
  import DataTable from "../../lib/components/data/DataTable.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import type { IconName } from "../../lib/components/Icon.svelte";
  import Tabs from "../../lib/components/Tabs.svelte";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";
  import { backoffDelay } from "./deliveryList";

  interface Props {
    orchestrationId: string;
  }
  let { orchestrationId }: Props = $props();

  let detail: DeliveryDetail | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);
  let confirmingCancel = $state(false);

  const WATCH_WAIT_SECONDS = 25;

  function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }

  async function loadOnce(id: string): Promise<boolean> {
    try {
      detail = await getDeliveryDetail(id);
      error = null;
      return true;
    } catch (e) {
      // Never clear already-loaded detail on a refresh failure.
      if (!detail) error = e instanceof Error ? e.message : String(e);
      return false;
    }
  }

  // watchLoop is this page's live-refresh mechanism: fetch once, then
  // long-poll the watch endpoint by the last projection_revision,
  // looping until the effect below aborts it (unmount or a changed
  // orchestrationId). Transport errors retry with bounded backoff that
  // resets on any successful response; a successful response is never
  // followed by clearing detail, even mid-backoff.
  async function watchLoop(id: string, signal: AbortSignal) {
    loading = true;
    await loadOnce(id);
    loading = false;
    if (signal.aborted) return;

    let failures = 0;
    while (!signal.aborted) {
      const sinceRevision = detail?.projection_revision ?? 0;
      try {
        const next = await watchDeliveryDetail(id, sinceRevision, WATCH_WAIT_SECONDS, signal);
        if (signal.aborted) return;
        detail = next;
        error = null;
        failures = 0;
      } catch (e) {
        if (signal.aborted) return;
        failures += 1;
        await sleep(backoffDelay(failures - 1));
      }
    }
  }

  $effect(() => {
    const id = orchestrationId;
    const controller = new AbortController();
    watchLoop(id, controller.signal);
    return () => controller.abort();
  });

  async function cancel() {
    if (!detail) return;
    cancelling = true;
    cancelError = null;
    try {
      detail = await cancelDelivery(orchestrationId, { expected_revision: detail.orchestration_revision });
      confirmingCancel = false;
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  function formatDate(value?: string): string {
    return value ? new Date(value).toLocaleString() : "Not recorded";
  }
  function formatDuration(ms: number): string {
    const minutes = Math.max(0, Math.floor(ms / 60_000));
    const hours = Math.floor(minutes / 60);
    return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
  }
  function sessionDuration(startedAt: string, endedAt?: string): string {
    const started = Date.parse(startedAt);
    const ended = endedAt ? Date.parse(endedAt) : Date.now();
    return Number.isFinite(started) && Number.isFinite(ended) ? formatDuration(ended - started) : "Not recorded";
  }
  function formatCosts(costs: Record<string, number>, pricingComplete: boolean): string {
    const entries = Object.entries(costs ?? {});
    if (entries.length === 0) return "No estimate";
    const formatted = entries
      .map(([currency, amount]) => new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amount))
      .join(" · ");
    return pricingComplete ? formatted : `${formatted} (partial - some usage has unknown pricing)`;
  }

  const statusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };

  // Tabs are computed from what the loaded detail actually has, per the
  // "do not render a tab with no applicable data" rule - Overview is
  // always shown, every other tab only appears once there is something
  // to show in it.
  const tabs = $derived.by(() => {
    if (!detail) return [];
    const list: { id: string; label: string; icon: IconName }[] = [
      { id: "overview", label: "Overview", icon: "dashboard" as IconName },
    ];
    if (detail.plan_detail) list.push({ id: "plan", label: "Plan", icon: "file" as IconName });
    if (detail.projects.length) list.push({ id: "projects", label: "Projects", icon: "folder" as IconName });
    if (detail.jira) list.push({ id: "jira", label: "Jira", icon: "git-branch" as IconName });
    if (detail.github) list.push({ id: "github", label: "GitHub", icon: "git-branch" as IconName });
    if (detail.sessions && detail.sessions.length) list.push({ id: "sessions", label: "Sessions", icon: "users" as IconName });
    if (detail.activity.length) list.push({ id: "activity", label: "Activity", icon: "activity" as IconName });
    return list;
  });

  function tabFromUrl(): string {
    if (typeof window === "undefined") return "overview";
    return new URL(window.location.href).searchParams.get("tab") ?? "overview";
  }
  let activeId = $state(tabFromUrl());
  $effect(() => {
    if (tabs.length && !tabs.some((t) => t.id === activeId)) activeId = "overview";
  });

  function selectTab(id: string) {
    activeId = id;
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (id === "overview") url.searchParams.delete("tab");
    else url.searchParams.set("tab", id);
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  }

  function projectPlan(d: DeliveryDetail, projectId: string) {
    return (d.project_plans ?? []).find((p) => p.project_id === projectId);
  }
</script>

{#if loading && !detail}
  <p>Loading…</p>
{:else if error && !detail}
  <ErrorStateCard title="Failed to load delivery" message={error} />
{:else if detail}
  {@const d = detail}
  <PageHeader title={d.title} description={d.description || "No description recorded."}>
    {#snippet actions()}
      {#if d.cancellable}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={() => (confirmingCancel = true)}>
          Cancel delivery
        </Button>
      {/if}
    {/snippet}
  </PageHeader>
  <DeliveryCancelDialog
    open={confirmingCancel}
    label={d.title}
    {orchestrationId}
    busy={cancelling}
    error={cancelError}
    onclose={() => !cancelling && (confirmingCancel = false)}
    onconfirm={cancel}
  />
  {#if cancelError && !confirmingCancel}<p role="alert" class="error">{cancelError}</p>{/if}

  <div class="status-row">
    <StatusBadge variant={statusVariants[d.status] ?? "neutral"} label={d.status} />
    <span>Updated {formatDate(d.updated_at)}</span>
    <details><summary>Technical reference</summary><code>{orchestrationId}</code></details>
  </div>

  <Tabs {tabs} {activeId} onchange={selectTab} ariaLabel="Delivery sections" />

  {#if activeId === "overview"}
    <div id="tabpanel-overview" role="tabpanel" aria-labelledby="tab-overview" class="overview">
      <BentoGrid>
        <MetricCard size="small" columns={3} label="Source" value={d.source?.kind === "jira" ? (d.source.key ?? "Jira") : "Ad-hoc"} />
        {#if d.workflow}<MetricCard size="small" columns={3} label="Workflow" value={d.workflow.name || d.workflow.id} />{/if}
        <MetricCard size="small" columns={3} label="Tool calls" value={d.usage.tool_calls.toLocaleString()} />
        <MetricCard size="small" columns={3} label="Tokens" value={(d.usage.input_tokens + d.usage.output_tokens).toLocaleString()} />
        <MetricCard size="small" columns={3} label="Elapsed" value={formatDuration(d.usage.elapsed_ms)} />
        <MetricCard size="small" columns={3} label="Estimated cost" value={formatCosts(d.usage.estimated_costs, d.usage.pricing_complete)} />
      </BentoGrid>
      {#if d.progress}
        <div class="progress-block">
          <h2>Latest progress</h2>
          <p>{d.progress.summary}{#if d.progress.percent !== undefined} ({d.progress.percent}%){/if}</p>
          <p class="muted">Reported {formatDate(d.progress.reported_at)}</p>
        </div>
      {/if}
      {#if d.session}
        <div class="progress-block">
          <h2>Latest session</h2>
          <p>{d.session.participant || "Unknown participant"} · {d.session.provider || "unknown provider"} · {d.session.status}</p>
          <p class="muted">Started {formatDate(d.session.started_at)}{#if d.session.stopped_at}, stopped {formatDate(d.session.stopped_at)}{/if}</p>
        </div>
      {/if}
    </div>
  {:else if activeId === "plan" && d.plan_detail}
    <div id="tabpanel-plan" role="tabpanel" aria-labelledby="tab-plan">
      <h2>{d.plan_detail.objective} <code>r{d.plan_detail.revision}</code></h2>
      {#if d.plan_detail.steps?.length}
        <ol class="steps">
          {#each d.plan_detail.steps as step, i (step.id ?? i)}
            <li>
              <strong>{step.objective}</strong>
              {#if step.expected_outcome}<p>{step.expected_outcome}</p>{/if}
              {#if step.acceptance_criteria?.length}
                <ul>{#each step.acceptance_criteria as ac (ac)}<li>{ac}</li>{/each}</ul>
              {/if}
              {#if step.verification_method}<p class="muted">Verify: {step.verification_method}</p>{/if}
            </li>
          {/each}
        </ol>
      {/if}
      {#if d.plan_detail.acceptance_criteria?.length}
        <h3>Acceptance criteria</h3>
        <ul>{#each d.plan_detail.acceptance_criteria as ac (ac)}<li>{ac}</li>{/each}</ul>
      {/if}
      {#if d.plan_detail.verification}<p><strong>Verification:</strong> {d.plan_detail.verification}</p>{/if}
    </div>
  {:else if activeId === "projects"}
    <div id="tabpanel-projects" role="tabpanel" aria-labelledby="tab-projects">
      <h2>Projects</h2>
      <BentoGrid>
        <TableCard title="Projects" size="full">
          <DataTable
            columns={[
              { key: "project", label: "Project", sortable: true },
              { key: "plan", label: "Plan", render: (row) => row.plan },
            ]}
            rows={d.projects.map((project) => {
              const linked = projectPlan(d, project.id);
              return {
                id: project.id,
                project: project.slug || project.id,
                plan: linked
                  ? `${linked.plan.objective} r${linked.plan.revision}${linked.plan.revision !== linked.head_revision ? ` (head is r${linked.head_revision})` : ""}`
                  : "No project plan linked",
              };
            })}
            rowAction={{ label: "Open", onSelect: (row) => navigate(`/projects/${encodeURIComponent(row.project)}`) }}
            emptyMessage="No projects linked to this delivery."
          />
        </TableCard>
      </BentoGrid>
    </div>
  {:else if activeId === "jira" && d.jira}
    <div id="tabpanel-jira" role="tabpanel" aria-labelledby="tab-jira">
      <h2>{d.jira.issue_key}{#if d.jira.parent_status} · {d.jira.parent_status}{/if}</h2>
      <p class="muted">
        Writes: {d.jira.write_health.succeeded} succeeded, {d.jira.write_health.pending + d.jira.write_health.retrying} pending,
        {d.jira.write_health.failed} failed
      </p>
      <BentoGrid>
        {#if d.jira.touched_items.length}
          <TableCard title="Touched subtasks" size="full">
            <DataTable
              columns={[
                { key: "task", label: "Task" },
                { key: "jiraKey", label: "Jira key" },
                { key: "touches", label: "Touches", sortable: true, align: "right" },
              ]}
              rows={d.jira.touched_items.map((item) => ({
                id: item.parent_task_id,
                task: item.parent_task_id,
                jiraKey: item.jira_issue_key,
                touches: item.touch_count,
              }))}
              emptyMessage="No subtasks touched yet."
            />
          </TableCard>
        {/if}
        {#if d.jira.transitions.length}
          <TableCard title="Status transitions" size="full">
            <DataTable
              columns={[
                { key: "from", label: "From" },
                { key: "to", label: "To" },
                { key: "writeStatus", label: "Write status" },
                { key: "occurred", label: "Occurred", sortable: true },
              ]}
              rows={d.jira.transitions.map((t) => ({
                id: `${t.occurred_at}-${t.to_status}`,
                from: t.from_status || "—",
                to: t.to_status,
                writeStatus: t.status,
                occurred: formatDate(t.occurred_at),
              }))}
              emptyMessage="No status transitions recorded."
            />
          </TableCard>
        {/if}
        {#if d.jira.worklogs.length}
          <TableCard title="Worklogs" size="full">
            <DataTable
              columns={[
                { key: "summary", label: "Summary" },
                { key: "duration", label: "Duration" },
                { key: "sync", label: "Sync" },
              ]}
              rows={d.jira.worklogs.map((w) => ({
                id: w.id,
                summary: w.summary,
                duration: formatDuration(w.duration_seconds * 1000),
                sync: w.sync_status,
              }))}
              emptyMessage="No worklogs recorded."
            />
          </TableCard>
        {/if}
      </BentoGrid>
    </div>
  {:else if activeId === "github" && d.github}
    <div id="tabpanel-github" role="tabpanel" aria-labelledby="tab-github">
      <h2>{d.github.repository || "Repository not recorded"}{#if d.github.pull_request_number} #{d.github.pull_request_number}{/if}</h2>
      {#if d.github.head_sha}<p class="muted">Head <code>{d.github.head_sha}</code></p>{/if}
      <p class="muted">
        Writes: {d.github.write_health.succeeded} succeeded, {d.github.write_health.pending + d.github.write_health.retrying} pending,
        {d.github.write_health.failed} failed
      </p>
      {#if d.github.reviews.length}
        <div class="table-wrap">
          <table>
            <thead><tr><th>Verdict</th><th>Status</th><th>Recorded</th></tr></thead>
            <tbody>
              {#each d.github.reviews as review (review.id)}
                <tr><td>{review.verdict}</td><td>{review.status}</td><td>{formatDate(review.created_at)}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="muted">No reviews recorded.</p>
      {/if}
    </div>
  {:else if activeId === "sessions"}
    <div id="tabpanel-sessions" role="tabpanel" aria-labelledby="tab-sessions">
      <h2>Sessions</h2>
      <BentoGrid>
        <TableCard title="Sessions" size="full">
          <DataTable
            columns={[
              { key: "participant", label: "Participant" },
              { key: "provider", label: "Provider" },
              { key: "status", label: "Status", sortable: true },
              { key: "started", label: "Started", sortable: true },
              { key: "duration", label: "Duration" },
              { key: "checkpoints", label: "Checkpoints", align: "right" },
            ]}
            rows={(d.sessions ?? []).map((session) => ({
              id: session.id,
              participant: session.participant,
              provider: session.provider || "Not recorded",
              status: session.status,
              started: formatDate(session.started_at),
              duration: sessionDuration(session.started_at, session.ended_at),
              checkpoints: session.checkpoints.length,
            }))}
            emptyMessage="No sessions recorded."
          />
        </TableCard>
      </BentoGrid>
      <p class="muted">Aggregate tokens, tool calls, and estimated cost across every session are shown in the Overview tab.</p>
    </div>
  {:else if activeId === "activity"}
    <div id="tabpanel-activity" role="tabpanel" aria-labelledby="tab-activity">
      <h2>Activity</h2>
      <BentoGrid>
        <TableCard title="Activity" size="full">
          <DataTable
            columns={[
              { key: "index", label: "#", align: "right" },
              { key: "kind", label: "Kind", sortable: true },
              { key: "summary", label: "Summary" },
              { key: "occurred", label: "Occurred", sortable: true },
            ]}
            rows={d.activity.map((entry, i) => ({
              id: i,
              index: i + 1,
              kind: entry.kind,
              summary: entry.summary,
              occurred: formatDate(entry.occurred_at),
            }))}
            emptyMessage="No activity recorded."
          />
        </TableCard>
      </BentoGrid>
    </div>
  {/if}
{/if}

<style>
  .error {
    color: var(--color-danger);
  }
  .status-row {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    flex-wrap: wrap;
    margin-bottom: 1rem;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .status-row details {
    margin-left: auto;
  }
  .status-row summary {
    cursor: pointer;
  }
  .status-row code {
    display: block;
    margin-top: 0.35rem;
  }
  h2 {
    font-size: 1rem;
    margin: 0 0 0.75rem;
  }
  h3 {
    font-size: 0.88rem;
    margin: 1rem 0 0.5rem;
  }
  .overview {
    display: grid;
    gap: 1.25rem;
  }
  .progress-block {
    border-left: 3px solid var(--color-accent);
    padding-left: 0.85rem;
  }
  .progress-block p {
    margin: 0;
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
  .table-wrap {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }
  th,
  td {
    padding: 0.65rem 0.75rem;
    text-align: left;
    border-bottom: 1px solid var(--color-border);
    vertical-align: top;
  }
  tr:last-child td {
    border-bottom: 0;
  }
  th {
    color: var(--color-text-muted);
    font-size: 0.72rem;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    white-space: nowrap;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    word-break: break-word;
  }
  .steps {
    display: grid;
    gap: 0.75rem;
    padding-left: 1.25rem;
  }
</style>
