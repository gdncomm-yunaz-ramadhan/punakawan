<script lang="ts">
  import {
    evidencePreviewUrl,
    getEvidenceTextPreview,
    isBinaryEvidence,
    type EvidenceRecord,
    type EvidenceTextPreview,
  } from "../api/client";

  interface Props {
    workspaceId: string;
    record: EvidenceRecord;
  }
  let { workspaceId, record }: Props = $props();

  let expanded = $state(false);
  let preview: EvidenceTextPreview | null = $state(null);
  let error: string | null = $state(null);
  let loading = $state(false);

  const PAGE_BYTES = 8192;

  async function loadMore() {
    loading = true;
    error = null;
    try {
      const offset = preview ? preview.offset + preview.text.length : 0;
      const next = await getEvidenceTextPreview(workspaceId, record.id, { offset, limit: PAGE_BYTES });
      preview = preview ? { ...next, text: preview.text + next.text } : next;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function toggle() {
    expanded = !expanded;
    if (expanded && !preview && !isBinaryEvidence(record.type)) {
      await loadMore();
    }
  }
</script>

<li class="row">
  <button type="button" class="row-head" onclick={toggle}>
    <span class="type">{record.type}</span>
    {#if record.summary}<span class="summary">{record.summary}</span>{/if}
    <span class="created">{record.created_at}</span>
  </button>

  {#if expanded}
    {#if isBinaryEvidence(record.type)}
      <img class="screenshot" src={evidencePreviewUrl(workspaceId, record.id)} alt={record.summary ?? record.type} />
    {:else if error}
      <p role="alert" class="error">Failed to load preview: {error}</p>
    {:else if preview}
      {#if preview.diff_summary}
        <p class="diff-summary">
          {preview.diff_summary.files_changed} file(s) changed, +{preview.diff_summary.insertions} -{preview
            .diff_summary.deletions}
          {#if preview.diff_summary.truncated}(counted from a truncated prefix){/if}
        </p>
      {/if}
      <pre class="text">{preview.text}</pre>
      {#if preview.truncated}
        <button type="button" class="load-more" onclick={loadMore} disabled={loading}>
          {loading ? "Loading…" : "Load more"}
        </button>
      {/if}
      <p class="size">{preview.offset + preview.text.length} / {preview.total_size} bytes shown</p>
    {:else if loading}
      <p>Loading…</p>
    {/if}
  {/if}
</li>

<style>
  .row {
    border: 1px solid #eee;
    border-radius: 6px;
    padding: 0.4rem 0.6rem;
  }
  .row-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    cursor: pointer;
    font: inherit;
    padding: 0;
  }
  .type {
    font-size: 0.75rem;
    color: #666;
    text-transform: uppercase;
  }
  .summary {
    flex: 1;
    font-size: 0.85rem;
  }
  .created {
    font-size: 0.75rem;
    color: #888;
  }
  .text {
    margin: 0.4rem 0 0;
    font-size: 0.75rem;
    background: #f7f7f7;
    padding: 0.5rem;
    border-radius: 4px;
    overflow-x: auto;
    white-space: pre-wrap;
    max-height: 20rem;
    overflow-y: auto;
  }
  .diff-summary {
    margin: 0.4rem 0 0;
    font-size: 0.8rem;
    color: #3949ab;
  }
  .size {
    margin: 0.2rem 0 0;
    font-size: 0.7rem;
    color: #999;
  }
  .load-more {
    margin-top: 0.3rem;
    font-size: 0.78rem;
    padding: 0.25rem 0.6rem;
    border: 1px solid #3949ab;
    background: white;
    color: #3949ab;
    border-radius: 6px;
    cursor: pointer;
  }
  .screenshot {
    margin-top: 0.4rem;
    max-width: 100%;
    border: 1px solid #ddd;
    border-radius: 4px;
  }
  .error {
    color: #b00020;
    font-size: 0.8rem;
  }
</style>
