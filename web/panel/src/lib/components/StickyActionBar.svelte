<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    children: Snippet;
  }
  let { children }: Props = $props();
</script>

<!--
  Keeps 1-3 primary actions reachable during review without hover, per
  §13.4 ("Primary actions remain visible through a sticky action bar when
  reviewing"). Sticks to the bottom of the viewport only below 640px -
  above that, actions render inline where callers place this component.
-->
<div class="sticky-bar">
  {@render children()}
</div>

<style>
  .sticky-bar {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
  }

  @media (max-width: 639px) {
    .sticky-bar {
      position: sticky;
      bottom: 0;
      justify-content: stretch;
      background: var(--color-surface-raised);
      border-top: 1px solid var(--color-border);
      padding: 0.6rem 0.75rem;
      padding-bottom: calc(0.6rem + env(safe-area-inset-bottom, 0px));
      box-shadow: var(--shadow-card);
      z-index: 15;
    }
    .sticky-bar :global(button) {
      flex: 1;
      min-height: 44px;
    }
  }
</style>
