<script lang="ts">
  import type { Snippet } from "svelte";
  import BentoCard, { type BentoSize } from "./BentoCard.svelte";
  import type { CardState } from "./Card.svelte";
  import Icon, { type IconName } from "../Icon.svelte";

  export type StatusVariant = "success" | "warning" | "danger" | "info";

  interface Props {
    variant: StatusVariant;
    label: string;
    description?: string;
    size?: BentoSize;
    state?: CardState;
    emptyMessage?: string;
    // Optional body rendered under the label/description. Use it to give the
    // card real content (e.g. a short preview list) so a large cell is not
    // left mostly empty behind a single count.
    children?: Snippet;
  }
  let { variant, label, description, size = "medium", state = "default", emptyMessage, children }: Props = $props();

  // Per §15 accessibility rules: color is never the only signal, so every
  // variant pairs a semantic color with a distinct icon glyph and a text
  // label (same convention StatusBadge already uses).
  const icons: Record<StatusVariant, IconName> = {
    success: "check",
    warning: "alert",
    danger: "x",
    info: "info",
  };
  const glyphs: Record<StatusVariant, string> = {
    success: "✓",
    warning: "⚠",
    danger: "✕",
    info: "i",
  };
</script>

<BentoCard {size} {state} {emptyMessage}>
  {#snippet children()}
    <div class="status status-{variant}">
      <span class="icon" aria-hidden="true">
        <Icon name={icons[variant]} size={17} strokeWidth={2.1} />
        <span class="icon-glyph">{glyphs[variant]}</span>
      </span>
      <div class="text">
        <span class="label">{label}</span>
        {#if description}
          <span class="description">{description}</span>
        {/if}
        {#if children}
          <div class="body">{@render children()}</div>
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
    width: 2rem;
    height: 2rem;
    border-radius: 50%;
    flex-shrink: 0;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, currentColor 14%, transparent);
  }
  .icon-glyph {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }
  .status-success .icon {
    background: color-mix(in srgb, var(--color-success) 12%, var(--color-surface));
    color: var(--color-success);
  }
  .status-warning .icon {
    background: color-mix(in srgb, var(--color-warning) 12%, var(--color-surface));
    color: var(--color-warning);
  }
  .status-danger .icon {
    background: color-mix(in srgb, var(--color-danger) 12%, var(--color-surface));
    color: var(--color-danger);
  }
  .status-info .icon {
    background: color-mix(in srgb, var(--color-info) 12%, var(--color-surface));
    color: var(--color-info);
  }
  .text {
    display: grid;
    gap: 0.2rem;
    min-width: 0;
    width: 100%;
  }
  .body {
    margin-top: 0.55rem;
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
