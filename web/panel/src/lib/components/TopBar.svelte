<script lang="ts">
  import type { SystemInfo } from "../api/client";
  import { getConnectionStatus } from "../events/sse.svelte";

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
</script>

<header>
  <img class="logo" src="/logo.svg" alt="" aria-hidden="true" width="28" height="28" />
  <h1>Punakawan Panel</h1>
  <div class="spacer"></div>
  {#if system}
    <span class="badge" data-testid="read-only-badge">
      {system.read_only ? "Read-only" : "Read-write"}
    </span>
    <span class="version">v{system.panel_version}</span>
  {/if}
  <span class="connection connection-{getConnectionStatus()}" data-testid="connection-indicator">
    <span aria-hidden="true">●</span>
    {connectionLabels[getConnectionStatus()]}
  </span>
  <time>{now.toLocaleTimeString()}</time>
</header>

<style>
  header {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--color-border);
    background: linear-gradient(180deg, var(--color-surface) 0%, var(--color-surface-subtle) 100%);
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
  .logo {
    display: block;
    flex-shrink: 0;
  }
  /* Logo art is a monochrome black silhouette; invert it in dark mode so it
     reads as light-on-dark. data-theme lives on <html> (see index.html). */
  :global(html[data-theme="dark"]) .logo {
    filter: invert(1);
  }
  h1 {
    font-size: 1.1rem;
    margin: 0;
  }
  .spacer {
    flex: 1;
  }
  .badge {
    font-size: 0.75rem;
    padding: 0.15rem 0.5rem;
    border-radius: 4px;
    background: var(--color-surface-subtle);
  }
  .version,
  time {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .connection {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.8rem;
    color: var(--color-text-muted);
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
