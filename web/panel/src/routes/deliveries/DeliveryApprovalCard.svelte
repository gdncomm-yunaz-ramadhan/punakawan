<script lang="ts">
  import { approveProjectDelivery, type ApprovalManifest, type DeliveryView } from "../../lib/api/client";
  import Card from "../../lib/components/cards/Card.svelte";
  import StatusBadge, { type BadgeVariant } from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    orchestrationId: string;
    manifest: ApprovalManifest;
    approvedBy: string;
    onResolved: (view: DeliveryView) => void;
  }
  let { orchestrationId, manifest, approvedBy, onResolved }: Props = $props();

  let submitting: "approve" | "reject" | null = $state(null);
  let error: string | null = $state(null);

  const statusVariants: Record<string, BadgeVariant> = {
    pending: "warning",
    approved: "success",
    rejected: "danger",
  };

  async function resolve(reject: boolean) {
    submitting = reject ? "reject" : "approve";
    error = null;
    try {
      const view = await approveProjectDelivery(orchestrationId, {
        manifest_id: manifest.id,
        approved_by: approvedBy,
        reject,
      });
      onResolved(view);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      submitting = null;
    }
  }

  const canDecide = $derived(manifest.status === "pending" && approvedBy.trim().length > 0 && submitting === null);
</script>

<Card>
  {#snippet header()}
    <div class="head">
      <strong>{manifest.project_id}</strong>
      <StatusBadge variant={statusVariants[manifest.status] ?? "neutral"} label={manifest.status} />
    </div>
  {/snippet}
  {#snippet children()}
    <p class="branches">
      base <code>{manifest.planned_base_ref}</code>
      {#if manifest.planned_branches?.length}
        → <code>{manifest.planned_branches.join(", ")}</code>
      {/if}
    </p>
    {#if manifest.checks.length > 0}
      <ul class="checks">
        {#each manifest.checks as check (check.name)}
          <li>
            <span class="check-name">{check.name}</span>
            <span class="check-status" class:danger={check.status !== "pass"}>{check.status}</span>
            {#if check.detail}<span class="check-detail">{check.detail}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#if manifest.proposed_worklog_total_hours}
      <p class="worklog">
        Proposed worklog: {manifest.proposed_worklog_total_hours.toFixed(1)}h
        {#if manifest.proposed_worklog_unmapped_hours}
          ({manifest.proposed_worklog_unmapped_hours.toFixed(1)}h unmapped)
        {/if}
      </p>
    {/if}
    {#if manifest.status !== "pending" && manifest.approved_by}
      <p class="decided">{manifest.status} by {manifest.approved_by}</p>
    {/if}
    {#if error}
      <p role="alert" class="error">{error}</p>
    {/if}
  {/snippet}
  {#snippet footer()}
    <div class="actions">
      <Button variant="primary" size="sm" disabled={!canDecide} onclick={() => resolve(false)}>
        {submitting === "approve" ? "Approving…" : "Approve"}
      </Button>
      <Button variant="danger" size="sm" disabled={!canDecide} onclick={() => resolve(true)}>
        {submitting === "reject" ? "Rejecting…" : "Reject"}
      </Button>
    </div>
  {/snippet}
</Card>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    width: 100%;
  }
  .branches {
    margin: 0;
    font-size: 0.85rem;
    color: var(--color-text);
  }
  .checks {
    list-style: none;
    padding: 0;
    margin: 0.4rem 0 0;
    display: grid;
    gap: 0.25rem;
  }
  .checks li {
    display: flex;
    align-items: baseline;
    gap: 0.4rem;
    font-size: 0.78rem;
  }
  .check-name {
    font-weight: 600;
  }
  .check-status {
    color: var(--color-success);
    text-transform: uppercase;
    font-size: 0.7rem;
  }
  .check-status.danger {
    color: var(--color-danger);
  }
  .check-detail {
    color: var(--color-text-muted);
  }
  .worklog {
    margin: 0.4rem 0 0;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  .decided {
    margin: 0.4rem 0 0;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  .error {
    color: var(--color-danger);
    font-size: 0.8rem;
    margin: 0.4rem 0 0;
  }
  .actions {
    display: flex;
    gap: 0.5rem;
  }
</style>
