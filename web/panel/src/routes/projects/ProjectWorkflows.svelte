<script lang="ts">
  import { onMount } from "svelte";
  import {
    listWorkflows,
    enableWorkflow,
    disableWorkflow,
    invokeWorkflow,
    ApiError,
    type WorkflowDefinition,
  } from "../../lib/api/client";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let workflows: WorkflowDefinition[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // Expanded row (only one detail open at a time).
  let selectedId: string | null = $state(null);

  // Per-workflow enable/disable in-flight guard + error, keyed by id.
  let toggling: Record<string, boolean> = $state({});
  let toggleError: Record<string, string> = $state({});

  // Invoke form state for the currently-selected workflow.
  let invokeInputs: Record<string, string> = $state({});
  let invoking = $state(false);
  // The invoke outcome: either a queued run_id, or the surfaced backend
  // message (e.g. the "not connected to the run engine" not-yet-wired
  // state, or a 409 "workflow disabled").
  let invokeRunId: string | null = $state(null);
  let invokeError: string | null = $state(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await listWorkflows(projectId);
      workflows = res.items;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function select(wf: WorkflowDefinition) {
    if (selectedId === wf.id) {
      selectedId = null;
      return;
    }
    selectedId = wf.id;
    // Seed the invoke form with each input's default (as text) and reset
    // the last invoke result.
    invokeInputs = {};
    for (const inp of wf.inputs ?? []) {
      invokeInputs[inp.name] = inp.default === undefined ? "" : String(inp.default);
    }
    invokeRunId = null;
    invokeError = null;
  }

  function replaceInList(updated: WorkflowDefinition) {
    workflows = workflows.map((w) => (w.id === updated.id ? updated : w));
  }

  async function toggle(wf: WorkflowDefinition) {
    toggling = { ...toggling, [wf.id]: true };
    toggleError = { ...toggleError, [wf.id]: "" };
    try {
      const updated = wf.enabled
        ? await disableWorkflow(projectId, wf.id)
        : await enableWorkflow(projectId, wf.id);
      replaceInList(updated);
    } catch (e) {
      toggleError = { ...toggleError, [wf.id]: e instanceof Error ? e.message : String(e) };
    } finally {
      toggling = { ...toggling, [wf.id]: false };
    }
  }

  // Parse a text input into a value: valid JSON becomes its parsed form,
  // anything else stays a plain string (mirrors ProjectMetadata).
  function parseValue(text: string): unknown {
    const trimmed = text.trim();
    if (trimmed === "") return "";
    try {
      return JSON.parse(trimmed);
    } catch {
      return text;
    }
  }

  async function invoke(wf: WorkflowDefinition) {
    invoking = true;
    invokeRunId = null;
    invokeError = null;
    const inputs: Record<string, unknown> = {};
    for (const inp of wf.inputs ?? []) {
      inputs[inp.name] = parseValue(invokeInputs[inp.name] ?? "");
    }
    try {
      const res = await invokeWorkflow(projectId, wf.id, inputs);
      invokeRunId = res.run_id;
    } catch (e) {
      if (e instanceof ApiError && e.code === "disabled") {
        invokeError = "This workflow is disabled — enable it before invoking.";
      } else {
        // Includes the not-yet-wired "not connected to the run engine"
        // message: surface it verbatim rather than hiding the action.
        invokeError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      invoking = false;
    }
  }
</script>

<section aria-label="Project workflows">
  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <ErrorStateCard title="Failed to load workflows" message={error} />
  {:else if workflows.length === 0}
    <EmptyStateCard
      title="No workflows yet"
      message="This project has no workflow definitions."
    />
  {:else}
    <div class="table-scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Status</th>
            <th scope="col">Steps</th>
            <th scope="col">Required metadata</th>
            <th scope="col"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each workflows as wf (wf.id)}
            <tr
              class="row"
              class:selected={selectedId === wf.id}
              role="button"
              tabindex="0"
              aria-expanded={selectedId === wf.id}
              onclick={() => select(wf)}
              onkeydown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(wf))}
              data-testid={`workflow-row-${wf.id}`}
            >
              <td class="name">{wf.name || wf.id}</td>
              <td>
                {#if wf.enabled}
                  <StatusBadge variant="success" label="Enabled" />
                {:else}
                  <StatusBadge variant="neutral" label="Disabled" />
                {/if}
              </td>
              <td class="steps">{wf.steps?.length ?? 0}</td>
              <td class="meta">{wf.required_metadata?.length ? wf.required_metadata.join(", ") : "—"}</td>
              <td class="actions">
                <button
                  type="button"
                  class="btn"
                  onclick={(e) => {
                    e.stopPropagation();
                    toggle(wf);
                  }}
                  disabled={toggling[wf.id]}
                  data-testid={`toggle-${wf.id}`}
                  aria-label={`${wf.enabled ? "Disable" : "Enable"} ${wf.name || wf.id}`}
                >
                  {#if toggling[wf.id]}
                    …
                  {:else}
                    {wf.enabled ? "Disable" : "Enable"}
                  {/if}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    {#each workflows as wf (wf.id)}
      {#if toggleError[wf.id]}
        <p class="error" role="alert" data-testid={`toggle-error-${wf.id}`}>
          {wf.name || wf.id}: {toggleError[wf.id]}
        </p>
      {/if}
    {/each}

    {#if selectedId}
      {@const wf = workflows.find((w) => w.id === selectedId)}
      {#if wf}
        <div class="detail" data-testid="workflow-detail">
          <header class="detail-head">
            <h3>{wf.name || wf.id}</h3>
            {#if wf.enabled}
              <StatusBadge variant="success" label="Enabled" />
            {:else}
              <StatusBadge variant="neutral" label="Disabled" />
            {/if}
          </header>
          {#if wf.description}<p class="description">{wf.description}</p>{/if}

          <dl class="meta-list">
            <dt>Workflow ID</dt>
            <dd><code>{wf.id}</code></dd>
            <dt>Version</dt>
            <dd>{wf.version}</dd>
            {#if wf.output?.type}
              <dt>Output</dt>
              <dd>{wf.output.type}</dd>
            {/if}
            {#if wf.required_metadata?.length}
              <dt>Required metadata</dt>
              <dd>{wf.required_metadata.join(", ")}</dd>
            {/if}
            {#if wf.allowed_capabilities?.length}
              <dt>Allowed capabilities</dt>
              <dd>{wf.allowed_capabilities.join(", ")}</dd>
            {/if}
            {#if wf.approval?.required_for?.length}
              <dt>Approval required for</dt>
              <dd>{wf.approval.required_for.join(", ")}</dd>
            {/if}
          </dl>

          <h4>Steps</h4>
          <ol class="steps-list">
            {#each wf.steps as step (step.id)}
              <li>
                <code>{step.capability}</code>
                {#if step.intent}<span class="intent"> — {step.intent}</span>{/if}
                {#if step.input_from?.length}
                  <span class="input-from">(from: {step.input_from.join(", ")})</span>
                {/if}
              </li>
            {/each}
          </ol>

          <h4>Invoke</h4>
          <form
            class="invoke-form"
            onsubmit={(e) => {
              e.preventDefault();
              invoke(wf);
            }}
          >
            {#if wf.inputs?.length}
              <div class="fields">
                {#each wf.inputs as inp (inp.name)}
                  <label>
                    <span>{inp.name}{inp.required ? " *" : ""} <em>({inp.type})</em></span>
                    <input
                      type="text"
                      bind:value={invokeInputs[inp.name]}
                      aria-label={`Workflow input ${inp.name}`}
                    />
                  </label>
                {/each}
              </div>
            {:else}
              <p class="no-inputs">This workflow takes no inputs.</p>
            {/if}
            <button type="submit" class="btn primary" disabled={invoking} data-testid="invoke-btn">
              {invoking ? "Invoking…" : "Invoke"}
            </button>
          </form>

          {#if invokeRunId}
            <p class="invoke-ok" role="status" data-testid="invoke-run-id">
              Run queued: <code>{invokeRunId}</code>
            </p>
          {/if}
          {#if invokeError}
            <p class="invoke-warn" role="alert" data-testid="invoke-error">{invokeError}</p>
          {/if}
        </div>
      {/if}
    {/if}
  {/if}
</section>

<style>
  .table-scroll {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }
  th,
  td {
    text-align: left;
    padding: 0.55rem 0.65rem;
    border-bottom: 1px solid var(--color-border);
    vertical-align: top;
  }
  th {
    color: var(--color-text-muted);
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  tr.row {
    cursor: pointer;
  }
  tr.row:hover {
    background: var(--color-surface-subtle);
  }
  tr.row.selected {
    background: var(--color-accent-soft);
  }
  tr.row:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: -2px;
  }
  td.name {
    font-weight: 600;
  }
  td.steps {
    text-align: center;
  }
  td.meta {
    color: var(--color-text-muted);
    word-break: break-word;
  }
  td.actions {
    white-space: nowrap;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0.5rem 0 0;
  }
  .btn {
    font: inherit;
    font-weight: 600;
    font-size: 0.82rem;
    padding: 0.4rem 0.8rem;
    min-height: 40px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--color-border-strong);
    background: var(--color-surface);
    color: var(--color-text);
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    border-color: var(--color-accent);
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .btn.primary {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-accent-contrast);
  }
  .detail {
    margin-top: 1rem;
    padding: 1rem 1.1rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
  }
  .detail-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .detail-head h3 {
    margin: 0;
    font-size: 1.05rem;
  }
  .description {
    margin: 0.4rem 0 0;
    color: var(--color-text);
    font-size: 0.9rem;
  }
  dl.meta-list {
    margin: 0.9rem 0 0;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
  }
  dl.meta-list dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  dl.meta-list dd {
    margin: 0;
    word-break: break-word;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
  }
  h4 {
    margin: 1rem 0 0.4rem;
    font-size: 0.9rem;
  }
  ol.steps-list {
    margin: 0;
    padding-left: 1.2rem;
    display: grid;
    gap: 0.3rem;
    font-size: 0.85rem;
  }
  .intent {
    color: var(--color-text);
  }
  .input-from {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    margin-left: 0.3rem;
  }
  .invoke-form {
    display: flex;
    align-items: flex-end;
    gap: 0.75rem;
    flex-wrap: wrap;
  }
  .fields {
    display: flex;
    gap: 0.75rem;
    flex-wrap: wrap;
    flex: 1;
  }
  label {
    display: grid;
    gap: 0.2rem;
    font-size: 0.78rem;
    color: var(--color-text-muted);
    flex: 1 1 10rem;
  }
  label em {
    font-style: normal;
    opacity: 0.7;
  }
  input {
    font: inherit;
    padding: 0.4rem 0.55rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    min-height: 40px;
    width: 100%;
    box-sizing: border-box;
  }
  input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .no-inputs {
    color: var(--color-text-muted);
    font-size: 0.85rem;
    margin: 0;
  }
  .invoke-ok {
    color: var(--color-success);
    font-size: 0.85rem;
    margin: 0.6rem 0 0;
  }
  .invoke-warn {
    color: var(--color-warning);
    font-size: 0.85rem;
    margin: 0.6rem 0 0;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
