<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon, { type IconName } from "./Icon.svelte";

  // Shared Setara button primitive. Every interactive button/CTA in the
  // panel should use this rather than a bespoke `.btn` class, so weight,
  // radius, focus, and disabled states stay consistent and theme-aware.
  // Renders an <a> when `href` is set, otherwise a <button>.
  export type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";
  export type ButtonSize = "sm" | "md";

  interface Props {
    variant?: ButtonVariant;
    size?: ButtonSize;
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    // Stretch to the container width (used when buttons stack full-width on
    // mobile, or for proportional button rows).
    fullWidth?: boolean;
    href?: string;
    icon?: IconName;
    ariaLabel?: string;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    children?: Snippet;
  }
  let {
    variant = "secondary",
    size = "md",
    type = "button",
    disabled = false,
    fullWidth = false,
    href,
    icon,
    ariaLabel,
    title,
    onclick,
    children,
  }: Props = $props();
</script>

{#if href}
  <a
    class="btn {variant} {size}"
    class:full={fullWidth}
    class:disabled
    {href}
    aria-label={ariaLabel}
    {title}
    aria-disabled={disabled ? "true" : undefined}
    onclick={disabled ? undefined : onclick}
  >
    {#if icon}<Icon name={icon} size={size === "sm" ? 14 : 16} />{/if}
    {#if children}<span class="label">{@render children()}</span>{/if}
  </a>
{:else}
  <button
    class="btn {variant} {size}"
    class:full={fullWidth}
    {type}
    {disabled}
    aria-label={ariaLabel}
    {title}
    {onclick}
  >
    {#if icon}<Icon name={icon} size={size === "sm" ? 14 : 16} />{/if}
    {#if children}<span class="label">{@render children()}</span>{/if}
  </button>
{/if}

<style>
  /* Mirrors setara-ui Button.svelte, mapped onto the panel's theme tokens
     (--color-accent-soft == Setara's accent-subtle). */
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font: inherit;
    font-weight: 600;
    line-height: 1.4;
    text-decoration: none;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    cursor: pointer;
    box-sizing: border-box;
    white-space: nowrap;
    outline: none;
  }
  .btn.md {
    font-size: 0.875rem;
    padding: 8px 18px;
    min-height: 38px;
  }
  .btn.sm {
    font-size: 0.8rem;
    padding: 6px 12px;
    min-height: 32px;
  }
  /* Larger tap targets on touch/small screens. */
  @media (max-width: 768px) {
    .btn.md {
      min-height: 44px;
      padding: 10px 18px;
    }
    .btn.sm {
      min-height: 36px;
      padding: 7px 12px;
    }
  }
  .btn.full {
    width: 100%;
  }
  .label {
    min-width: 0;
  }

  .btn:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-accent) 20%, transparent);
  }
  .btn:disabled,
  .btn.disabled {
    opacity: 0.5;
    cursor: not-allowed;
    pointer-events: none;
  }

  /* Primary: solid accent. */
  .btn.primary {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .btn.primary:hover:not(:disabled):not(.disabled) {
    background: var(--color-accent-hover);
    border-color: var(--color-accent-hover);
    box-shadow: 0 2px 8px color-mix(in srgb, var(--color-accent) 25%, transparent);
  }

  /* Secondary: accent-tinted fill with accent text (the Setara signature). */
  .btn.secondary {
    background: var(--color-accent-soft);
    color: var(--color-accent);
    border-color: var(--color-border);
  }
  .btn.secondary:hover:not(:disabled):not(.disabled) {
    background: var(--color-border);
  }

  /* Danger: soft red fill, danger text. */
  .btn.danger {
    background: color-mix(in srgb, var(--color-danger) 14%, var(--color-surface));
    color: var(--color-danger);
    border-color: color-mix(in srgb, var(--color-danger) 26%, transparent);
  }
  .btn.danger:hover:not(:disabled):not(.disabled) {
    background: color-mix(in srgb, var(--color-danger) 22%, var(--color-surface));
    border-color: var(--color-danger);
  }

  /* Ghost: transparent with a border, accent-tinted on hover. */
  .btn.ghost {
    background: transparent;
    border-color: var(--color-border);
    color: var(--color-text-muted);
  }
  .btn.ghost:hover:not(:disabled):not(.disabled) {
    background: var(--color-accent-soft);
    border-color: var(--color-accent);
    color: var(--color-accent);
  }

  @media (prefers-reduced-motion: no-preference) {
    .btn {
      transition:
        background 150ms ease,
        color 150ms ease,
        border-color 150ms ease,
        box-shadow 150ms ease,
        transform 80ms ease;
    }
    .btn:active:not(:disabled):not(.disabled) {
      transform: translateY(1px);
    }
  }
</style>
