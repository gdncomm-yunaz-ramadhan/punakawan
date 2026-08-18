<script lang="ts">
  // Shared confirmation for cancelling a delivery, so the list and the detail
  // page guard the same irreversible action with the same wording instead of
  // one of them cancelling on a single click.
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    open: boolean;
    // The human-readable label for the delivery (deliveryLabel's output), not
    // its raw id - the id is shown separately as the precise reference.
    label: string;
    orchestrationId: string;
    busy: boolean;
    error: string | null;
    onclose: () => void;
    onconfirm: () => void;
  }
  let { open, label, orchestrationId, busy, error, onclose, onconfirm }: Props = $props();
</script>

<Dialog {open} title="Cancel delivery" {onclose}>
  <div class="cancel-confirm">
    <p>Stop <strong>{label}</strong> before it finishes?</p>
    <p class="scope">
      Cancelling records the delivery as cancelled and stops any further lanes from being handed out. It does
      <strong>not</strong> undo work already done: commits, branches, worktrees, and pull requests its lanes have already
      created all stay as they are. The panel cannot reopen a cancelled delivery, and it stays on the list as a record.
    </p>
    <p class="scope">Delivery <code>{orchestrationId}</code></p>

    {#if error}
      <p class="error" role="alert" data-testid="cancel-error">{error}</p>
    {/if}

    <div class="confirm-actions">
      <Button variant="secondary" onclick={onclose} disabled={busy}>Keep running</Button>
      <Button variant="danger" onclick={onconfirm} disabled={busy}>
        {busy ? "Cancelling…" : "Cancel delivery"}
      </Button>
    </div>
  </div>
</Dialog>

<style>
  .cancel-confirm {
    display: grid;
    gap: 0.85rem;
  }
  .cancel-confirm p {
    margin: 0;
    font-size: 0.9rem;
  }
  .cancel-confirm .scope {
    color: var(--color-text-muted);
    font-size: 0.84rem;
  }
  .cancel-confirm code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }
  .cancel-confirm .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
</style>
