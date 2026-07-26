<script lang="ts">
  import { onMount } from "svelte";
  import {
    getSystem,
    getPanelSettings,
    updatePanelSettings,
    type SystemInfo,
    type PanelSettings,
  } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import Icon, { type IconName } from "../../lib/components/Icon.svelte";
  import AccentPicker from "../../lib/components/AccentPicker.svelte";
  import Button from "../../lib/components/Button.svelte";

  let info: SystemInfo | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(true);

  // Runtime pool settings: loaded independently of the diagnostics above so
  // a failure in one doesn't blank the other.
  let settings: PanelSettings | null = $state(null);
  let settingsError: string | null = $state(null);
  let settingsLoading = $state(true);
  // Form model, seeded from the loaded settings and edited in place.
  let maxActiveRuntimes = $state(1);
  let idleTimeoutSeconds = $state(1);
  let saving = $state(false);
  let saved = $state(false);
  let saveError: string | null = $state(null);

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

  async function loadSettings() {
    settingsLoading = true;
    settingsError = null;
    try {
      const s = await getPanelSettings();
      settings = s;
      maxActiveRuntimes = s.max_active_runtimes;
      idleTimeoutSeconds = s.runtime_idle_timeout_seconds;
    } catch (e) {
      settingsError = e instanceof Error ? e.message : String(e);
    } finally {
      settingsLoading = false;
    }
  }

  // Clear a stale "Saved"/error banner as soon as the user edits a field.
  function onEdit() {
    saved = false;
    saveError = null;
  }

  async function saveSettings() {
    saving = true;
    saved = false;
    saveError = null;
    try {
      const updated = await updatePanelSettings({
        max_active_runtimes: maxActiveRuntimes,
        runtime_idle_timeout_seconds: idleTimeoutSeconds,
      });
      settings = updated;
      maxActiveRuntimes = updated.max_active_runtimes;
      idleTimeoutSeconds = updated.runtime_idle_timeout_seconds;
      saved = true;
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  onMount(() => {
    load();
    loadSettings();
  });
</script>

<PageHeader
  title="System"
  description="Local diagnostic info about this panel process. Never shows tokens, secrets, environment variables, or agent reasoning - only the facts below."
/>

<section class="runtime-pool" aria-labelledby="runtime-pool-heading">
  <div class="section-heading">
    <div>
      <span class="eyebrow">Resources</span>
      <h2 id="runtime-pool-heading">Runtime pool</h2>
    </div>
  </div>

  {#if settingsLoading}
    <p>Loading…</p>
  {:else if settingsError}
    <p role="alert" class="error">Failed to load runtime settings: {settingsError}</p>
  {:else if settings}
    <form
      class="runtime-form"
      onsubmit={(e) => {
        e.preventDefault();
        saveSettings();
      }}
    >
      <label>
        <span class="field-label">Max active runtimes</span>
        <input
          type="number"
          min="1"
          step="1"
          bind:value={maxActiveRuntimes}
          oninput={onEdit}
          aria-label="Max active runtimes"
        />
        <span class="help">
          Caps how many project workspaces run their own <code>dolt sql-server</code> at once;
          least-recently-used idle projects are shut down when the cap is exceeded (the primary
          project is never evicted, and lowering the cap frees memory immediately).
        </span>
      </label>

      <label>
        <span class="field-label">Idle timeout (seconds)</span>
        <input
          type="number"
          min="1"
          step="1"
          bind:value={idleTimeoutSeconds}
          oninput={onEdit}
          aria-label="Runtime idle timeout in seconds"
        />
        <span class="help">
          How long an idle non-primary project's <code>dolt sql-server</code> lingers before it is
          shut down.
        </span>
      </label>

      <div class="runtime-actions">
        <Button type="submit" variant="primary" disabled={saving}>
          {saving ? "Saving…" : "Save"}
        </Button>
        {#if saved}
          <span class="save-status ok" role="status">Saved</span>
        {:else if saveError}
          <span class="save-status error" role="alert">{saveError}</span>
        {/if}
      </div>
    </form>
  {/if}
</section>

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

  .runtime-pool {
    margin-top: 1rem;
    padding: 1.1rem;
    border: 1px solid var(--surface-card-border, var(--color-border));
    border-radius: var(--radius-card);
    background: var(--surface-card-bg, var(--color-surface));
    box-shadow: var(--shadow-card);
  }
  .runtime-pool .section-heading {
    margin-top: 0;
  }
  .runtime-form {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 1.1rem;
    margin-top: 1rem;
  }
  .runtime-form label {
    display: grid;
    gap: 0.35rem;
    align-content: start;
  }
  .field-label {
    color: var(--color-text);
    font-size: 0.8rem;
    font-weight: 650;
  }
  .runtime-form input {
    font: inherit;
    padding: 0.4rem 0.55rem;
    border: 1px solid var(--color-border-strong, var(--color-border));
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    min-height: 40px;
    max-width: 12rem;
    box-sizing: border-box;
  }
  .runtime-form input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .help {
    color: var(--color-text-muted);
    font-size: 0.76rem;
    line-height: 1.5;
  }
  .help code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.72rem;
    background: var(--color-surface-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.3rem;
  }
  .runtime-actions {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .save-status {
    font-size: 0.8rem;
    font-weight: 600;
  }
  .save-status.ok {
    color: var(--color-accent);
  }
  .save-status.error {
    color: var(--color-danger);
  }

  @media (max-width: 760px) {
    .appearance {
      grid-template-columns: 1fr;
    }
  }
</style>
