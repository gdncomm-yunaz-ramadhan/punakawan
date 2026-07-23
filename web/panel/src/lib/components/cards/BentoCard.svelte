<script lang="ts">
  import type { Snippet } from "svelte";
  import Card, { type CardState } from "./Card.svelte";

  export type BentoSize = "small" | "medium" | "wide" | "tall" | "full";

  interface Props {
    size?: BentoSize;
    // Column span override for sizes with a range (medium: 4-6, wide:
    // 8-12) or for "tall", which only fixes row span and leaves column
    // span to the caller. Defaults per size follow §13.5's supported
    // spans, picking the low end of each range as the concrete default.
    columns?: number;
    state?: CardState;
    emptyMessage?: string;
    warningMessage?: string;
    errorMessage?: string;
    header?: Snippet;
    footer?: Snippet;
    children?: Snippet;
  }
  let {
    size = "medium",
    columns,
    state = "default",
    emptyMessage,
    warningMessage,
    errorMessage,
    header,
    footer,
    children,
  }: Props = $props();

  // Defaults per §13.5: small=3, medium=4 (of its 4-6 range), wide=8 (of
  // its 8-12 range), tall keeps whatever column span it's given (default
  // 4, same as medium) and additionally spans 2 rows, full=full width
  // (12 of 12).
  const defaultColumns: Record<BentoSize, number> = {
    small: 3,
    medium: 4,
    wide: 8,
    tall: 4,
    full: 12,
  };

  const columnSpan = $derived(columns ?? defaultColumns[size]);
  const rowSpan = $derived(size === "tall" ? 2 : 1);
</script>

<!--
  Wraps Card with a semantic bento size (UI-010, §13.5) so feature pages
  declare "small"/"medium"/"wide"/"tall"/"full" instead of writing raw
  grid-column/grid-row CSS themselves.
-->
<div
  class="bento-card"
  style={`grid-column: span ${columnSpan}; grid-row: span ${rowSpan};`}
  data-size={size}
  data-columns={columnSpan}
  data-rows={rowSpan}
>
  <Card {state} {emptyMessage} {warningMessage} {errorMessage} {header} {footer} {children} />
</div>

<style>
  .bento-card {
    min-width: 0;
    min-height: 0;
  }
  .bento-card :global(.card) {
    height: 100%;
  }
</style>
