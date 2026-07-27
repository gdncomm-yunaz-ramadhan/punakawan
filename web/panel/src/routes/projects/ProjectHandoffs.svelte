<script lang="ts">
  import { onMount } from "svelte";
  import {
    listHandoffs,
    validateHandoff,
    supersedeHandoff,
    ApiError,
    type HandoffCapsule,
    type HandoffValidation,
    type HandoffValidationStatus,
  } from "../../lib/api/client";
  import { roleLabel } from "../../lib/roles";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let items: HandoffCapsule[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // Per-row transient action state, keyed by handoff id.
  let busyId: string | null = $state(null);
  let validations = $state<Record<string, HandoffValidation>>({});
  let actionErrors = $state<Record<string, string>>({});
  let copiedId: string | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listHandoffs(projectId);
      items = res.items ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function validationVariant(status: HandoffValidationStatus): BadgeVariant {
    switch (status) {
      case "resumable":
        return "success";
      case "refresh_required":
        return "warning";
      case "blocked":
      case "invalid":
        return "danger";
      case "superseded":
        return "info";
      default:
        return "neutral";
    }
  }

  function humanize(s: string): string {
    const spaced = s.replace(/_/g, " ");
    return spaced.charAt(0).toUpperCase() + spaced.slice(1);
  }

  async function doValidate(id: string) {
    busyId = id;
    actionErrors = { ...actionErrors, [id]: "" };
    try {
      const res = await validateHandoff(projectId, id);
      validations = { ...validations, [id]: res };
    } catch (e) {
      actionErrors = {
        ...actionErrors,
        [id]: e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e),
      };
    } finally {
      busyId = null;
    }
  }

  async function doSupersede(id: string) {
    busyId = id;
    actionErrors = { ...actionErrors, [id]: "" };
    try {
      await supersedeHandoff(projectId, id);
      // Reflect the new superseded state in place.
      items = items.map((h) => (h.id === id ? { ...h, superseded: true } : h));
    } catch (e) {
      actionErrors = {
        ...actionErrors,
        [id]: e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e),
      };
    } finally {
      busyId = null;
    }
  }

  async function copyId(id: string) {
    try {
      await navigator.clipboard?.writeText(id);
      copiedId = id;
      setTimeout(() => {
        if (copiedId === id) copiedId = null;
      }, 1500);
    } catch {
      /* clipboard unavailable — no-op */
    }
  }
</script>

<section aria-label="Project handoffs">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load handoffs" message={error} />
  {:else if items.length === 0}
    <EmptyStateCard
      title="No handoff capsules"
      message="Resumable handoff capsules created during this project's runs will appear here."
    />
  {:else}
    <ul class="handoffs">
      {#each items as h (h.id)}
        {@const v = validations[h.id]}
        <li class="card" data-testid={`handoff-${h.id}`} aria-label={`Handoff ${h.id}`}>
          <header class="card-head">
            <div class="obj">
              <p class="objective">{h.objective?.statement || h.id}</p>
              <p class="run">
                <code>{h.id}</code>
                {#if h.superseded}<span class="superseded">superseded</span>{/if}
              </p>
            </div>
            <div class="phase">
              <StatusBadge variant="neutral" label={humanize(h.current_phase || "unknown")} />
            </div>
          </header>

          <dl class="meta">
            {#if h.current_task}
              <dt>Current task</dt>
              <dd><code>{h.current_task.id}</code> — {h.current_task.next_action}</dd>
            {/if}
            {#if h.created_by}
              <dt>Source agent</dt>
              <dd>{roleLabel(h.created_by.role)} · {h.created_by.agent_client}</dd>
            {/if}
            {#if h.dossier}
              <dt>Dossier</dt>
              <dd><code>{h.dossier.id}</code> ({h.dossier.status})</dd>
            {/if}
            {#if h.created_at}
              <dt>Created</dt>
              <dd>{h.created_at}</dd>
            {/if}
          </dl>

          {#if v}
            <div class="validation" data-testid={`handoff-validation-${h.id}`}>
              <p class="validation-head">
                Validation: <StatusBadge variant={validationVariant(v.status)} label={humanize(v.status)} />
              </p>
              {#if v.changes_since_handoff?.length}
                <div class="validation-list">
                  <span class="vl-label">Changes since handoff</span>
                  <ul>
                    {#each v.changes_since_handoff as c (c)}
                      <li>{c}</li>
                    {/each}
                  </ul>
                </div>
              {/if}
              {#if v.required_refresh?.length}
                <div class="validation-list">
                  <span class="vl-label">Required refresh</span>
                  <ul>
                    {#each v.required_refresh as r (r)}
                      <li>{r}</li>
                    {/each}
                  </ul>
                </div>
              {/if}
            </div>
          {/if}

          {#if actionErrors[h.id]}
            <p class="error" role="alert" data-testid={`handoff-error-${h.id}`}>{actionErrors[h.id]}</p>
          {/if}

          <div class="card-actions">
            <!-- data-testid on a display:contents wrapper span (Button doesn't
                 forward data-testid); this testid is only asserted present. -->
            <span class="btn-wrap" data-testid={`copy-${h.id}`}>
              <Button variant="ghost" onclick={() => copyId(h.id)}>
                {copiedId === h.id ? "Copied" : "Copy capsule id"}
              </Button>
            </span>
            <Button variant="secondary" onclick={() => doSupersede(h.id)} disabled={busyId === h.id || h.superseded}>
              Supersede
            </Button>
            <Button variant="primary" onclick={() => doValidate(h.id)} disabled={busyId === h.id}>
              {busyId === h.id ? "Validating…" : "Validate"}
            </Button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .handoffs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 1rem;
  }
  .card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.05rem 1.15rem;
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
  }
  .card-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .obj {
    min-width: 0;
  }
  .objective {
    margin: 0;
    font-weight: 600;
    font-size: 0.95rem;
    overflow-wrap: anywhere;
  }
  .run {
    margin: 0.25rem 0 0;
    font-size: 0.8rem;
    color: var(--color-text-muted);
    overflow-wrap: anywhere;
  }
  .superseded {
    display: inline-block;
    margin-left: 0.4rem;
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    padding: 0.05rem 0.45rem;
  }
  dl.meta {
    margin: 0;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.3rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
  }
  /* On mobile the two-column definition grid crushes labels/values —
     stack to a single column below 640px. */
  @media (max-width: 639px) {
    dl.meta {
      grid-template-columns: 1fr;
      gap: 0.15rem 0;
    }
  }
  dl.meta dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  dl.meta dd {
    margin: 0;
    word-break: break-word;
    overflow-wrap: anywhere;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
    overflow-wrap: anywhere;
  }
  .validation {
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-sm);
    padding: 0.6rem 0.75rem;
    display: grid;
    gap: 0.5rem;
  }
  .validation-head {
    margin: 0;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.85rem;
  }
  .validation-list .vl-label {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .validation-list ul {
    margin: 0.25rem 0 0;
    padding-left: 1.1rem;
    display: grid;
    gap: 0.2rem;
    font-size: 0.82rem;
  }
  .validation-list li {
    overflow-wrap: anywhere;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0;
  }
  .card-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-top: auto;
  }
  /* testid wrapper is layout-transparent so the Button is the flex item. */
  .btn-wrap {
    display: contents;
  }
  @media (max-width: 639px) {
    .card-actions {
      flex-direction: column;
    }
    /* Stretch the shared Button (global .btn) full-width when stacked. */
    .card-actions :global(.btn) {
      width: 100%;
    }
  }
</style>
