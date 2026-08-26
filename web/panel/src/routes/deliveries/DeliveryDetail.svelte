<script lang="ts">
  import { getDeliveryView, cancelDelivery, type DeliveryView } from "../../lib/api/client";
  import { navigate } from "../../lib/router/router.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import type { IconName } from "../../lib/components/Icon.svelte";
  import Tabs from "../../lib/components/Tabs.svelte";
  import DeliveryCancelDialog from "./DeliveryCancelDialog.svelte";

  interface Props { orchestrationId: string; }
  let { orchestrationId }: Props = $props();
  let view: DeliveryView | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);
  let cancelling = $state(false);
  let cancelError: string | null = $state(null);
  let confirmingCancel = $state(false);
  const tabs = [
    { id: "summary", label: "Summary", icon: "dashboard" as IconName },
    { id: "projects", label: "Projects", icon: "folder" as IconName },
    { id: "plans", label: "Plans", icon: "file" as IconName },
    { id: "sessions", label: "Sessions", icon: "users" as IconName },
    { id: "activities", label: "Activities", icon: "activity" as IconName },
  ];
  const tabIds = new Set(tabs.map((tab) => tab.id));

  function tabFromUrl(): string {
    if (typeof window === "undefined") return "summary";
    const tab = new URL(window.location.href).searchParams.get("tab") ?? "summary";
    return tabIds.has(tab) ? tab : "summary";
  }
  let activeId = $state(tabFromUrl());

  function selectTab(id: string) {
    activeId = id;
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (id === "summary") url.searchParams.delete("tab");
    else url.searchParams.set("tab", id);
    window.history.replaceState({}, "", `${url.pathname}${url.search}${url.hash}`);
  }

  async function load(id: string) {
    loading = true;
    error = null;
    try { view = await getDeliveryView(id); }
    catch (e) { error = e instanceof Error ? e.message : String(e); }
    finally { loading = false; }
  }
  $effect(() => { load(orchestrationId); });

  async function cancel() {
    if (!view) return;
    cancelling = true;
    cancelError = null;
    try { view = await cancelDelivery(orchestrationId, { expected_revision: view.orchestration.revision }); confirmingCancel = false; }
    catch (e) { cancelError = e instanceof Error ? e.message : String(e); }
    finally { cancelling = false; }
  }

  function formatDate(value: string): string { return new Date(value).toLocaleString(); }
  function formatDuration(seconds: number): string {
    const minutes = Math.max(0, Math.floor(seconds / 60));
    const hours = Math.floor(minutes / 60);
    return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
  }
  function sessionDuration(startedAt: string, endedAt?: string): string {
    const started = Date.parse(startedAt);
    const ended = endedAt ? Date.parse(endedAt) : Date.now();
    return Number.isFinite(started) && Number.isFinite(ended) ? formatDuration((ended - started) / 1000) : "Not recorded";
  }
  function formatAmount(amount: number, currency: string): string {
    return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amount);
  }
  function estimatedCost(v: DeliveryView): string {
    const totals = new Map<string, number>();
    for (const usage of v.lifecycle?.usage ?? []) {
      if (usage.kind !== "estimate" || usage.cost_amount === undefined || !usage.cost_currency) continue;
      totals.set(usage.cost_currency, (totals.get(usage.cost_currency) ?? 0) + usage.cost_amount);
    }
    const values = [...totals].map(([currency, amount]) => formatAmount(amount, currency));
    return values.length ? values.join(" · ") : "No estimate";
  }
  function plansFor(v: DeliveryView, projectId: string): number { return (v.project_plans ?? []).filter((plan) => plan.project_id === projectId).length; }
  function projectSlug(v: DeliveryView, projectId: string): string {
    return v.projects.find((project) => project.project_id === projectId)?.project_slug || projectId;
  }
  const statusVariants: Record<string, BadgeVariant> = { pending: "neutral", active: "info", completed: "success", cancelled: "danger" };
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load delivery" message={error} />
{:else if view}
  {@const v = view}
  <PageHeader title={v.title} description={v.description || "No description recorded."}>
    {#snippet actions()}
      {#if v.orchestration.status === "pending" || v.orchestration.status === "active"}
        <Button variant="danger" size="sm" disabled={cancelling} onclick={() => (confirmingCancel = true)}>Cancel delivery</Button>
      {/if}
    {/snippet}
  </PageHeader>
  <DeliveryCancelDialog open={confirmingCancel} label={v.title} {orchestrationId} busy={cancelling} error={cancelError} onclose={() => !cancelling && (confirmingCancel = false)} onconfirm={cancel} />
  {#if cancelError && !confirmingCancel}<p role="alert" class="error">{cancelError}</p>{/if}

  <div class="status-row">
    <StatusBadge variant={statusVariants[v.orchestration.status] ?? "neutral"} label={v.orchestration.status} />
    <span>Created {formatDate(v.orchestration.created_at)}</span>
    <details><summary>Technical reference</summary><code>{orchestrationId}</code></details>
  </div>
  <Tabs {tabs} {activeId} onchange={selectTab} ariaLabel="Delivery sections" />

  {#if activeId === "summary"}
    <div id="tabpanel-summary" role="tabpanel" aria-labelledby="tab-summary" class="summary">
      <div class="high-level-plan">
        <h2>High-level plan</h2>
        {#if v.plan_id}<p><code>{v.plan_id} r{v.plan_revision ?? 1}</code></p>
        {:else if v.plan_record_id}<p><code>{v.plan_record_id}</code></p>
        {:else}<p class="empty">No high-level plan recorded.</p>{/if}
      </div>
      <BentoGrid>
        <MetricCard size="small" columns={3} label="Projects" value={v.projects.length} />
        <MetricCard size="small" columns={3} label="Sessions" value={v.lifecycle?.sessions.length ?? 0} />
        <MetricCard size="small" columns={3} label="Project plans" value={v.project_plans?.length ?? 0} />
        <MetricCard size="small" columns={3} label="Estimated cost" value={estimatedCost(v)} />
      </BentoGrid>
    </div>
  {:else if activeId === "projects"}
    <div id="tabpanel-projects" role="tabpanel" aria-labelledby="tab-projects">
      <h2>Projects</h2>
      {#if v.projects.length}
        <div class="table-wrap"><table><thead><tr><th>Project</th><th>Project plans</th></tr></thead><tbody>
          {#each v.projects as project (project.project_id)}<tr><td><a href={`/projects/${encodeURIComponent(project.project_slug || project.project_id)}`} onclick={(event) => { event.preventDefault(); navigate(`/projects/${encodeURIComponent(project.project_slug || project.project_id)}`); }}>{project.project_slug || project.project_id}</a></td><td>{plansFor(v, project.project_id)}</td></tr>{/each}
        </tbody></table></div>
      {:else}<p class="empty">No projects are linked to this delivery.</p>{/if}
    </div>
  {:else if activeId === "plans"}
    <div id="tabpanel-plans" role="tabpanel" aria-labelledby="tab-plans">
      <h2>Project plans</h2>
      {#if (v.project_plans?.length ?? 0) > 0}
        <div class="table-wrap"><table><thead><tr><th>Project</th><th>Plan ID</th><th>Created</th></tr></thead><tbody>
          {#each v.project_plans ?? [] as plan (`${plan.project_id}-${plan.plan_id}-${plan.plan_revision}`)}<tr><td>{projectSlug(v, plan.project_id)}</td><td><a href={`/projects/${encodeURIComponent(projectSlug(v, plan.project_id))}?tab=plans&plan=${encodeURIComponent(plan.plan_id)}`} onclick={(event) => { event.preventDefault(); navigate(`/projects/${encodeURIComponent(projectSlug(v, plan.project_id))}?tab=plans&plan=${encodeURIComponent(plan.plan_id)}`); }}>{plan.plan_id} r{plan.plan_revision}</a></td><td>{formatDate(plan.created_at)}</td></tr>{/each}
        </tbody></table></div>
      {:else}<p class="empty">No project plans linked to this delivery.</p>{/if}
    </div>
  {:else if activeId === "sessions"}
    <div id="tabpanel-sessions" role="tabpanel" aria-labelledby="tab-sessions">
      <h2>Sessions</h2>
      {#if v.lifecycle?.sessions.length}
        <div class="table-wrap"><table><thead><tr><th>Agent</th><th>Path</th><th>Provider</th><th>Started</th><th>Duration</th></tr></thead><tbody>
          {#each v.lifecycle.sessions as session (session.id)}<tr><td>{session.participant}</td><td><code>{session.worktree_path || "Not recorded"}</code></td><td>{session.provider || "Not recorded"}</td><td>{formatDate(session.started_at)}</td><td>{sessionDuration(session.started_at, session.ended_at)}</td></tr>{/each}
        </tbody></table></div>
      {:else}<p class="empty">No sessions recorded.</p>{/if}
    </div>
  {:else}
    <div id="tabpanel-activities" role="tabpanel" aria-labelledby="tab-activities">
      <h2>Jira activity</h2>
      {#if v.jira_activity?.length}
        <div class="table-wrap"><table><caption>Jira activity</caption><thead><tr><th>Issue</th><th>Activity</th><th>Reference</th><th>Recorded</th></tr></thead><tbody>
          {#each v.jira_activity as activity (`${activity.event_type}-${activity.entity_id ?? ""}-${activity.fired_at}`)}<tr><td>{activity.issue_key}</td><td>{activity.event_type}</td><td>{activity.entity_id || "—"}</td><td>{formatDate(activity.fired_at)}</td></tr>{/each}
        </tbody></table></div>
      {:else}<p class="empty">No Jira activity recorded.</p>{/if}
    </div>
  {/if}
{/if}

<style>
  .error { color: var(--color-danger); }
  .status-row { display: flex; align-items: center; gap: .65rem; flex-wrap: wrap; margin-bottom: 1rem; color: var(--color-text-muted); font-size: .85rem; }
  .status-row details { margin-left: auto; } .status-row summary { cursor: pointer; } .status-row code { display: block; margin-top: .35rem; }
  h2 { font-size: 1rem; margin: 0 0 .75rem; } .summary { display: grid; gap: 1.25rem; }
  .high-level-plan { border-left: 3px solid var(--color-accent); padding-left: .85rem; } .high-level-plan p { margin: 0; }
  .table-wrap { overflow-x: auto; border: 1px solid var(--color-border); border-radius: var(--radius-sm); }
  table { width: 100%; border-collapse: collapse; font-size: .85rem; } th, td { padding: .65rem .75rem; text-align: left; border-bottom: 1px solid var(--color-border); vertical-align: top; } tr:last-child td { border-bottom: 0; } th { color: var(--color-text-muted); font-size: .72rem; letter-spacing: .04em; text-transform: uppercase; white-space: nowrap; } caption { caption-side: top; padding: .65rem .75rem; text-align: left; font-weight: 700; }
  a { color: var(--color-accent); font-weight: 600; text-decoration: none; } a:hover { text-decoration: underline; } code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; word-break: break-word; } .empty { color: var(--color-text-muted); }
</style>
