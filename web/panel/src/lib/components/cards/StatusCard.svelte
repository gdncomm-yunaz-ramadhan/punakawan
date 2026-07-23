<script lang="ts">
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";

  export type StatusVariant = "success" | "warning" | "danger" | "info";

  interface Props {
    variant: StatusVariant;
    label: string;
    description?: string;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
  }
  let { variant, label, description, size = "medium", state = "default", emptyMessage }: Props = $props();

  // Per §15 accessibility rules: color is never the only signal, so every
  // variant pairs a semantic color with a distinct icon glyph and a text
  // label (same convention StatusBadge already uses).
  const icons: Record<StatusVariant, string> = {
    success: "✓",
    warning: "⚠",
    danger: "✕",
    info: "ℹ",
  };
</script>

<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div class="status status-{variant}">
      <span class="icon" aria-hidden="true">{icons[variant]}</span>
      <div class="text">
        <span class="label">{label}</span>
        {#if description}
          <span class="description">{description}</span>
        {/if}
      </div>
    </div>
  {/snippet}
</BentoCard>

<style>
  .status {
    display: flex;
    align-items: flex-start;
    gap: 0.6rem;
    height: 100%;
  }
  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.6rem;
    height: 1.6rem;
    border-radius: 50%;
    flex-shrink: 0;
    font-size: 0.9rem;
  }
  .status-success .icon {
    background: var(--color-accent-soft);
    color: var(--color-success);
  }
  .status-warning .icon {
    background: var(--color-accent-soft);
    color: var(--color-warning);
  }
  .status-danger .icon {
    background: var(--color-accent-soft);
    color: var(--color-danger);
  }
  .status-info .icon {
    background: var(--color-accent-soft);
    color: var(--color-info);
  }
  .text {
    display: grid;
    gap: 0.2rem;
  }
  .label {
    font-weight: 600;
    color: var(--color-text);
  }
  .description {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
</style>
