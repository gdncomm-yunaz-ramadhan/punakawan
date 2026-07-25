<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import Icon, { type IconName } from "../../lib/components/Icon.svelte";

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
  {@const items: { label: string; value: string | number; icon: IconName }[] = [
    { label: "Panel version", value: info.panel_version, icon: "code" },
    { label: "Punakawan version", value: info.punakawan_version, icon: "workspace" },
    { label: "Server started", value: new Date(info.server_start_time).toLocaleString(), icon: "clock" },
    { label: "Bound address", value: info.bound_address, icon: "server" },
    { label: "Read-only mode", value: info.read_only ? "yes" : "no", icon: "approval" },
    { label: "Registered workspaces", value: info.registered_workspaces, icon: "folder" },
    { label: "Watcher status", value: info.watcher_status, icon: "activity" },
  ]}
  <dl>
    {#each items as item (item.label)}
      <div class="row">
        <span class="icon"><Icon name={item.icon} size={18} /></span>
        <dt>{item.label}</dt>
        <dd>{item.value}</dd>
      </div>
    {/each}
    <div class="row flags">
      <span class="icon"><Icon name="settings" size={18} /></span>
      <dt>Feature flags</dt>
      <dd>
        {#if info.feature_flags.length}
          {#each info.feature_flags as flag (flag)}<span class="flag">{flag}</span>{/each}
        {:else}
          <span class="muted">No feature flags enabled</span>
        {/if}
      </dd>
    </div>
  </dl>
{/if}

<style>
  dl {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 0.85rem;
    margin-top: 1rem;
  }
  .row {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.15rem 0.75rem;
    align-items: center;
    min-height: 86px;
    padding: 1rem;
    background: var(--surface-card-bg, var(--color-surface));
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
  }
  .icon {
    grid-row: 1 / span 2;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.35rem;
    height: 2.35rem;
    border-radius: 10px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  dt {
    color: var(--color-text-muted);
    font-size: 0.76rem;
    font-weight: 650;
  }
  dd {
    margin: 0;
    color: var(--color-text);
    font-size: 0.9rem;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .flags {
    grid-column: 1 / -1;
  }
  .flag {
    display: inline-flex;
    margin: 0.15rem 0.25rem 0.15rem 0;
    padding: 0.16rem 0.48rem;
    border-radius: 999px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    font-size: 0.72rem;
  }
  .muted {
    color: var(--color-text-muted);
    font-weight: 500;
  }
  .error {
    color: var(--color-danger);
  }
</style>
