<script lang="ts">
  import type { SystemInfo } from "../api/client";
  import { getConnectionStatus } from "../events/sse.svelte";
  import Icon from "./Icon.svelte";

  interface Props {
    system: SystemInfo | null;
  }
  let { system }: Props = $props();

  let now = $state(new Date());
  if (typeof window !== "undefined") {
    setInterval(() => {
      now = new Date();
    }, 1000);
  }

  const connectionLabels = { connecting: "Connecting…", open: "Live", error: "Reconnecting…" };
  const versionTitle = $derived(
    system ? `Panel v${system.panel_version} · Punakawan v${system.punakawan_version}` : "",
  );
</script>

<!--
  Deliberately minimal: the sidebar already carries the brand, so the top
  bar is only a right-aligned status strip. Version and connection state
  are compact glyphs whose full text is revealed on hover (title) and to
  assistive tech (aria-label), rather than always-on chips.
-->
<header>
  {#if system}
    <span class="info" data-testid="panel-version" title={versionTitle} aria-label={versionTitle}><Icon name="info" size={16} /></span>
  {/if}
  <span
    class="connection connection-{getConnectionStatus()}"
    data-testid="connection-indicator"
    title={connectionLabels[getConnectionStatus()]}
    aria-label={`Connection: ${connectionLabels[getConnectionStatus()]}`}
  >
    <span class="connection-dot" aria-hidden="true"></span>
    <span class="connection-label">{connectionLabels[getConnectionStatus()]}</span>
  </span>
  <time title="Local time">{now.toLocaleTimeString()}</time>
</header>

<style>
  header {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.75rem;
    min-height: 52px;
    padding: 0.55rem 2rem;
    border-bottom: 1px solid var(--color-border);
    background: color-mix(in srgb, var(--color-surface) 90%, transparent);
    backdrop-filter: blur(14px);
  }
  /* Signature batik ribbon: a 3px gold->terracotta->teal->indigo bar
     running the full width of the header's top edge. */
  header::before {
    content: "";
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 3px;
    background: var(--gradient-brand);
  }
  .info {
    display: inline-flex;
    color: var(--color-text-muted);
    font-size: 0.95rem;
    line-height: 1;
    cursor: help;
  }
  time {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    font-variant-numeric: tabular-nums;
  }
  .connection {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.22rem 0.55rem;
    border: 1px solid var(--color-border);
    border-radius: 999px;
    background: var(--color-surface);
    font-size: 0.72rem;
    font-weight: 600;
    color: var(--color-text-muted);
    cursor: default;
  }
  .connection-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
    box-shadow: 0 0 0 3px color-mix(in srgb, currentColor 13%, transparent);
  }
  .connection-open {
    color: var(--color-success);
  }
  .connection-error {
    color: var(--color-danger);
  }
  .connection-connecting {
    color: var(--color-warning);
  }
</style>
