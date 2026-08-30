<script lang="ts">
  import { onMount } from "svelte";
  import { getSystem, type SystemInfo } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import Icon, { type IconName } from "../../lib/components/Icon.svelte";
  import AccentPicker from "../../lib/components/AccentPicker.svelte";

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
  <section class="appearance" aria-labelledby="accent-heading">
    <div class="appearance-copy">
      <span class="appearance-icon"><Icon name="palette" size={20} /></span>
      <div>
        <span class="eyebrow">Appearance</span>
        <h2 id="accent-heading">Color accent</h2>
        <p>Choose the highlight color used for navigation, focus states, charts, and primary actions.</p>
      </div>
    </div>
    <AccentPicker />
  </section>

  <div class="section-heading">
    <div>
      <span class="eyebrow">Diagnostics</span>
      <h2>Runtime information</h2>
    </div>
    <span class="local-badge"><Icon name="server" size={14} /> Local only</span>
  </div>

  {@const items: { label: string; value: string | number; icon: IconName }[] = [
    { label: "Panel version", value: info.panel_version, icon: "code" },
    { label: "Punakawan version", value: info.punakawan_version, icon: "workspace" },
    { label: "Server started", value: new Date(info.server_start_time).toLocaleString(), icon: "clock" },
    { label: "Bound address", value: info.bound_address, icon: "server" },
    { label: "Read-only mode", value: info.read_only ? "yes" : "no", icon: "approval" },
    { label: "Registered workspaces", value: info.registered_workspaces, icon: "folder" },
  ]}
  <dl>
    {#each items as item (item.label)}
      <div class="row">
        <span class="icon"><Icon name={item.icon} size={18} /></span>
        <dt>{item.label}</dt>
        <dd>{item.value}</dd>
      </div>
    {/each}
  </dl>
{/if}

<style>
  .appearance {
    display: grid;
    grid-template-columns: minmax(260px, 0.8fr) minmax(360px, 1.2fr);
    gap: 1.25rem;
    align-items: center;
    margin-top: 1rem;
    padding: 1.1rem;
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    background:
      radial-gradient(circle at 95% 10%, var(--color-accent-soft), transparent 36%),
      var(--surface-card-bg, var(--color-surface));
    box-shadow: var(--shadow-card);
  }
  .appearance-copy {
    display: flex;
    align-items: flex-start;
    gap: 0.75rem;
  }
  .appearance-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.65rem;
    height: 2.65rem;
    flex: 0 0 auto;
    color: var(--color-accent);
    border: 1px solid color-mix(in srgb, var(--color-accent) 20%, var(--color-border));
    border-radius: 11px;
    background: var(--color-accent-soft);
  }
  .eyebrow {
    color: var(--color-accent);
    font-size: 0.68rem;
    font-weight: 750;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .appearance h2,
  .section-heading h2 {
    margin: 0.14rem 0 0;
    color: var(--color-text);
    font-size: 1rem;
  }
  .appearance p {
    max-width: 34rem;
    margin: 0.3rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.8rem;
    line-height: 1.5;
  }
  .section-heading {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 1rem;
    margin-top: 1.35rem;
  }
  .local-badge {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.25rem 0.55rem;
    color: var(--color-text-muted);
    border: 1px solid var(--color-border);
    border-radius: 999px;
    background: var(--color-surface-subtle);
    font-size: 0.72rem;
    font-weight: 650;
  }
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
  .error {
    color: var(--color-danger);
  }

  @media (max-width: 760px) {
    .appearance {
      grid-template-columns: 1fr;
    }
  }
</style>
