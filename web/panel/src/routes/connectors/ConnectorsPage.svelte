<script lang="ts">
  import { onMount } from "svelte";
  import { getConnectors, type ConnectorAdapter, type ConnectorOrganization, type Connectors } from "../../lib/api/client";
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import BentoGrid from "../../lib/components/cards/BentoGrid.svelte";
  import BentoCard from "../../lib/components/cards/BentoCard.svelte";
  import MetricCard from "../../lib/components/cards/MetricCard.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import Icon from "../../lib/components/Icon.svelte";

  let data = $state<Connectors | null>(null);
  let error = $state<string | null>(null);
  let loading = $state(true);
  let selected = $state<{ adapter: ConnectorAdapter; org: ConnectorOrganization } | null>(null);

  onMount(async () => {
    try {
      data = await getConnectors();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  });

  const adapters = $derived(data?.adapters ?? []);
  const connectedCount = $derived(adapters.reduce((n: number, a: ConnectorAdapter) => n + a.organizations.length, 0));
  const readyCount = $derived(adapters.filter((a: ConnectorAdapter) => a.installed && a.organizations.length > 0).length);

  // An adapter is only usable when its files are deployed AND it holds a
  // credential, so those two halves are reported separately rather than
  // collapsed into one "connected" flag that hides which half is missing.
  function adapterState(adapter: ConnectorAdapter): { variant: "success" | "warning" | "neutral"; label: string } {
    if (!adapter.installed) return { variant: "warning", label: "Not installed" };
    if (adapter.organizations.length === 0) return { variant: "neutral", label: "No account" };
    return { variant: "success", label: "Connected" };
  }

  function setupCommand(adapter: ConnectorAdapter): string {
    return adapter.provider ? `punakawan setup ${adapter.provider}` : "";
  }

  function formatDate(value?: string): string {
    if (!value) return "never";
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
  }
</script>

<PageHeader
  title="Connectors"
  description="The adapters this install can start, and the organisations whose credentials it holds. Nothing here contacts a provider."
/>

{#if error}
  <p role="alert" class="error">Failed to load connectors: {error}</p>
{:else if loading}
  <p>Loading connectors…</p>
{:else}
  <BentoGrid>
    <MetricCard label="Adapters" value={adapters.length} columns={4} accent="indigo" icon="server" />
    <MetricCard label="Connected organisations" value={connectedCount} columns={4} accent="teal" icon="users" />
    <MetricCard label="Ready to use" value={readyCount} columns={4} accent="gold" icon="check" />

    {#each adapters as adapter (adapter.id)}
      {@const state = adapterState(adapter)}
      <BentoCard size="medium" columns={6}>
        {#snippet header()}
          <div class="adapter-header">
            <h2>{adapter.label}</h2>
            <StatusBadge variant={state.variant} label={state.label} />
          </div>
        {/snippet}

        <dl class="facts">
          <dt>Adapter id</dt>
          <dd><code>{adapter.id}</code></dd>
          {#if adapter.entrypoint}
            <dt>Entrypoint</dt>
            <dd><code class="path">{adapter.entrypoint}</code></dd>
          {/if}
        </dl>

        {#if adapter.organizations.length === 0}
          <p class="empty">
            No organisation connected.
            {#if setupCommand(adapter)}
              Run <code>{setupCommand(adapter)}</code> to add one.
            {/if}
          </p>
        {:else}
          <ul class="orgs" aria-label={`${adapter.label} organisations`}>
            {#each adapter.organizations as org (org.adapter_id)}
              <li>
                <button type="button" class="org" onclick={() => (selected = { adapter, org })}>
                  <span class="org-name">
                    {org.id}
                    {#if org.default}<span class="default-tag">default</span>{/if}
                  </span>
                  <span class="org-host">{org.host}</span>
                  <Icon name="info" />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </BentoCard>
    {/each}

    {#if adapters.length === 0}
      <BentoCard size="full">
        <p class="empty">
          Nothing is connected yet. Run <code>punakawan setup jira</code> or
          <code>punakawan setup github</code> to add an organisation.
        </p>
      </BentoCard>
    {/if}
  </BentoGrid>

  {#if data?.credentials_path}
    <p class="credentials-note">
      Credentials are stored in <code>{data.credentials_path}</code>, readable only by you. Tokens are
      never sent to this page.
    </p>
  {/if}
{/if}

<Dialog
  open={selected !== null}
  title={selected ? `${selected.adapter.label} · ${selected.org.id}` : ""}
  size="md"
  onclose={() => (selected = null)}
>
  {#if selected}
    <dl class="facts detail">
      <dt>Site</dt>
      <dd><a href={selected.org.base_url} target="_blank" rel="noreferrer noopener">{selected.org.base_url}</a></dd>
      {#if selected.org.account}
        <dt>Account</dt>
        <dd>{selected.org.account}</dd>
      {/if}
      <dt>Adapter process</dt>
      <dd><code>{selected.org.adapter_id}</code></dd>
      <dt>Auth</dt>
      <dd>{selected.org.token_scoped ? "Scoped token (API gateway)" : "API token"}</dd>
      <dt>Default for new work</dt>
      <dd>{selected.org.default ? "yes" : "no"}</dd>
      <dt>Added</dt>
      <dd>{formatDate(selected.org.added_at)}</dd>
      <dt>Last verified</dt>
      <dd>{formatDate(selected.org.last_verified_at)}</dd>
    </dl>
    {#if setupCommand(selected.adapter)}
      <p class="hint">
        Re-check or replace this credential with
        <code>{setupCommand(selected.adapter)} --url {selected.org.base_url}</code>, and remove it with
        <code>{setupCommand(selected.adapter)} --remove {selected.org.id}</code>.
      </p>
    {/if}
  {/if}
</Dialog>

<style>
  .error {
    color: var(--color-danger);
  }
  .adapter-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }
  .adapter-header h2 {
    margin: 0;
    font-size: 1rem;
    font-weight: 700;
  }
  .facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.2rem 0.75rem;
    margin: 0 0 0.75rem;
    font-size: 0.82rem;
  }
  .facts dt {
    color: var(--color-text-muted);
  }
  .facts dd {
    margin: 0;
    min-width: 0;
  }
  .facts .path {
    overflow-wrap: anywhere;
  }
  .detail {
    font-size: 0.9rem;
    gap: 0.35rem 1rem;
  }
  .orgs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.35rem;
  }
  .org {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.5rem 0.6rem;
    border: 1px solid var(--color-border);
    border-radius: 9px;
    background: var(--color-surface-subtle);
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .org:hover {
    border-color: var(--color-accent);
    color: var(--color-accent);
  }
  .org-name {
    font-weight: 600;
  }
  .org-host {
    margin-left: auto;
    color: var(--color-text-muted);
    font-size: 0.78rem;
    overflow-wrap: anywhere;
  }
  .default-tag {
    margin-left: 0.4rem;
    padding: 0.05rem 0.35rem;
    border-radius: 999px;
    background: var(--color-accent-soft);
    color: var(--color-accent);
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .empty,
  .hint {
    margin: 0;
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .hint {
    margin-top: 0.9rem;
  }
  .credentials-note {
    margin-top: 1.25rem;
    color: var(--color-text-muted);
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }
  code {
    font-size: 0.8em;
  }
</style>
