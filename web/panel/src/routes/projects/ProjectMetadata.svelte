<script lang="ts">
  import { onMount } from "svelte";
  import {
    listMetadata,
    addMetadata,
    updateMetadata,
    deleteMetadata,
    ApiError,
    type MetadataEntry,
  } from "../../lib/api/client";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import EmptyStateCard from "../../lib/components/cards/EmptyStateCard.svelte";

  interface Props {
    projectId: string;
  }
  let { projectId }: Props = $props();

  let entries: MetadataEntry[] = $state([]);
  // Optimistic-locking token: every mutation sends this as base_revision;
  // the server bumps it on success (and 409s if it moved underneath us).
  let revision = $state(0);
  let loading = $state(true);
  let error: string | null = $state(null);
  // A banner shown after a 409: the list was reloaded to the latest.
  let conflictNotice: string | null = $state(null);

  // Add form.
  let addKey = $state("");
  let addDescription = $state("");
  let addValueText = $state("");

  // Inline edit state (only one row editable at a time).
  let editingKey: string | null = $state(null);
  let editDescription = $state("");
  let editValueText = $state("");

  // Pending mutation awaiting the confirm dialog (plan §4.3 compact
  // confirm: never commit an edit without showing the old->new diff).
  type Pending =
    | { action: "add"; key: string; oldEntry: null; newEntry: { description: string; value: unknown } }
    | { action: "update"; key: string; oldEntry: MetadataEntry; newEntry: { description: string; value: unknown } }
    | { action: "delete"; key: string; oldEntry: MetadataEntry; newEntry: null };
  let pending: Pending | null = $state(null);
  let busy = $state(false);
  let mutationError: string | null = $state(null);

  // Human messages for the backend's 400 validation codes (plan §5).
  const codeMessages: Record<string, string> = {
    duplicate_key: "A metadata entry with this key already exists.",
    secret_rejected: "This value looks like a secret — secrets are not allowed in project metadata.",
    invalid_value: "This value is not valid for this metadata entry.",
    missing_field: "A required field is missing.",
  };

  // Metadata values are arbitrary JSON. Show strings verbatim; stringify
  // everything else so the raw JSON is visible and re-editable.
  function valueToText(v: unknown): string {
    return typeof v === "string" ? v : JSON.stringify(v);
  }

  // Parse a text input back into a value: valid JSON becomes its parsed
  // form (number/bool/object/array); anything else stays a plain string.
  function parseValue(text: string): unknown {
    const trimmed = text.trim();
    if (trimmed === "") return "";
    try {
      return JSON.parse(trimmed);
    } catch {
      return text;
    }
  }

  async function reload() {
    const res = await listMetadata(projectId);
    entries = res.items;
    revision = res.revision;
  }

  async function load() {
    loading = true;
    error = null;
    try {
      await reload();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  function startAdd() {
    if (!addKey.trim()) return;
    conflictNotice = null;
    mutationError = null;
    pending = {
      action: "add",
      key: addKey.trim(),
      oldEntry: null,
      newEntry: { description: addDescription, value: parseValue(addValueText) },
    };
  }

  function beginEdit(entry: MetadataEntry) {
    editingKey = entry.key;
    editDescription = entry.description;
    editValueText = valueToText(entry.value);
  }

  function cancelEdit() {
    editingKey = null;
  }

  function startUpdate(entry: MetadataEntry) {
    conflictNotice = null;
    mutationError = null;
    pending = {
      action: "update",
      key: entry.key,
      oldEntry: entry,
      newEntry: { description: editDescription, value: parseValue(editValueText) },
    };
  }

  function startDelete(entry: MetadataEntry) {
    conflictNotice = null;
    mutationError = null;
    pending = { action: "delete", key: entry.key, oldEntry: entry, newEntry: null };
  }

  function closeDialog() {
    if (busy) return;
    pending = null;
    mutationError = null;
  }

  async function confirm() {
    if (!pending) return;
    busy = true;
    mutationError = null;
    try {
      if (pending.action === "add") {
        await addMetadata(projectId, {
          key: pending.key,
          description: pending.newEntry.description,
          value: pending.newEntry.value,
          base_revision: revision,
        });
      } else if (pending.action === "update") {
        await updateMetadata(projectId, pending.key, {
          description: pending.newEntry.description,
          value: pending.newEntry.value,
          base_revision: revision,
        });
      } else {
        await deleteMetadata(projectId, pending.key, revision);
      }
      await reload();
      // Reset transient form state on success.
      addKey = "";
      addDescription = "";
      addValueText = "";
      editingKey = null;
      pending = null;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        // Someone else changed this project's metadata since we loaded it.
        // Reload to the latest revision and ask the user to review + retry.
        conflictNotice = "This project's metadata changed since you loaded it — reloaded to the latest. Review and try again.";
        pending = null;
        try {
          await reload();
        } catch {
          /* surfaced as a load error below on next interaction */
        }
      } else if (e instanceof ApiError && e.status === 400) {
        mutationError = codeMessages[e.code ?? ""] ?? e.message;
      } else {
        mutationError = e instanceof Error ? e.message : String(e);
      }
    } finally {
      busy = false;
    }
  }
</script>

<section aria-label="Project metadata">
  {#if conflictNotice}
    <p class="conflict" role="alert" data-testid="conflict-notice">{conflictNotice}</p>
  {/if}

  {#if loading}
    <p>Loading…</p>
  {:else if error}
    <p class="error" role="alert">Failed to load metadata: {error}</p>
  {:else}
    <!-- Add form -->
    <form
      class="add-form"
      onsubmit={(e) => {
        e.preventDefault();
        startAdd();
      }}
    >
      <div class="fields">
        <label>
          <span>Key</span>
          <input type="text" bind:value={addKey} aria-label="New metadata key" placeholder="deploy_target" />
        </label>
        <label>
          <span>Description</span>
          <input
            type="text"
            bind:value={addDescription}
            aria-label="New metadata description"
            placeholder="What this value means"
          />
        </label>
        <label>
          <span>Value</span>
          <input
            type="text"
            bind:value={addValueText}
            aria-label="New metadata value"
            placeholder="production (or JSON)"
          />
        </label>
      </div>
      <button type="submit" class="btn primary" disabled={!addKey.trim()}>Add</button>
    </form>

    {#if entries.length === 0}
      <EmptyStateCard title="No metadata yet" message="Add a key/value entry above to describe this project." />
    {:else}
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th scope="col">Key</th>
              <th scope="col">Description</th>
              <th scope="col">Value</th>
              <th scope="col"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {#each entries as entry (entry.key)}
              <tr>
                <td class="key">{entry.key}</td>
                {#if editingKey === entry.key}
                  <td>
                    <input
                      type="text"
                      bind:value={editDescription}
                      aria-label={`Edit description for ${entry.key}`}
                    />
                  </td>
                  <td>
                    <input type="text" bind:value={editValueText} aria-label={`Edit value for ${entry.key}`} />
                  </td>
                  <td class="actions">
                    <button type="button" class="btn primary" onclick={() => startUpdate(entry)}>Save</button>
                    <button type="button" class="btn" onclick={cancelEdit}>Cancel</button>
                  </td>
                {:else}
                  <td class="description">{entry.description}</td>
                  <td class="value">{valueToText(entry.value)}</td>
                  <td class="actions">
                    <button type="button" class="btn" onclick={() => beginEdit(entry)} aria-label={`Edit ${entry.key}`}>
                      Edit
                    </button>
                    <button
                      type="button"
                      class="btn danger"
                      onclick={() => startDelete(entry)}
                      aria-label={`Delete ${entry.key}`}
                    >
                      Delete
                    </button>
                  </td>
                {/if}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</section>

<!-- Compact confirm with the old->new diff (plan §4.3). -->
<Dialog open={pending !== null} title="Confirm change" onclose={closeDialog}>
  {#if pending}
    <div class="confirm">
      <p class="summary">
        {#if pending.action === "add"}
          Add metadata <code>{pending.key}</code>.
        {:else if pending.action === "update"}
          Update metadata <code>{pending.key}</code>.
        {:else}
          Delete metadata <code>{pending.key}</code>.
        {/if}
      </p>

      {#if pending.action !== "delete"}
        <dl class="diff">
          <dt>Description</dt>
          <dd>
            <span class="old">{pending.oldEntry ? pending.oldEntry.description || "(empty)" : "(none)"}</span>
            <span aria-hidden="true" class="arrow">→</span>
            <span class="new">{pending.newEntry.description || "(empty)"}</span>
          </dd>
          <dt>Value</dt>
          <dd>
            <span class="old">{pending.oldEntry ? valueToText(pending.oldEntry.value) : "(none)"}</span>
            <span aria-hidden="true" class="arrow">→</span>
            <span class="new">{valueToText(pending.newEntry.value)}</span>
          </dd>
        </dl>
      {:else}
        <dl class="diff">
          <dt>Value</dt>
          <dd>
            <span class="old">{valueToText(pending.oldEntry.value)}</span>
            <span aria-hidden="true" class="arrow">→</span>
            <span class="new">(removed)</span>
          </dd>
        </dl>
      {/if}

      {#if mutationError}
        <p class="error" role="alert" data-testid="mutation-error">{mutationError}</p>
      {/if}

      <div class="confirm-actions">
        <button type="button" class="btn" onclick={closeDialog} disabled={busy}>Cancel</button>
        <button
          type="button"
          class="btn primary"
          class:danger={pending.action === "delete"}
          onclick={confirm}
          disabled={busy}
        >
          {busy ? "Saving…" : "Confirm"}
        </button>
      </div>
    </div>
  {/if}
</Dialog>

<style>
  .conflict {
    background: color-mix(in srgb, var(--color-warning) 14%, var(--color-surface));
    color: var(--color-warning);
    border: 1px solid color-mix(in srgb, var(--color-warning) 30%, transparent);
    border-radius: var(--radius-sm);
    padding: 0.5rem 0.75rem;
    font-size: 0.85rem;
    margin: 0 0 1rem;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  .add-form {
    display: flex;
    align-items: flex-end;
    gap: 0.75rem;
    flex-wrap: wrap;
    margin-bottom: 1.25rem;
    padding: 0.9rem 1rem;
    background: var(--color-surface-subtle);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
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
  .btn.danger {
    color: var(--color-danger);
    border-color: color-mix(in srgb, var(--color-danger) 40%, transparent);
    background: var(--color-surface);
  }
  .btn.primary.danger {
    background: var(--color-danger);
    border-color: var(--color-danger);
    color: var(--color-accent-contrast);
  }
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
  td.key {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-weight: 600;
    white-space: nowrap;
  }
  td.value {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    word-break: break-word;
  }
  td.description {
    color: var(--color-text-muted);
  }
  td.actions {
    white-space: nowrap;
    display: flex;
    gap: 0.4rem;
  }
  .confirm {
    display: grid;
    gap: 0.85rem;
  }
  .summary {
    margin: 0;
    font-size: 0.9rem;
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    background: var(--color-surface-subtle);
    border-radius: 4px;
    padding: 0.05rem 0.35rem;
  }
  .diff {
    margin: 0;
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 0.35rem 0.85rem;
    align-items: baseline;
    font-size: 0.85rem;
  }
  .diff dt {
    color: var(--color-text-muted);
    font-weight: 600;
  }
  .diff dd {
    margin: 0;
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .old {
    color: var(--color-text-muted);
    text-decoration: line-through;
  }
  .arrow {
    color: var(--color-text-muted);
  }
  .new {
    color: var(--color-text);
    font-weight: 600;
  }
  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
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
