<script lang="ts">
  interface Props {
    lines?: number;
  }
  let { lines = 3 }: Props = $props();
</script>

<!--
  Standalone skeleton card (UI-010), usable on its own for a full-page
  loading state rather than only inside a Card's `state` prop.
-->
<div class="skeleton-card" role="status" aria-label="Loading">
  {#each Array(lines) as _, i (i)}
    <span class="skeleton-line" class:short={i === lines - 1}></span>
  {/each}
</div>

<style>
  .skeleton-card {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
    padding: 1.25rem;
    display: grid;
    gap: 0.5rem;
  }
  .skeleton-line {
    display: block;
    height: 0.9rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
  }
  .skeleton-line.short {
    width: 45%;
  }
  @media (prefers-reduced-motion: no-preference) {
    .skeleton-line {
      animation: pulse 1.1s ease-in-out infinite;
    }
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.5;
    }
  }
</style>
