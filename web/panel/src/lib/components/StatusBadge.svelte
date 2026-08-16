<script lang="ts">
  import type { Availability } from "../api/client";
  import Icon, { type IconName } from "./Icon.svelte";

  // Generic semantic variants for callers that aren't rendering a
  // workspace Availability (e.g. DataTable status cells). Kept distinct
  // from Availability's own five states so existing call sites (Overview)
  // are untouched.
  export type BadgeVariant = "success" | "warning" | "danger" | "info" | "neutral";

  interface AvailabilityProps {
    availability: Availability;
    variant?: undefined;
    label?: undefined;
  }
  interface VariantProps {
    availability?: undefined;
    variant: BadgeVariant;
    label: string;
  }
  type Props = AvailabilityProps | VariantProps;
  let { availability, variant, label }: Props = $props();

  const labels: Record<Availability, string> = {
    available: "Available",
    partially_available: "Partially available",
    busy: "Busy",
    unavailable: "Unavailable",
    invalid: "Invalid",
  };

  const icons: Record<Availability, IconName> = {
    available: "check",
    partially_available: "alert",
    busy: "clock",
    unavailable: "x",
    invalid: "info",
  };

  const variantIcons: Record<BadgeVariant, IconName> = {
    success: "check",
    warning: "alert",
    danger: "x",
    info: "info",
    neutral: "clock",
  };
  const glyphs: Record<Availability | BadgeVariant, string> = {
    available: "✓",
    partially_available: "⚠",
    busy: "●",
    unavailable: "✕",
    invalid: "?",
    success: "✓",
    warning: "⚠",
    danger: "✕",
    info: "i",
    neutral: "●",
  };

  const resolvedClass = $derived(availability ? `status-${availability}` : `status-variant-${variant}`);
  const resolvedIcon = $derived(availability ? icons[availability] : variantIcons[variant as BadgeVariant]);
  const resolvedGlyph = $derived(glyphs[(availability ?? variant) as Availability | BadgeVariant]);
  const resolvedLabel = $derived(availability ? labels[availability] : label);
</script>

<!--
  Per §15: color must never be the only signal. Every badge pairs a
  status class (color) with an icon glyph and text label. Extended
  (UI-011) with a generic variant+label mode for DataTable/ReviewCard
  status cells that aren't workspace Availability values, rather than
  creating a second competing badge component.
-->
<span class="status {resolvedClass}">
  <span class="status-icon" aria-hidden="true">
    <Icon name={resolvedIcon} size={13} strokeWidth={2.2} />
    <span class="icon-glyph">{resolvedGlyph}</span>
  </span>
  {resolvedLabel}
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 0.32rem;
    font-size: 0.76rem;
    padding: 0.2rem 0.58rem;
    border-radius: 999px;
    font-weight: 600;
    line-height: 1.4;
    border: 1px solid transparent;
    box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.28);
  }
  .status-icon {
    display: inline-flex;
  }
  .icon-glyph {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
  }
  /* Semantic pill tints: each background is a low-alpha mix of the
     semantic/batik token over the surface, so contrast holds in both
     light and dark, with the full-strength token as the readable text. */
  .status-available {
    background: color-mix(in srgb, var(--color-success) 14%, var(--color-surface));
    color: var(--color-success);
    border-color: color-mix(in srgb, var(--color-success) 30%, transparent);
  }
  .status-partially_available {
    background: color-mix(in srgb, var(--color-warning) 16%, var(--color-surface));
    color: var(--color-warning);
    border-color: color-mix(in srgb, var(--color-warning) 30%, transparent);
  }
  .status-busy {
    background: var(--color-gold-soft);
    color: var(--color-gold);
    border-color: color-mix(in srgb, var(--color-gold) 30%, transparent);
  }
  .status-unavailable,
  .status-invalid {
    background: color-mix(in srgb, var(--color-danger) 14%, var(--color-surface));
    color: var(--color-danger);
    border-color: color-mix(in srgb, var(--color-danger) 30%, transparent);
  }

  .status-variant-success {
    background: color-mix(in srgb, var(--color-success) 14%, var(--color-surface));
    color: var(--color-success);
    border-color: color-mix(in srgb, var(--color-success) 30%, transparent);
  }
  .status-variant-warning {
    background: color-mix(in srgb, var(--color-warning) 16%, var(--color-surface));
    color: var(--color-warning);
    border-color: color-mix(in srgb, var(--color-warning) 30%, transparent);
  }
  .status-variant-danger {
    background: color-mix(in srgb, var(--color-danger) 14%, var(--color-surface));
    color: var(--color-danger);
    border-color: color-mix(in srgb, var(--color-danger) 30%, transparent);
  }
  .status-variant-info {
    background: color-mix(in srgb, var(--color-info) 14%, var(--color-surface));
    color: var(--color-info);
    border-color: color-mix(in srgb, var(--color-info) 30%, transparent);
  }
  .status-variant-neutral {
    background: var(--color-surface-subtle);
    color: var(--color-text-muted);
    border-color: var(--color-border);
  }
</style>
