<script lang="ts">
  import {
    getDeliveryView,
    cancelDelivery,
    deliveryEvidenceUrl,
    type DeliveryView,
    type DeliveryLaneStatus,
    type DeliveryLaneSummary,
  } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Card from "../../lib/components/cards/Card.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import DeliveryQuestionForm from "./DeliveryQuestionForm.svelte";
  import DeliveryApprovalCard from "./DeliveryApprovalCard.svelte";
  import { deliveryLabel } from "./deliveryList";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";

  interface Props {
    orchestrationId: string;
  }
  let { orchestrationId }: Props = $props();

  let view: DeliveryView | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let approvedBy = $state("");
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      const v = await getDeliveryView(id);
      view = v;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  // Single trigger for the first load and for a later orchestrationId change -
  // effects run after the first render too, so loading from onMount as well
  // would fire the same request twice per open.
  $effect(() => {
    load(orchestrationId);
  });

  function applyUpdate(v: DeliveryView) {
    view = v;
  }

  // Cancelling is irreversible, so it goes through the same confirmation the
  // deliveries list uses rather than firing on a single click.
  let confirmingCancel = $state(false);

  function startCancel() {
    cancelError = null;
    confirmingCancel = true;
  }

  function closeCancel() {
    if (cancelling) return;
    confirmingCancel = false;
    cancelError = null;
  }

  async function cancel() {
    if (!view) return;
    cancelling = true;
    cancelError = null;
    try {
      applyUpdate(await cancelDelivery(orchestrationId, { expected_revision: view.orchestration.revision }));
      confirmingCancel = false;
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      cancelling = false;
    }
  }

  function lanesFor(projectId: string): DeliveryLaneSummary[] {
    return view?.lanes.filter((l) => l.project_id === projectId) ?? [];
  }

  // stageLabel names how far a lane's current attempt has progressed
  // through the fixed Semar -> Gareng -> Petruk -> Bagong pipeline, e.g.
  // "Semar → Gareng" once Gareng's review is recorded but Petruk's plan
  // isn't yet - null before any stage has been recorded at all.
  function stageLabel(lane: DeliveryLaneSummary): string | null {
    const stages: [string, string | undefined][] = [
      ["Semar", lane.semar_record_id],
      ["Gareng", lane.gareng_record_id],
      ["Petruk", lane.petruk_record_id],
      ["Bagong", lane.bagong_record_id],
    ];
    const reached = stages.filter(([, id]) => id).map(([name]) => name);
    return reached.length ? reached.join(" → ") : null;
  }

  function shortSha(sha: string): string {
    return sha.slice(0, 8);
  }

  function formatDate(value: string): string {
    return new Date(value).toLocaleString();
  }

  function formatDuration(seconds: number): string {
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    return hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`;
  }

  function formatAmount(amount: number, currency: string): string {
    return `${currency} ${amount.toFixed(2)}`;
  }

  function totalsByCurrency(entries: { amount: number; currency: string }[]): Record<string, number> {
    return entries.reduce<Record<string, number>>((totals, { amount, currency }) => {
      totals[currency] = (totals[currency] ?? 0) + amount;
      return totals;
    }, {});
  }

  const orchestrationStatusVariants: Record<string, BadgeVariant> = {
    pending: "neutral",
    active: "info",
    completed: "success",
    cancelled: "danger",
  };

  const laneStatusVariants: Record<DeliveryLaneStatus, BadgeVariant> = {
    waiting: "neutral",
    blocked: "warning",
    runnable: "info",
    leased: "info",
    running: "info",
    review: "warning",
    accepted: "success",
    failed: "danger",
  };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load delivery" message={error} />
{:else if view}
  {@const v = view}
  <PageHeader title={deliveryLabel(v.orchestration, v)} description={v.next_action}>
    {#snippet actions()}
      {#if v.orchestration.status === "pending" || v.orchestration.status === "active"}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={startCancel}>Cancel delivery</Button>
      {/if}
    {/snippet}
  </PageHeader>

  <DeliveryCancelDialog
    open={confirmingCancel}
    label={deliveryLabel(v.orchestration, v)}
    orchestrationId={orchestrationId}
    busy={cancelling}
    error={cancelError}
    onclose={closeCancel}
    onconfirm={cancel}
  />

  <!-- A cancel failure while the dialog is closed (it closes on success) still
       needs somewhere to show. -->
  {#if cancelError && !confirmingCancel}
    <p role="alert" class="error">{cancelError}</p>
  {/if}

  <div class="status-row">
    <StatusBadge
      variant={orchestrationStatusVariants[v.orchestration.status] ?? "neutral"}
      label={v.orchestration.status}
    />
    <!-- The id is no longer the page title, so it is surfaced here (in a
         monospace run that selects cleanly) to stay copyable for CLI use. -->
    <span class="meta"><code class="id">{orchestrationId}</code></span>
    <span class="meta">revision {v.orchestration.revision} · latest seq {v.latest_seq}</span>
  </div>

  <!-- Prose is never derived, so a delivery nobody wrote a description for has
       nothing to show here and the block is left out entirely rather than
       rendering an empty paragraph. -->
  {#if v.description}
    <p class="description" data-testid="delivery-description">{v.description}</p>
  {/if}

  <!-- The session and the plan record are questions a reader comes here to
       answer either way, so both rows always render and an unrecorded one says
       so in muted text - unlike the description, which simply disappears. -->
  <dl class="references" data-testid="delivery-references">
    <dt>Session</dt>
    <dd>
      {#if v.session_id}
        <code class="id">{v.session_id}</code>
      {:else}
        <span class="unset">Not recorded</span>
      {/if}
    </dd>
    <dt>Plan</dt>
    <dd>
      {#if v.plan_id}
        <code class="id">{v.plan_id} r{v.plan_revision ?? 1}</code>
      {:else if v.plan_record_id}
        <code class="id">{v.plan_record_id}</code>
      {:else}
        <span class="unset">Not recorded</span>
      {/if}
    </dd>
  </dl>

  <BentoGrid>
    <MetricCard size="small" columns={3} label="Projects" value={v.projects.length} />
    <MetricCard size="small" columns={3} label="Lanes" value={v.lanes.length} />
    <MetricCard
      size="small"
      columns={3}
      label="Blocked lanes"
      value={v.blockers.length}
      accent={v.blockers.length > 0 ? "danger" : "none"}
    />
    <MetricCard
      size="small"
      columns={3}
      label="Pending approvals"
      value={v.pending_approvals.length}
      accent={v.pending_approvals.length > 0 ? "gold" : "none"}
    />
    <MetricCard size="small" columns={3} label="Logged work" value={formatDuration(v.worklog_seconds ?? 0)} />
  </BentoGrid>

  {#if (v.worklogs?.length ?? 0) > 0}
    <section aria-labelledby="worklogs-heading">
      <h2 id="worklogs-heading">Logged work</h2>
      <ul class="timeline">
        {#each v.worklogs ?? [] as worklog (worklog.id)}
          <li>
            <Card>
              {#snippet children()}
                <div class="timeline-header">
                  <strong>{worklog.jira_issue_key}</strong>
                  <span class="meta">{formatDuration(worklog.duration_seconds)} · {worklog.sync_status}</span>
                </div>
                <p>{worklog.summary}</p>
                <p class="meta">
                  {formatDate(worklog.started_at)}
                  {#if worklog.session_id} · session <code class="id">{worklog.session_id}</code>{/if}
                  {#if worklog.jira_worklog_id} · Jira worklog <code class="id">{worklog.jira_worklog_id}</code>{/if}
                </p>
              {/snippet}
            </Card>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if v.lifecycle}
    {@const lifecycle = v.lifecycle}
    {@const budgetTotals = totalsByCurrency(lifecycle.budgets)}
    <section aria-labelledby="lifecycle-heading">
      <h2 id="lifecycle-heading">Delivery lifecycle</h2>
      <dl class="references lifecycle-overview">
        <dt>Jira case</dt>
        <dd><code class="id">{lifecycle.case.jira_issue_key}</code> · {lifecycle.case.status}</dd>
        <dt>Source</dt>
        <dd><code class="id">{lifecycle.case.jira_source_key}</code></dd>
        <dt>Execution</dt>
        <dd>#{lifecycle.execution.ordinal} · {lifecycle.execution.status} · started {formatDate(lifecycle.execution.started_at)}</dd>
        <dt>Jira sync health</dt>
        <dd>
          {#if lifecycle.jira_write_intents.length === 0}
            <span class="unset">No Jira writes recorded</span>
          {:else}
            {lifecycle.jira_write_intents.filter((intent) => intent.status === "succeeded").length} succeeded ·
            {lifecycle.jira_write_intents.filter((intent) => intent.status === "pending").length} pending ·
            {lifecycle.jira_write_intents.filter((intent) => intent.status === "retrying").length} retrying ·
            {lifecycle.jira_write_intents.filter((intent) => intent.status === "failed").length} failed
          {/if}
        </dd>
      </dl>

      <h3>Sessions</h3>
      {#if lifecycle.sessions.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Participant</th><th scope="col">Status</th><th scope="col">Started</th><th scope="col">Ended</th><th scope="col">Session</th></tr></thead>
            <tbody>
              {#each lifecycle.sessions as session (session.id)}
                <tr>
                  <td>{session.participant}</td>
                  <td>{session.status}</td>
                  <td>{formatDate(session.started_at)}</td>
                  <td>{session.ended_at ? formatDate(session.ended_at) : "In progress"}</td>
                  <td><code class="id">{session.id}</code>{#if session.resumed_from_id}<span class="meta"> resumed from <code class="id">{session.resumed_from_id}</code></span>{/if}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No lifecycle sessions recorded.</p>
      {/if}

      <h3>Checkpoints</h3>
      {#if lifecycle.checkpoints.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Sequence</th><th scope="col">Summary</th><th scope="col">Progress</th><th scope="col">Handoff</th><th scope="col">Recorded</th></tr></thead>
            <tbody>
              {#each lifecycle.checkpoints as checkpoint (checkpoint.id)}
                <tr>
                  <td>{checkpoint.sequence}</td>
                  <td>{checkpoint.summary}</td>
                  <td>{checkpoint.progress_percent === undefined ? "Not recorded" : `${checkpoint.progress_percent}%`}</td>
                  <td>{checkpoint.handoff_to || "—"}</td>
                  <td>{formatDate(checkpoint.created_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No session checkpoints recorded.</p>
      {/if}

      <h3>Usage, cost &amp; budgets</h3>
      <dl class="references lifecycle-totals">
        <dt>Known cost</dt>
        <dd>{Object.keys(lifecycle.known_cost_by_currency).length > 0 ? Object.entries(lifecycle.known_cost_by_currency).map(([currency, amount]) => formatAmount(amount, currency)).join(" · ") : "No priced usage recorded"}</dd>
        <dt>Budget total</dt>
        <dd>{Object.keys(budgetTotals).length > 0 ? Object.entries(budgetTotals).map(([currency, amount]) => formatAmount(amount, currency)).join(" · ") : "No budgets recorded"}</dd>
        <dt>Unpriced usage</dt>
        <dd>{lifecycle.unknown_priced_usage ? "Yes — one or more usage entries have no known price." : "No"}</dd>
      </dl>
      {#if lifecycle.usage.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Category</th><th scope="col">Usage</th><th scope="col">Model</th><th scope="col">Unit price</th><th scope="col">Cost</th><th scope="col">Price source</th><th scope="col">Recorded</th></tr></thead>
            <tbody>
              {#each lifecycle.usage as usage (usage.id)}
                <tr>
                  <td>{usage.kind} · {usage.category}</td>
                  <td>{usage.quantity} {usage.unit}</td>
                  <td>{usage.model || "—"}</td>
                  <td>{usage.unit_price === undefined || !usage.cost_currency ? "Not recorded" : formatAmount(usage.unit_price, usage.cost_currency)}</td>
                  <td>{usage.cost_amount === undefined || !usage.cost_currency ? "Unpriced" : formatAmount(usage.cost_amount, usage.cost_currency)}</td>
                  <td>{usage.price_source || "—"}</td>
                  <td>{formatDate(usage.recorded_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No usage recorded.</p>
      {/if}
      {#if lifecycle.budgets.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <caption>Budgets</caption>
            <thead><tr><th scope="col">Category</th><th scope="col">Amount</th><th scope="col">Session</th><th scope="col">Recorded</th></tr></thead>
            <tbody>
              {#each lifecycle.budgets as budget (budget.id)}
                <tr>
                  <td>{budget.category || "Overall"}</td>
                  <td>{formatAmount(budget.amount, budget.currency)}</td>
                  <td>{#if budget.session_id}<code class="id">{budget.session_id}</code>{:else}Overall{/if}</td>
                  <td>{formatDate(budget.created_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No budgets recorded.</p>
      {/if}

      <h3>Jira source &amp; assessments</h3>
      {#if lifecycle.jira_snapshots.length > 0}
        <ul class="lifecycle-cards">
          {#each lifecycle.jira_snapshots as snapshot (snapshot.id)}
            <li>
              <strong>{snapshot.jira_issue_key} · version {snapshot.version}</strong>
              <span class="meta">{formatDate(snapshot.captured_at)} · hash <code class="id">{snapshot.content_hash}</code></span>
              <p>{snapshot.title}</p>
              <details><summary>Snapshot content</summary><p class="snapshot-body">{snapshot.body}</p></details>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="empty">No Jira source snapshots recorded.</p>
      {/if}
      {#if lifecycle.jira_assessments.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <caption>Assessments</caption>
            <thead><tr><th scope="col">Clarity</th><th scope="col">Approval</th><th scope="col">Rationale</th><th scope="col">Snapshot</th><th scope="col">Assessed</th></tr></thead>
            <tbody>
              {#each lifecycle.jira_assessments as assessment (assessment.id)}
                <tr>
                  <td>{assessment.clarity}</td>
                  <td>{assessment.approval}</td>
                  <td>{assessment.rationale}</td>
                  <td>{#if assessment.snapshot_id}<code class="id">{assessment.snapshot_id}</code>{:else}Not linked{/if}</td>
                  <td>{formatDate(assessment.assessed_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No Jira assessments recorded.</p>
      {/if}

      <h3>Jira work-item mappings</h3>
      {#if lifecycle.jira_work_items.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Jira issue</th><th scope="col">Parent task</th><th scope="col">Requirement source</th><th scope="col">Session</th><th scope="col">Mapped</th></tr></thead>
            <tbody>
              {#each lifecycle.jira_work_items as workItem (workItem.id)}
                <tr>
                  <td><code class="id">{workItem.jira_issue_key}</code></td>
                  <td><code class="id">{workItem.parent_task_id}</code></td>
                  <td><code class="id">{workItem.requirement_source_id}</code></td>
                  <td>{#if workItem.session_id}<code class="id">{workItem.session_id}</code>{:else}Not linked{/if}</td>
                  <td>{formatDate(workItem.created_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No Jira work-item mappings recorded.</p>
      {/if}

      <h3>Progress reports</h3>
      {#if lifecycle.progress_reports.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Summary</th><th scope="col">Progress</th><th scope="col">Session</th><th scope="col">Reported</th></tr></thead>
            <tbody>
              {#each lifecycle.progress_reports as report (report.id)}
                <tr><td>{report.summary}</td><td>{report.progress_percent === undefined ? "Not recorded" : `${report.progress_percent}%`}</td><td><code class="id">{report.session_id}</code></td><td>{formatDate(report.reported_at)}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No progress reports recorded.</p>
      {/if}

      <h3>Jira write intents</h3>
      {#if lifecycle.jira_write_intents.length > 0}
        <div class="lifecycle-table-wrap">
          <table class="lifecycle-table">
            <thead><tr><th scope="col">Jira issue</th><th scope="col">Action</th><th scope="col">Status</th><th scope="col">Attempts</th><th scope="col">Retry</th><th scope="col">External ID</th><th scope="col">Updated</th></tr></thead>
            <tbody>
              {#each lifecycle.jira_write_intents as intent (intent.id)}
                <tr>
                  <td><code class="id">{intent.jira_issue_key}</code></td>
                  <td>{intent.action}</td>
                  <td>{intent.status}{#if intent.last_error}<span class="intent-error"> — {intent.last_error}</span>{/if}</td>
                  <td>{intent.attempt_count}</td>
                  <td>{intent.retry_at ? formatDate(intent.retry_at) : "—"}</td>
                  <td>{#if intent.external_id}<code class="id">{intent.external_id}</code>{:else}—{/if}</td>
                  <td>{formatDate(intent.updated_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <p class="empty">No Jira write intents recorded.</p>
      {/if}
    </section>
  {/if}

  {#if v.pending_questions.length > 0}
    <section aria-labelledby="questions-heading">
      <h2 id="questions-heading">Pending questions</h2>
      <ul class="questions">
        {#each v.pending_questions as question (question)}
          <li>
            <Card>
              {#snippet children()}
                <DeliveryQuestionForm
                  {orchestrationId}
                  {question}
                  projectIds={v.projects.map((p) => p.project_id)}
                  revision={v.orchestration.revision}
                  onAnswered={applyUpdate}
                />
              {/snippet}
            </Card>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if v.pending_approvals.length > 0}
    <section aria-labelledby="approvals-heading">
      <h2 id="approvals-heading">Pending approvals</h2>
      <label class="approver">
        Approved by
        <input type="text" bind:value={approvedBy} placeholder="your name" />
      </label>
      <ul class="approvals">
        {#each v.pending_approvals as manifest (manifest.id)}
          <li>
            <DeliveryApprovalCard {orchestrationId} {manifest} {approvedBy} onResolved={applyUpdate} />
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if v.blockers.length > 0}
    <section aria-labelledby="blockers-heading">
      <h2 id="blockers-heading">Blockers</h2>
      <ul class="blockers">
        {#each v.blockers as blocker (blocker.lane_id)}
          <li>
            <strong>{blocker.lane_id}</strong>
            {#if blocker.parent_task_id}<span class="meta">task {blocker.parent_task_id}</span>{/if}
            blocked by {blocker.blocked_by.join(", ")}
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  <section aria-labelledby="projects-heading">
    <h2 id="projects-heading">Projects &amp; lanes</h2>
    <div class="projects">
      {#each v.projects as project (project.project_id)}
        <Card>
          {#snippet header()}
            <div class="project-head">
              <span class="project-name">
                <strong>{project.project_id}</strong>
                <!-- Attached means the delivery states it involves this
                     project; anything else is here only because a lane names
                     it, which includes a project detached once its lanes
                     finished. Saying which is which is the difference between
                     "this is in scope" and "work happened here". -->
                {#if project.attached}
                  <span class="attach-badge attached" data-testid={`attached-${project.project_id}`}>Attached</span>
                {:else}
                  <span class="attach-badge detached" data-testid={`unattached-${project.project_id}`}>
                    Not attached · has lanes here
                  </span>
                {/if}
              </span>
              <span class="counts">
                {#each Object.entries(project.counts_by_status) as [status, count] (status)}
                  <StatusBadge
                    variant={laneStatusVariants[status as DeliveryLaneStatus] ?? "neutral"}
                    label={`${count} ${status}`}
                  />
                {/each}
              </span>
            </div>
          {/snippet}
          {#snippet children()}
            <ul class="lanes">
              {#each lanesFor(project.project_id) as lane (lane.lane_id)}
                {@const stage = stageLabel(lane)}
                <li class="lane">
                  <div class="lane-head">
                    <span class="lane-id">{lane.lane_id}</span>
                    <StatusBadge variant={laneStatusVariants[lane.status] ?? "neutral"} label={lane.status} />
                  </div>
                  {#if lane.parent_task_id}<span class="meta">task {lane.parent_task_id}</span>{/if}
                  {#if lane.blocked_by?.length}
                    <span class="blocked-by">blocked by {lane.blocked_by.join(", ")}</span>
                  {/if}
                  {#if lane.pr_url}
                    <a class="pr-link" href={lane.pr_url} target="_blank" rel="noopener noreferrer">
                      PR{#if lane.pr_number}
                        #{lane.pr_number}
                      {/if}{#if lane.pr_provider}
                        ({lane.pr_provider})
                      {/if}
                    </a>
                  {/if}
                  {#if lane.repository || lane.branch || lane.session_id || lane.worker || lane.worktree_path || lane.base_sha}
                    <div class="lane-detail meta">
                      <!-- The session that opened the lane and the worker
                           currently holding its lease are two different
                           things, so they are labelled separately and never
                           collapsed into one "who" field. -->
                      {#if lane.repository}<span>repository {lane.repository}</span>{/if}
                      {#if lane.branch}<span>branch {lane.branch}</span>{/if}
                      {#if lane.session_id}<span>session {lane.session_id}</span>{/if}
                      {#if lane.worker}<span>worker {lane.worker}</span>{/if}
                      {#if lane.worktree_path}<span>worktree {lane.worktree_path}</span>{/if}
                      {#if lane.base_sha}<span>base {shortSha(lane.base_sha)}{lane.base_remote ? ` (${lane.base_remote})` : ""}</span>{/if}
                    </div>
                  {/if}
                  {#if lane.commits?.length}
                    <div class="lane-detail meta">
                      <strong>Commits</strong>
                      {#each lane.commits as commit (commit)}<code>{shortSha(commit)}</code>{/each}
                    </div>
                  {/if}
                  {#if stage}
                    <span class="meta">stage: {stage}</span>
                  {/if}
                  {#if lane.repair_cycle_count}
                    <span class="meta">
                      attempt {lane.attempt ?? 1} · {lane.repair_cycle_count} repair cycle{lane.repair_cycle_count === 1 ? "" : "s"}
                    </span>
                  {/if}
                  {#if lane.escalated_at}
                    <span class="escalated">escalated at {lane.escalated_at}</span>
                  {/if}
                  {#if lane.evidence?.length}
                    <ul class="evidence">
                      {#each lane.evidence as ev (ev.id)}
                        <li>
                          <a
                            href={deliveryEvidenceUrl(orchestrationId, ev.id)}
                            target="_blank"
                            rel="noopener noreferrer"
                          >
                            {ev.kind} evidence ({ev.byte_size}B)
                          </a>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                  {#if lane.verification}
                    <div class="audit-block">
                      <strong>Verification</strong>
                      <div class="verification">
                        {#each lane.verification.dimensions as dimension (dimension.name)}
                          <StatusBadge
                            variant={dimension.status === "passed" ? "success" : dimension.status === "failed" ? "danger" : "neutral"}
                            label={`${dimension.name}: ${dimension.status}`}
                          />
                        {/each}
                      </div>
                    </div>
                  {/if}
                  {#if lane.bagong_review}
                    <div class="audit-block">
                      <strong>Bagong review</strong>
                      <span>{lane.bagong_review.outcome} · {lane.bagong_review.independence_level}</span>
                      <span class="meta">reviewer {lane.bagong_review.reviewer_worker_id} · {formatDate(lane.bagong_review.recorded_at)}</span>
                    </div>
                  {/if}
                </li>
              {/each}
              {#if lanesFor(project.project_id).length === 0}
                <li class="empty">No lanes yet.</li>
              {/if}
            </ul>
          {/snippet}
        </Card>
      {/each}
    </div>
  </section>

  <section aria-labelledby="jira-heading">
    <h2 id="jira-heading">Jira activity</h2>
    {#if v.jira_activity?.length}
      <ul class="audit-list">
        {#each v.jira_activity as activity (`${activity.event_type}-${activity.entity_id ?? ""}-${activity.fired_at}`)}
          <li>
            <strong>{activity.issue_key}</strong>
            <span>{activity.event_type}{activity.entity_id ? ` · ${activity.entity_id}` : ""}</span>
            <time>{formatDate(activity.fired_at)}</time>
          </li>
        {/each}
      </ul>
    {:else}
      <p class="empty">No Jira updates recorded.</p>
    {/if}
  </section>

  <section aria-labelledby="timeline-heading">
    <h2 id="timeline-heading">Timeline</h2>
    <ol class="audit-list timeline">
      {#each v.timeline ?? [] as event (event.sequence)}
        <li>
          <code>#{event.sequence}</code>
          <strong>{event.type}</strong>
          {#if event.entity_id}<span>{event.entity_id}</span>{/if}
          <time>{formatDate(event.occurred_at)}</time>
        </li>
      {/each}
    </ol>
  </section>
{/if}

<style>
  .error {
    color: var(--color-danger);
  }
  .status-row {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-bottom: 1rem;
    flex-wrap: wrap;
  }
  .meta {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    min-width: 0;
  }
  .meta .id,
  .references .id {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    overflow-wrap: anywhere;
  }
  .description {
    margin: 0 0 1rem;
    max-width: 68ch;
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--color-text);
    white-space: pre-wrap;
  }
  .references {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 0.25rem 0.7rem;
    margin: 0 0 1rem;
    font-size: 0.8rem;
  }
  .references dt {
    color: var(--color-text-muted);
  }
  .references dd {
    margin: 0;
    min-width: 0;
  }
  .unset {
    color: var(--color-text-muted);
    font-style: italic;
  }
  .project-name {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
    min-width: 0;
  }
  .attach-badge {
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    border-radius: 999px;
    padding: 0.1rem 0.5rem;
  }
  .attach-badge.attached {
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, var(--color-surface));
  }
  .attach-badge.detached {
    color: var(--color-text-muted);
    background: var(--color-surface-subtle);
    text-transform: none;
    letter-spacing: 0;
    font-weight: 600;
  }
  section {
    margin-top: 1.6rem;
  }
  h2 {
    font-size: 1rem;
    margin: 0 0 0.6rem;
  }
  h3 {
    font-size: 0.9rem;
    margin: 1.1rem 0 0.45rem;
  }
  .lifecycle-totals {
    margin-bottom: 0.65rem;
  }
  .lifecycle-table-wrap {
    overflow-x: auto;
    border: 1px solid var(--color-border);
    border-radius: 8px;
  }
  .lifecycle-table {
    width: 100%;
    min-width: 640px;
    border-collapse: collapse;
    font-size: 0.8rem;
  }
  .lifecycle-table th,
  .lifecycle-table td {
    padding: 0.5rem 0.65rem;
    vertical-align: top;
    text-align: left;
    border-bottom: 1px solid var(--color-border);
  }
  .lifecycle-table th {
    color: var(--color-text-muted);
    font-weight: 600;
    background: var(--color-surface-subtle);
  }
  .lifecycle-table tr:last-child td {
    border-bottom: 0;
  }
  .lifecycle-table caption {
    padding: 0.5rem 0.65rem;
    text-align: left;
    font-weight: 600;
    background: var(--color-surface-subtle);
  }
  .lifecycle-table .id {
    overflow-wrap: anywhere;
  }
  .lifecycle-cards {
    list-style: none;
    display: grid;
    gap: 0.5rem;
    padding: 0;
    margin: 0;
  }
  .lifecycle-cards li {
    display: grid;
    gap: 0.35rem;
    padding: 0.6rem 0.7rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    font-size: 0.8rem;
  }
  .lifecycle-cards p {
    margin: 0;
  }
  .snapshot-body {
    white-space: pre-wrap;
    line-height: 1.45;
    color: var(--color-text-muted);
  }
  .intent-error {
    color: var(--color-danger);
  }
  .questions,
  .approvals,
  .blockers {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.7rem;
  }
  .blockers li {
    background: var(--color-surface-subtle);
    border-radius: 8px;
    padding: 0.5rem 0.7rem;
    font-size: 0.85rem;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .approver {
    display: inline-grid;
    gap: 0.2rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
    margin-bottom: 0.8rem;
  }
  .approver input {
    font: inherit;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
  }
  .approvals {
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  }
  .projects {
    display: grid;
    grid-template-columns: 1fr;
    gap: 1rem;
  }
  @media (min-width: 900px) {
    .projects {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .project-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    width: 100%;
    flex-wrap: wrap;
  }
  .counts {
    display: flex;
    gap: 0.3rem;
    flex-wrap: wrap;
  }
  .lanes {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.5rem;
  }
  .lane {
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 0.5rem 0.65rem;
    display: grid;
    gap: 0.2rem;
    font-size: 0.82rem;
  }
  .lane-head {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }
  .lane-id {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-weight: 600;
  }
  .blocked-by {
    color: var(--color-warning);
  }
  .pr-link {
    color: var(--color-accent);
    font-weight: 600;
    text-decoration: none;
  }
  .pr-link:hover {
    text-decoration: underline;
  }
  .lane-detail {
    display: flex;
    flex-wrap: wrap;
    gap: 0.6rem;
  }
  .audit-block {
    display: grid;
    gap: 0.35rem;
    margin-top: 0.35rem;
  }
  .verification {
    display: flex;
    gap: 0.35rem;
    flex-wrap: wrap;
  }
  .audit-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.45rem;
  }
  .audit-list li {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
    padding: 0.55rem 0.7rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
  }
  .audit-list time {
    margin-left: auto;
    color: var(--color-text-muted);
    font-size: 0.78rem;
  }
  .escalated {
    color: var(--color-danger);
    font-weight: 600;
    font-size: 0.78rem;
  }
  .evidence {
    list-style: none;
    padding: 0;
    margin: 0.1rem 0 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .evidence a {
    color: var(--color-accent);
    font-size: 0.78rem;
    text-decoration: none;
  }
  .evidence a:hover {
    text-decoration: underline;
  }
  .empty {
    color: var(--color-text-muted);
    font-size: 0.8rem;
  }
</style>
