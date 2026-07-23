<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";

  let info: SystemInfo | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  async function load() {
    loading = true;
    error = null;
    try {
      info = await getSystem();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<PageHeader
  title="System"
  description="Local diagnostic info about this panel process. Never shows tokens, secrets, environment variables, or agent reasoning - only the facts below."
/>

{#if loading}
  <p>Loading…</p>
{:else if error}
  <p role="alert" class="error">Failed to load system info: {error}</p>
{:else if info}
  <dl>
    <div class="row"><dt>Panel version</dt><dd>{info.panel_version}</dd></div>
    <div class="row"><dt>Punakawan version</dt><dd>{info.punakawan_version}</dd></div>
    <div class="row"><dt>Server started</dt><dd>{info.server_start_time}</dd></div>
    <div class="row"><dt>Bound address</dt><dd>{info.bound_address}</dd></div>
    <div class="row"><dt>Read-only</dt><dd>{info.read_only ? "yes" : "no"}</dd></div>
    <div class="row"><dt>Registered workspaces</dt><dd>{info.registered_workspaces}</dd></div>
    <div class="row"><dt>Watcher status</dt><dd>{info.watcher_status}</dd></div>
    <div class="row">
      <dt>Feature flags</dt>
      <dd>{info.feature_flags.length ? info.feature_flags.join(", ") : "none"}</dd>
    </div>
  </dl>
{/if}

<style>
  dl {
    display: grid;
    gap: 0.4rem;
    margin-top: 1rem;
  }
  .row {
    display: grid;
    grid-template-columns: 200px 1fr;
    gap: 0.5rem;
    border-bottom: 1px solid #eee;
    padding-bottom: 0.4rem;
  }
  dt {
    color: #666;
    font-size: 0.85rem;
  }
  dd {
    margin: 0;
    font-size: 0.9rem;
  }
  .error {
    color: #b00020;
  }
</style>
