<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    children: Snippet;
  }
  let { children }: Props = $props();
</script>

<!--
  CSS grid container per §13.5: 12 columns/16px gap desktop (>=1024px),
  6 columns/12px gap tablet (640-1023px), 1 column/12px gap mobile
  (<640px), 120px minimum row height. BentoCard children set their own
  grid-column/grid-row span; this container only owns the track layout.
-->
<div class="bento-grid">
  {@render children()}
</div>

<style>
  .bento-grid {
    display: grid;
    grid-template-columns: repeat(1, 1fr);
    grid-auto-rows: minmax(120px, auto);
    gap: 12px;
  }

  @media (min-width: 640px) {
    .bento-grid {
      grid-template-columns: repeat(6, 1fr);
      gap: 12px;
    }
  }

  @media (min-width: 1024px) {
    .bento-grid {
      grid-template-columns: repeat(12, 1fr);
      gap: 16px;
    }
  }
</style>
