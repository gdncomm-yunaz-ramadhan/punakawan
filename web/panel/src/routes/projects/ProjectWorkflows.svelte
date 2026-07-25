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
  import Icon from "../../lib/components/Icon.svelte";
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

  function stepKind(capability: string) {
    if (capability.includes("approval") || capability.includes("review")) return "approval";
    if (capability.includes("git") || capability.includes("code")) return "code";
    return "action";
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
    <header class="workflow-toolbar">
      <div>
        <span class="eyebrow">Automation library</span>
        <h3>{workflows.length} workflow{workflows.length === 1 ? "" : "s"}</h3>
        <p>Select a workflow to inspect its execution path and run configuration.</p>
      </div>
      <div class="toolbar-stat">
        <span class="live-dot"></span>
        {workflows.filter((wf) => wf.enabled).length} active
      </div>
    </header>

    <div class="workflow-grid">
      {#each workflows as wf (wf.id)}
        <div
          class="workflow-card"
          class:selected={selectedId === wf.id}
          role="button"
          tabindex="0"
          aria-expanded={selectedId === wf.id}
          onclick={() => select(wf)}
          onkeydown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), select(wf))}
          data-testid={`workflow-row-${wf.id}`}
        >
          <div class="workflow-card-head">
            <span class="workflow-mark"><Icon name="git-branch" size={19} /></span>
            <div class="workflow-title">
              <strong>{wf.name || wf.id}</strong>
              <code>{wf.id}</code>
            </div>
            {#if wf.enabled}
              <StatusBadge variant="success" label="Enabled" />
            {:else}
              <StatusBadge variant="neutral" label="Disabled" />
            {/if}
          </div>
          <p>{wf.description || "Declarative automation workflow"}</p>
          <div class="workflow-stats">
            <span><Icon name="list" size={14} /> {wf.steps?.length ?? 0} steps</span>
            <span><Icon name="database" size={14} /> {wf.required_metadata?.length ?? 0} metadata</span>
          </div>
          <div class="workflow-actions">
            <span class="inspect">Open canvas <span aria-hidden="true">→</span></span>
            <button
              type="button"
              class="btn compact"
              onclick={(e) => {
                e.stopPropagation();
                toggle(wf);
              }}
              disabled={toggling[wf.id]}
              data-testid={`toggle-${wf.id}`}
              aria-label={`${wf.enabled ? "Disable" : "Enable"} ${wf.name || wf.id}`}
            >
              {#if toggling[wf.id]}…{:else}{wf.enabled ? "Disable" : "Enable"}{/if}
            </button>
          </div>
        </div>
      {/each}
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
            <div class="detail-title">
              <span class="workflow-mark large"><Icon name="git-branch" size={20} /></span>
              <div>
                <span class="eyebrow">Workflow canvas</span>
                <h3>{wf.name || wf.id}</h3>
              </div>
            </div>
            <div class="detail-actions">
              {#if wf.enabled}
                <StatusBadge variant="success" label="Enabled" />
              {:else}
                <StatusBadge variant="neutral" label="Disabled" />
              {/if}
              <span class="version">v{wf.version}</span>
            </div>
          </header>
          {#if wf.description}<p class="description">{wf.description}</p>{/if}

          <div class="workflow-canvas" aria-label={`${wf.name || wf.id} workflow graph`}>
            <div class="canvas-label">
              <span><Icon name="activity" size={14} /> Execution path</span>
              <span>{wf.steps.length} nodes</span>
            </div>
            <div class="node-track">
              <div class="flow-node trigger">
                <span class="node-icon"><Icon name="activity" size={18} /></span>
                <div><small>TRIGGER</small><strong>Invoke</strong></div>
                <span class="port output"></span>
              </div>
              {#each wf.steps as step, index (step.id)}
                <span class="connector" aria-hidden="true"><span></span></span>
                <div class="flow-node" class:approval={stepKind(step.capability) === "approval"}>
                  <span class="port input"></span>
                  <span class="node-icon">
                    <Icon
                      name={stepKind(step.capability) === "approval"
                        ? "approval"
                        : stepKind(step.capability) === "code"
                          ? "code"
                          : "settings"}
                      size={18}
                    />
                  </span>
                  <div>
                    <small>STEP {index + 1}</small>
                    <strong>{step.intent || step.id}</strong>
                    <code>{step.capability}</code>
                  </div>
                  <span class="port output"></span>
                </div>
              {/each}
              <span class="connector" aria-hidden="true"><span></span></span>
              <div class="flow-node finish">
                <span class="port input"></span>
                <span class="node-icon"><Icon name="check" size={18} /></span>
                <div><small>OUTPUT</small><strong>{wf.output?.type || "Complete"}</strong></div>
              </div>
            </div>
          </div>

          <dl class="meta-list">
            <dt>Workflow ID</dt>
            <dd><code>{wf.id}</code></dd>
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

          <div class="invoke-panel">
            <div>
              <span class="eyebrow">Manual trigger</span>
              <h4>Run this workflow</h4>
              <p>Provide the execution inputs, then send the workflow to the run engine.</p>
            </div>
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
                <Icon name="activity" size={15} />
                {invoking ? "Invoking…" : "Execute workflow"}
              </button>
            </form>
          </div>

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
  section {
    display: grid;
    gap: 1rem;
  }
  .workflow-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 1rem;
  }
  .workflow-toolbar h3 {
    margin: 0.15rem 0 0;
    font-size: 1rem;
  }
  .workflow-toolbar p {
    margin: 0.25rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.82rem;
  }
  .eyebrow {
    color: var(--color-text-muted);
    font-size: 0.68rem;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .toolbar-stat {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    color: var(--color-text-muted);
    font-size: 0.76rem;
    font-weight: 650;
    white-space: nowrap;
  }
  .live-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-success);
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-success) 14%, transparent);
  }
  .workflow-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 0.75rem;
  }
  .workflow-card {
    min-width: 0;
    padding: 0.9rem;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: linear-gradient(145deg, var(--color-surface-raised), var(--color-surface));
    box-shadow: var(--shadow-card);
    cursor: pointer;
  }
  .workflow-card:hover {
    border-color: color-mix(in srgb, var(--color-accent) 45%, var(--color-border));
    box-shadow: var(--shadow-card-hover);
  }
  .workflow-card.selected {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 2px var(--color-accent-soft), var(--shadow-card);
  }
  .workflow-card:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .workflow-card-head {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }
  .workflow-mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    flex: 0 0 auto;
    border-radius: 10px;
    color: #cf4d17;
    background: color-mix(in srgb, #ff6d2e 14%, var(--color-surface));
    border: 1px solid color-mix(in srgb, #ff6d2e 30%, var(--color-border));
  }
  .workflow-mark.large {
    width: 40px;
    height: 40px;
  }
  .workflow-title {
    min-width: 0;
    display: grid;
    gap: 0.12rem;
    flex: 1;
  }
  .workflow-title strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.92rem;
  }
  .workflow-title code {
    color: var(--color-text-muted);
    font-size: 0.68rem;
  }
  .workflow-card > p {
    min-height: 2.4em;
    margin: 0.65rem 0;
    color: var(--color-text-muted);
    font-size: 0.78rem;
    line-height: 1.5;
  }
  .workflow-stats,
  .workflow-actions {
    display: flex;
    align-items: center;
    gap: 0.8rem;
  }
  .workflow-stats {
    color: var(--color-text-muted);
    font-size: 0.73rem;
  }
  .workflow-stats span {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
  }
  .workflow-actions {
    justify-content: space-between;
    padding-top: 0.7rem;
    margin-top: 0.7rem;
    border-top: 1px solid var(--color-border);
  }
  .inspect {
    color: var(--color-accent);
    font-size: 0.75rem;
    font-weight: 700;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0;
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
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
  }
  .btn.compact {
    min-height: 32px;
    padding: 0.25rem 0.65rem;
    font-size: 0.72rem;
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
    padding: 1rem;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-card);
  }
  .detail-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    flex-wrap: wrap;
  }
  .detail-title,
  .detail-actions {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }
  .detail-head h3 {
    margin: 0.12rem 0 0;
    font-size: 1.05rem;
  }
  .version {
    color: var(--color-text-muted);
    font: 600 0.72rem ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .description {
    margin: 0.55rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.9rem;
  }
  .workflow-canvas {
    min-width: 0;
    margin-top: 0.9rem;
    padding: 0.75rem;
    overflow: auto;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background-color: color-mix(in srgb, var(--color-surface-subtle) 85%, var(--color-surface));
    background-image: radial-gradient(circle, color-mix(in srgb, var(--color-text-muted) 28%, transparent) 1px, transparent 1px);
    background-size: 18px 18px;
  }
  .canvas-label {
    position: sticky;
    left: 0;
    display: flex;
    justify-content: space-between;
    gap: 1rem;
    color: var(--color-text-muted);
    font-size: 0.68rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .canvas-label span {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
  }
  .node-track {
    min-width: max-content;
    display: flex;
    align-items: center;
    padding: 1.6rem 0.65rem 1.2rem;
  }
  .flow-node {
    position: relative;
    width: 164px;
    min-height: 74px;
    display: flex;
    align-items: flex-start;
    gap: 0.55rem;
    padding: 0.72rem;
    border: 1px solid var(--color-border-strong);
    border-radius: 9px;
    background: var(--color-surface-raised);
    box-shadow: 0 4px 14px color-mix(in srgb, var(--color-text) 9%, transparent);
  }
  .flow-node.trigger {
    border-color: color-mix(in srgb, #ff6d2e 55%, var(--color-border));
  }
  .flow-node.approval {
    border-color: color-mix(in srgb, var(--color-warning) 55%, var(--color-border));
  }
  .flow-node.finish {
    border-color: color-mix(in srgb, var(--color-success) 55%, var(--color-border));
  }
  .node-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    flex: 0 0 auto;
    border-radius: 8px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .trigger .node-icon {
    color: #cf4d17;
    background: color-mix(in srgb, #ff6d2e 14%, var(--color-surface));
  }
  .finish .node-icon {
    color: var(--color-success);
    background: color-mix(in srgb, var(--color-success) 12%, var(--color-surface));
  }
  .flow-node > div {
    min-width: 0;
    display: grid;
    gap: 0.15rem;
  }
  .flow-node small {
    color: var(--color-text-muted);
    font-size: 0.58rem;
    font-weight: 750;
    letter-spacing: 0.07em;
  }
  .flow-node strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.78rem;
  }
  .flow-node code {
    overflow: hidden;
    color: var(--color-text-muted);
    font-size: 0.62rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .port {
    position: absolute;
    top: calc(50% - 5px);
    width: 8px;
    height: 8px;
    border: 2px solid var(--color-surface-raised);
    border-radius: 50%;
    background: var(--color-border-strong);
  }
  .port.input {
    left: -5px;
  }
  .port.output {
    right: -5px;
  }
  .connector {
    width: 38px;
    height: 14px;
    display: inline-flex;
    align-items: center;
  }
  .connector span {
    position: relative;
    width: 100%;
    height: 2px;
    background: var(--color-border-strong);
  }
  .connector span::after {
    content: "";
    position: absolute;
    right: -1px;
    top: -3px;
    width: 6px;
    height: 6px;
    border-top: 2px solid var(--color-border-strong);
    border-right: 2px solid var(--color-border-strong);
    transform: rotate(45deg);
  }
  dl.meta-list {
    margin: 0.8rem 0 0;
    padding: 0.75rem;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
    border-radius: 9px;
    background: var(--color-surface-subtle);
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
  }
  .invoke-panel {
    display: grid;
    grid-template-columns: minmax(180px, 0.65fr) minmax(280px, 1.35fr);
    gap: 1rem;
    margin-top: 0.8rem;
    padding: 0.85rem;
    border: 1px solid var(--color-border);
    border-radius: 10px;
    background: linear-gradient(135deg, var(--color-surface-subtle), var(--color-surface));
  }
  .invoke-panel h4 {
    margin: 0.12rem 0 0;
    font-size: 0.9rem;
  }
  .invoke-panel > div > p {
    margin: 0.25rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.75rem;
    line-height: 1.45;
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

  @media (max-width: 760px) {
    .workflow-toolbar,
    .detail-head {
      align-items: flex-start;
    }
    .invoke-panel {
      grid-template-columns: 1fr;
    }
  }

  @media (prefers-reduced-motion: no-preference) {
    .workflow-card {
      transition: border-color 140ms ease, box-shadow 140ms ease, transform 140ms ease;
    }
    .workflow-card:hover {
      transform: translateY(-1px);
    }
  }
</style>
