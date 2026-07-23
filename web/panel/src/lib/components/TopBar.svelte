<script lang="ts">
  import type { SystemInfo } from "../api/client";

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
</script>

<header>
  <h1>Punakawan Panel</h1>
  <div class="spacer"></div>
  {#if system}
    <span class="badge" data-testid="read-only-badge">
      {system.read_only ? "Read-only" : "Read-write"}
    </span>
    <span class="version">v{system.panel_version}</span>
  {/if}
  <!-- SSE-backed live connection status arrives in Phase 3; until then
       this is intentionally omitted rather than showing a fake indicator. -->
  <time>{now.toLocaleTimeString()}</time>
</header>

<style>
  header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid #e0e0e0;
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
    background: #eee;
  }
  .version,
  time {
    color: #666;
    font-size: 0.85rem;
  }
</style>
