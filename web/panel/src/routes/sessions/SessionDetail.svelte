<script lang="ts">
  import { onMount } from "svelte";
  import {
    getSession,
    listCapsules,
    listEvidence,
    type SessionDetail,
    type TimelineEvent,
    type ContextCapsule,
    type EvidenceRecord,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import EvidenceItem from "../../lib/components/EvidenceItem.svelte";

  interface Props {
    workspaceId: string;
    sessionId: string;
  }
  let { workspaceId, sessionId }: Props = $props();

  let detail: SessionDetail | null = $state(null);
  let capsules: ContextCapsule[] = $state([]);
  let evidence: EvidenceRecord[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  const ROLES = ["semar", "gareng", "petruk", "bagong"] as const;

  async function load(wsId: string, sesId: string) {
    loading = true;
    error = null;
    try {
      const [d, ev] = await Promise.all([getSession(wsId, sesId), listEvidence(wsId, sesId)]);
      detail = d;
      evidence = ev.items;

      // Context capsules have no run/session field (a documented gap, see
      // internal/panel/api/capsule_handler.go) - the closest honest link
      // is the set of task ids this session's own timeline touched.
      const taskIds = [...new Set((d.Timeline ?? []).map((e) => e.task).filter((t): t is string => !!t))];
      const perTask = await Promise.all(taskIds.map((t) => listCapsules(wsId, t)));
      capsules = perTask.flatMap((r) => r.items);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load(workspaceId, sessionId);
    return onPanelEvent(() => load(workspaceId, sessionId));
  });
  $effect(() => {
    load(workspaceId, sessionId);
  });

  function timelineFor(role: string, events: TimelineEvent[]): TimelineEvent[] {
    return events.filter((e) => e.role === role);
  }

  function isFailure(e: TimelineEvent): boolean {
    return e.result === "failure" || e.result === "timeout" || e.result === "cancelled";
  }
</script>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load this session: {error}</p>
{:else if detail}
  <header>
    <h1>{detail.id}</h1>
    <span class="status status-{detail.status}">{detail.status}</span>
  </header>
  <p class="meta">
    {detail.workflow}
    {#if detail.initiator}· initiated by {detail.initiator}{/if}
    {#if detail.active_role}· active role: {detail.active_role}{/if}
  </p>
  {#if detail.objective}<p class="objective">{detail.objective}</p>{/if}

  <section aria-labelledby="progress-heading">
    <h2 id="progress-heading">Progress</h2>
    <div class="cards">
      <div class="card"><strong>{detail.task_counts?.total ?? 0}</strong><span>Tasks</span></div>
      <div class="card"><strong>{detail.task_counts?.open ?? 0}</strong><span>Open</span></div>
      <div class="card"><strong>{detail.task_counts?.in_progress ?? 0}</strong><span>In progress</span></div>
      <div class="card"><strong>{detail.task_counts?.blocked ?? 0}</strong><span>Blocked</span></div>
      <div class="card"><strong>{detail.task_counts?.closed ?? 0}</strong><span>Closed</span></div>
      <div class="card"><strong>{detail.evidence_count ?? 0}</strong><span>Evidence</span></div>
    </div>
  </section>

  <section aria-labelledby="evidence-heading">
    <h2 id="evidence-heading">Evidence</h2>
    {#if evidence.length === 0}
      <p>No evidence recorded for this session.</p>
    {:else}
      <ul class="evidence">
        {#each evidence as rec (rec.id)}
          <EvidenceItem {workspaceId} record={rec} />
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="timeline-heading">
    <h2 id="timeline-heading">Phase Timeline</h2>
    {#if !detail.Timeline || detail.Timeline.length === 0}
      <p>No events recorded yet.</p>
    {:else}
      <ol class="timeline">
        {#each detail.Timeline as e (e.id)}
          <li class:failure={isFailure(e)}>
            <span class="time">{new Date(e.timestamp).toLocaleTimeString()}</span>
            <span class="op">{e.operation}</span>
            {#if e.role}<span class="role">{e.role}</span>{/if}
            <span class="result result-{e.result}">{e.result}</span>
          </li>
        {/each}
      </ol>
    {/if}
  </section>

  <section aria-labelledby="role-lane-heading">
    <h2 id="role-lane-heading">Role Lane</h2>
    <div class="lanes">
      {#each ROLES as role (role)}
        {@const events = timelineFor(role, detail.Timeline ?? [])}
        <div class="lane">
          <h3>{role}</h3>
          {#if events.length === 0}
            <p class="empty">No activity.</p>
          {:else}
            <ol>
              {#each events as e (e.id)}
                <li class:failure={isFailure(e)}>{e.operation}</li>
              {/each}
            </ol>
          {/if}
        </div>
      {/each}
    </div>
  </section>

  <section aria-labelledby="capsules-heading">
    <h2 id="capsules-heading">Context Capsules</h2>
    {#if capsules.length === 0}
      <p>No context capsules for this session's tasks.</p>
    {:else}
      <ul class="capsules">
        {#each capsules as c (c.id)}
          <li>
            <div class="capsule-head">
              <strong>{c.role}</strong>
              <code class="digest">{c.digest.slice(0, 15)}…</code>
            </div>
            <p>{c.objective}</p>
            <p class="refs">
              {c.relevant_knowledge?.length ?? 0} knowledge refs · {c.evidence?.length ?? 0} evidence refs ·
              {c.allowed_tools.length} allowed tools
            </p>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="errors-heading">
    <h2 id="errors-heading">Errors and Recovery</h2>
    {#if !detail.Timeline || detail.Timeline.filter(isFailure).length === 0}
      <p>No failures recorded.</p>
    {:else}
      <ul class="errors">
        {#each detail.Timeline.filter(isFailure) as e (e.id)}
          <li>
            <span class="time">{new Date(e.timestamp).toLocaleTimeString()}</span>
            <span class="op">{e.operation}</span>
            <span class="result result-{e.result}">{e.result}</span>
          </li>
        {/each}
      </ul>
      <p class="hint">{detail.error_count ?? 0} total error(s) recorded for this session.</p>
    {/if}
  </section>
{/if}

<style>
  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  h1 {
    font-size: 1.2rem;
    margin: 0;
    word-break: break-all;
  }
  .meta {
    color: #666;
    font-size: 0.85rem;
    margin: 0.15rem 0;
  }
  .objective {
    font-size: 0.95rem;
  }
  .error {
    color: #b00020;
  }
  h2 {
    font-size: 1rem;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
    gap: 0.6rem;
    margin: 0.5rem 0 1.25rem;
  }
  .card {
    border: 1px solid #ddd;
    border-radius: 6px;
    padding: 0.6rem 0.8rem;
    display: grid;
    gap: 0.1rem;
  }
  .card strong {
    font-size: 1.3rem;
  }
  .card span {
    color: #666;
    font-size: 0.8rem;
  }
  .status {
    font-size: 0.8rem;
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  ol.timeline {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.3rem;
  }
  ol.timeline li {
    display: flex;
    gap: 0.6rem;
    align-items: center;
    border: 1px solid #eee;
    border-radius: 6px;
    padding: 0.4rem 0.6rem;
    font-size: 0.85rem;
  }
  ol.timeline li.failure,
  ul.errors li {
    border-color: #f3c2c2;
    background: #fff8f8;
  }
  .time {
    color: #666;
    font-size: 0.8rem;
    min-width: 5.5rem;
  }
  .op {
    flex: 1;
  }
  .role {
    color: #3949ab;
    font-size: 0.8rem;
  }
  .result {
    font-size: 0.75rem;
    padding: 0.05rem 0.4rem;
    border-radius: 4px;
    background: #eee;
  }
  .result-failure,
  .result-timeout,
  .result-cancelled {
    background: #fdecea;
    color: #c62828;
  }
  .result-success {
    background: #e6f4ea;
    color: #1e7d32;
  }
  .lanes {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 0.75rem;
  }
  .lane {
    border: 1px solid #eee;
    border-radius: 6px;
    padding: 0.5rem 0.6rem;
    min-width: 0;
  }
  .lane h3 {
    font-size: 0.85rem;
    margin: 0 0 0.3rem;
    text-transform: capitalize;
  }
  .lane ol {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.2rem;
    font-size: 0.8rem;
  }
  .lane li.failure {
    color: #c62828;
  }
  .empty {
    color: #999;
    font-size: 0.8rem;
    margin: 0;
  }
  ul.capsules {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.5rem;
  }
  ul.capsules li {
    border: 1px solid #eee;
    border-radius: 6px;
    padding: 0.5rem 0.75rem;
  }
  .capsule-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: capitalize;
  }
  .digest {
    color: #666;
    font-size: 0.75rem;
  }
  .refs {
    color: #666;
    font-size: 0.8rem;
    margin: 0.2rem 0 0;
  }
  ul.errors {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.3rem;
  }
  ul.errors li {
    display: flex;
    gap: 0.6rem;
    border-radius: 6px;
    padding: 0.4rem 0.6rem;
    font-size: 0.85rem;
  }
  .hint {
    color: #666;
    font-size: 0.8rem;
  }
  ul.evidence {
    list-style: none;
    padding: 0;
    display: grid;
    gap: 0.4rem;
  }

  @media (max-width: 720px) {
    .lanes {
      grid-template-columns: 1fr;
    }
  }
</style>
