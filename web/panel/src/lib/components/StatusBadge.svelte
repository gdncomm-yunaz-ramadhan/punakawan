<script lang="ts">
  import type { Availability } from "../api/client";

  // Generic semantic variants for callers that aren't rendering a
  // workspace Availability (e.g. DataTable status cells, ReviewCard).
  // Kept distinct from Availability's own five states so existing call
  // sites (WorkspacesList, WorkspaceSummary, Overview) are untouched.
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

  const icons: Record<Availability, string> = {
    available: "✓",
    partially_available: "⚠",
    busy: "●",
    unavailable: "✕",
    invalid: "?",
  };

  const variantIcons: Record<BadgeVariant, string> = {
    success: "✓",
    warning: "⚠",
    danger: "✕",
    info: "ℹ",
    neutral: "●",
  };

  const resolvedClass = $derived(availability ? `status-${availability}` : `status-variant-${variant}`);
  const resolvedIcon = $derived(availability ? icons[availability] : variantIcons[variant as BadgeVariant]);
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
  <span aria-hidden="true">{resolvedIcon}</span>
  {resolvedLabel}
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.8rem;
    padding: 0.1rem 0.5rem;
    border-radius: 4px;
    font-weight: 500;
  }
  .status-available {
    background: #e6f4ea;
    color: #1e7d32;
  }
  .status-partially_available {
    background: #fff4e5;
    color: #9a6700;
  }
  .status-busy {
    background: #e8eaf6;
    color: #3949ab;
  }
  .status-unavailable,
  .status-invalid {
    background: #fdecea;
    color: #c62828;
  }

  .status-variant-success {
    background: var(--color-accent-soft);
    color: var(--color-success);
  }
  .status-variant-warning {
    background: var(--color-accent-soft);
    color: var(--color-warning);
  }
  .status-variant-danger {
    background: var(--color-accent-soft);
    color: var(--color-danger);
  }
  .status-variant-info {
    background: var(--color-accent-soft);
    color: var(--color-info);
  }
  .status-variant-neutral {
    background: var(--color-surface-subtle);
    color: var(--color-text-muted);
  }
</style>
