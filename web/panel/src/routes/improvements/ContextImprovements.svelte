<script lang="ts">
  import { onMount } from "svelte";
  import {
    listProjects,
    listContextImprovements,
    acceptContextImprovement,
    rejectContextImprovement,
    type ContextImprovement,
    type ContextImprovementArtifactType,
  } from "../../lib/api/client";
  import { onPanelEvent } from "../../lib/events/sse.svelte";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import Card from "../../lib/components/cards/Card.svelte";
  import Button from "../../lib/components/Button.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  // The id of the project whose inbox is shown. Resolved from listProjects()
  // the same way the rest of the panel treats "the current project": the one
  // flagged `primary` (the single project this panel instance serves), with a
  // fallback to the first registered project if none is flagged.
  let projectId: string | null = $state(null);
  let improvements: ContextImprovement[] = $state([]);
  let error: string | null = $state(null);
  let loading = $state(true);

  // Per-row in-flight state and the last accept/reject result banner.
  let pendingId: string | null = $state(null);
  let result: { kind: "success" | "error"; message: string } | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      if (!projectId) {
        const projects = (await listProjects()).items;
        projectId = (projects.find((p) => p.primary) ?? projects[0])?.id ?? null;
      }
      if (!projectId) {
        improvements = [];
        return;
      }
      improvements = (await listContextImprovements(projectId)).improvements;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    load();
    return onPanelEvent(load);
  });

  const artifactLabels: Record<ContextImprovementArtifactType, string> = {
    workflow: "Workflow",
    project_metadata: "Project metadata",
    knowledge: "Knowledge",
  };

  const artifactVariants: Record<ContextImprovementArtifactType, BadgeVariant> = {
    workflow: "info",
    project_metadata: "neutral",
    knowledge: "success",
  };

  const statusVariants: Record<string, BadgeVariant> = {
    proposal_ready: "warning",
    accepted: "success",
    rejected: "danger",
    conflicted: "danger",
  };

  // A proposal is only actionable while it is awaiting a decision.
  function isActionable(status: string): boolean {
    return status === "proposal_ready";
  }

  async function resolve(imp: ContextImprovement, action: "accept" | "reject") {
    if (!projectId) return;
    pendingId = imp.id;
    result = null;
    try {
      if (action === "accept") {
        await acceptContextImprovement(projectId, imp.review_id, imp.proposal_attempt);
      } else {
        await rejectContextImprovement(projectId, imp.review_id, imp.proposal_attempt);
      }
      result = {
        kind: "success",
        message: `Proposal for ${imp.target_id} ${action === "accept" ? "accepted" : "rejected"}.`,
      };
      await load();
    } catch (e) {
      result = { kind: "error", message: e instanceof Error ? e.message : String(e) };
    } finally {
      pendingId = null;
    }
  }
</script>

<PageHeader
  title="Context Improvements"
  description="Proposed changes to this project's workflows, metadata, and knowledge, distilled from prior runs. Accept the ones worth keeping."
/>

{#if result}
  <p role={result.kind === "error" ? "alert" : "status"} class="result {result.kind}">{result.message}</p>
{/if}

{#if loading}
  <p>Loading…</p>
{:else if error}
  <ErrorStateCard title="Failed to load context improvements" message={error} />
{:else if improvements.length === 0}
  <EmptyStateCard title="Inbox is clear" message="No pending context improvements." />
{:else}
  <ul>
    {#each improvements as imp (imp.id)}
      {@const actionable = isActionable(imp.status)}
      <li>
        <Card>
          {#snippet header()}
            <div class="row-head">
              <StatusBadge variant={artifactVariants[imp.artifact_type] ?? "neutral"} label={artifactLabels[imp.artifact_type] ?? imp.artifact_type} />
              <span class="target">{imp.target_id}</span>
            </div>
            <StatusBadge variant={statusVariants[imp.status] ?? "neutral"} label={imp.status} />
          {/snippet}
          {#snippet children()}
            <p class="rationale">{imp.rationale}</p>
            <div class="chips" aria-label="Supporting signal">
              <span class="chip" title="How many runs support this proposal">{imp.support_count} supporting {imp.support_count === 1 ? "run" : "runs"}</span>
              <span class="chip">{imp.evidence_ids.length} evidence</span>
              <span class="chip">{imp.source_run_ids.length} source {imp.source_run_ids.length === 1 ? "run" : "runs"}</span>
              {#if imp.proposal_attempt > 1}<span class="chip">attempt {imp.proposal_attempt}</span>{/if}
            </div>
            <p class="meta">
              created {new Date(imp.created_at).toLocaleString()} by {imp.created_by}
              · updated {new Date(imp.updated_at).toLocaleString()}
              · review {imp.review_id}
            </p>
          {/snippet}
          {#snippet footer()}
            <div class="actions">
              <Button
                variant="primary"
                size="sm"
                disabled={!actionable || pendingId === imp.id}
                onclick={() => resolve(imp, "accept")}
              >
                {pendingId === imp.id ? "Working…" : "Accept"}
              </Button>
              <Button
                variant="danger"
                size="sm"
                disabled={!actionable || pendingId === imp.id}
                onclick={() => resolve(imp, "reject")}
              >
                Reject
              </Button>
              {#if !actionable}
                <span class="resolved-note">Resolved — no action available.</span>
              {/if}
            </div>
          {/snippet}
        </Card>
      </li>
    {/each}
  </ul>
{/if}

<style>
  ul {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.6rem;
  }
  .row-head {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    min-width: 0;
  }
  .target {
    font-weight: 600;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.85rem;
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .rationale {
    margin: 0;
    font-size: 0.9rem;
    color: var(--color-text);
    overflow-wrap: anywhere;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }
  .chip {
    padding: 0.2rem 0.42rem;
    border-radius: 6px;
    background: var(--color-surface-subtle);
    color: var(--color-text-muted);
    font-size: 0.72rem;
  }
  .meta {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-text-muted);
    overflow-wrap: anywhere;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .resolved-note {
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }
  .result {
    margin: 0 0 1rem;
    font-size: 0.85rem;
    padding: 0.5rem 0.65rem;
    border-radius: 8px;
  }
  .result.success {
    background: color-mix(in srgb, var(--color-success) 14%, var(--color-surface));
    color: var(--color-success);
  }
  .result.error {
    background: color-mix(in srgb, var(--color-danger) 14%, var(--color-surface));
    color: var(--color-danger);
  }
</style>
