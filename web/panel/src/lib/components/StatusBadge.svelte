<script lang="ts">
  import type { Availability } from "../api/client";

  interface Props {
    availability: Availability;
  }
  let { availability }: Props = $props();

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
</script>

<!--
  Per §15: color must never be the only signal. Every badge pairs a
  status class (color) with an icon glyph and text label.
-->
<span class="status status-{availability}">
  <span aria-hidden="true">{icons[availability]}</span>
  {labels[availability]}
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
</style>
