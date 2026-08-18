<script lang="ts">
  import { onMount } from "svelte";
  import {
    listWorkflows,
    enableWorkflow,
    disableWorkflow,
    createWorkflow,
    ApiError,
    type WorkflowDefinition,
    type WorkflowErrorCode,
  } from "../../lib/api/client";
  import { roleLabel, stepRole, ROLE_STEP_VERB } from "../../lib/roles";
  import StatusBadge from "../../lib/components/StatusBadge.svelte";
  import Button from "../../lib/components/Button.svelte";
  import Icon from "../../lib/components/Icon.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let workflows: WorkflowDefinition[] = $state([]);
  let loading = $state(true);
  let error: string | null = $state(null);

  // Workflow whose detail modal is open (only one at a time).
  let selectedId: string | null = $state(null);
  let selectedWf = $derived(workflows.find((w) => w.id === selectedId) ?? null);

  // Per-workflow enable/disable in-flight guard + error, keyed by id.
  let toggling: Record<string, boolean> = $state({});
  let toggleError: Record<string, string> = $state({});

  // Definition editor state for the workflow currently open in the modal.
  let definitionText = $state("");
  let saving = $state(false);
  let saveError: string | null = $state(null);

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
    definitionText = JSON.stringify(wf, null, 2);
    saveError = null;
  }

  function closeDetail() {
    selectedId = null;
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

  // Whole-definition editor: unlike the old per-input parseValue helper,
  // invalid JSON here is a validation error shown to the user, not a
  // silent fallback to a plain string - there's no sensible non-JSON form
  // of an entire workflow definition.
  async function saveDefinition(wf: WorkflowDefinition) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(definitionText);
    } catch (e) {
      saveError = `Invalid JSON: ${e instanceof Error ? e.message : String(e)}`;
      return;
    }
    saving = true;
    saveError = null;
    try {
      const updated = await createWorkflow(projectId, parsed);
      replaceInList(updated);
      // Reflect the saved (and possibly server-normalized) definition and
      // its bumped revision back into the modal.
      definitionText = JSON.stringify(updated, null, 2);
    } catch (e) {
      if (e instanceof ApiError) {
        const code = e.code as WorkflowErrorCode | undefined;
        if (code === "revision_conflict") {
          saveError =
            "Someone else changed this workflow since you opened it. Close and reopen it to reload the latest version, then try saving again.";
        } else if (code === "unknown_capability" || code === "command_not_allowed" || code === "invalid") {
          saveError = `This definition is invalid: ${e.message}`;
        } else {
          saveError = e.message;
        }
      } else {
        saveError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      saving = false;
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
            <span data-testid={`toggle-${wf.id}`}>
              <Button
                variant="secondary"
                size="sm"
                onclick={(e) => {
                  e.stopPropagation();
                  toggle(wf);
                }}
                disabled={toggling[wf.id]}
                ariaLabel={`${wf.enabled ? "Disable" : "Enable"} ${wf.name || wf.id}`}
              >
                {#if toggling[wf.id]}…{:else}{wf.enabled ? "Disable" : "Enable"}{/if}
              </Button>
            </span>
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

  {/if}
</section>

<Dialog
  open={selectedWf !== null}
  title={selectedWf ? selectedWf.name || selectedWf.id : undefined}
  onclose={closeDetail}
  size="lg"
>
  {#if selectedWf}
    {@const wf = selectedWf}
    <div class="detail" data-testid="workflow-detail">
      <div class="detail-meta">
        <code>{wf.id}</code>
        {#if wf.enabled}
          <StatusBadge variant="success" label="Enabled" />
        {:else}
          <StatusBadge variant="neutral" label="Disabled" />
        {/if}
        <span class="version">v{wf.version}</span>
      </div>
      {#if wf.description}<p class="description">{wf.description}</p>{/if}

      <div class="workflow-canvas" aria-label={`${wf.name || wf.id} workflow steps`}>
        <div class="canvas-label">
          <span><Icon name="activity" size={14} /> Execution path</span>
          <span>{wf.steps.length} steps</span>
        </div>
        <ol class="stepper" data-testid="workflow-stepper">
          <li class="step trigger">
            <div class="step-rail">
              <span class="step-marker"><Icon name="activity" size={15} /></span>
              <span class="step-line" aria-hidden="true"></span>
            </div>
            <div class="step-content">
              <small>TRIGGER</small>
              <strong>Invoke</strong>
            </div>
          </li>
          {#each wf.steps as step, index (step.id)}
            {@const kind = stepKind(step.capability)}
            <li class="step" class:approval={kind === "approval"} class:code={kind === "code"}>
              <div class="step-rail">
                <span class="step-marker">
                  <Icon
                    name={kind === "approval" ? "approval" : kind === "code" ? "code" : "settings"}
                    size={15}
                  />
                </span>
                <span class="step-line" aria-hidden="true"></span>
              </div>
              <div class="step-content">
                <small>STEP {index + 1}</small>
                <strong>{step.intent || step.id}</strong>
                <code>{step.capability}</code>
                {#if stepRole(step.capability)}
                  {@const owner = stepRole(step.capability)!}
                  <span class="step-role">{ROLE_STEP_VERB[owner]} {roleLabel(owner)}</span>
                {/if}
              </div>
            </li>
          {/each}
          <li class="step finish">
            <div class="step-rail">
              <span class="step-marker"><Icon name="check" size={15} /></span>
            </div>
            <div class="step-content">
              <small>OUTPUT</small>
              <strong>Complete</strong>
            </div>
          </li>
        </ol>
      </div>

      <dl class="meta-list">
        <dt>Workflow ID</dt>
        <dd><code>{wf.id}</code></dd>
        {#if wf.required_metadata?.length}
          <dt>Required metadata</dt>
          <dd>{wf.required_metadata.join(", ")}</dd>
        {/if}
        {#if wf.allowed_capabilities?.length}
          <dt>Allowed capabilities</dt>
          <dd>{wf.allowed_capabilities.join(", ")}</dd>
        {/if}
      </dl>

      <div class="editor-panel">
        <div>
          <span class="eyebrow">Definition</span>
          <h4>Edit workflow definition</h4>
          <p>Edit the raw JSON below, then save to update this workflow in place.</p>
        </div>
        <form
          class="editor-form"
          onsubmit={(e) => {
            e.preventDefault();
            saveDefinition(wf);
          }}
        >
          <textarea
            class="definition-input"
            bind:value={definitionText}
            aria-label={`${wf.name || wf.id} definition JSON`}
            data-testid="definition-textarea"
            rows="14"
            wrap="off"
            spellcheck="false"
          ></textarea>
          <span data-testid="save-definition-btn">
            <Button type="submit" variant="primary" icon="check" disabled={saving}>
              {saving ? "Saving…" : "Save definition"}
            </Button>
          </span>
        </form>
        {#if saveError}
          <p class="save-error" role="alert" data-testid="save-definition-error">{saveError}</p>
        {/if}
      </div>
    </div>
  {/if}
</Dialog>

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
  .detail-meta {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    flex-wrap: wrap;
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
    padding: 0.85rem 0.9rem;
    border: 1px solid var(--color-border);
    border-radius: 12px;
    background: var(--color-surface-subtle);
  }
  .canvas-label {
    display: flex;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 0.7rem;
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
  ol.stepper {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .step {
    display: flex;
    gap: 0.7rem;
  }
  .step-rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex: 0 0 auto;
  }
  .step-marker {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: 0 0 auto;
    border-radius: 50%;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    box-shadow: 0 0 0 1px var(--color-border-strong);
  }
  .step-line {
    width: 2px;
    flex: 1;
    min-height: 0.6rem;
    margin: 0.2rem 0;
    background: var(--color-border-strong);
  }
  .step-content {
    flex: 1;
    min-width: 0;
    padding: 0.15rem 0 1rem 0.75rem;
    border-left: 3px solid var(--color-border-strong);
    display: grid;
    gap: 0.12rem;
  }
  .step:last-child .step-content {
    padding-bottom: 0.1rem;
    border-left-color: transparent;
  }
  .step.trigger .step-marker {
    color: #cf4d17;
    background: color-mix(in srgb, #ff6d2e 14%, var(--color-surface));
  }
  .step.trigger .step-content {
    border-left-color: color-mix(in srgb, #ff6d2e 55%, var(--color-border));
  }
  .step.approval .step-marker {
    color: var(--color-warning);
    background: color-mix(in srgb, var(--color-warning) 14%, var(--color-surface));
  }
  .step.approval .step-content {
    border-left-color: color-mix(in srgb, var(--color-warning) 55%, var(--color-border));
  }
  .step.finish .step-marker {
    color: var(--color-success);
    background: color-mix(in srgb, var(--color-success) 12%, var(--color-surface));
  }
  .step.finish .step-content {
    border-left-color: color-mix(in srgb, var(--color-success) 55%, var(--color-border));
  }
  .step-content small {
    color: var(--color-text-muted);
    font-size: 0.6rem;
    font-weight: 750;
    letter-spacing: 0.07em;
  }
  .step-content strong {
    font-size: 0.85rem;
  }
  .step-content code {
    color: var(--color-text-muted);
    font-size: 0.7rem;
    word-break: break-all;
  }
  .step-role {
    margin-top: 0.1rem;
    font-size: 0.68rem;
    font-weight: 600;
    color: var(--color-accent, var(--color-text));
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
  .editor-panel {
    display: grid;
    gap: 0.6rem;
    margin-top: 0.8rem;
    padding: 0.85rem;
    border: 1px solid var(--color-border);
    border-radius: 10px;
    background: linear-gradient(135deg, var(--color-surface-subtle), var(--color-surface));
  }
  .editor-panel h4 {
    margin: 0.12rem 0 0;
    font-size: 0.9rem;
  }
  .editor-panel > div > p {
    margin: 0.25rem 0 0;
    color: var(--color-text-muted);
    font-size: 0.75rem;
    line-height: 1.45;
  }
  .editor-form {
    display: grid;
    gap: 0.6rem;
  }
  .definition-input {
    font: 0.78rem/1.4 ui-monospace, SFMono-Regular, Menlo, monospace;
    padding: 0.6rem 0.7rem;
    border: 1px solid var(--color-border-strong);
    border-radius: var(--radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    overflow-x: auto;
  }
  .definition-input:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 1px;
  }
  .save-error {
    color: var(--color-danger);
    font-size: 0.85rem;
    margin: 0;
  }

  @media (max-width: 760px) {
    .workflow-toolbar {
      align-items: flex-start;
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
