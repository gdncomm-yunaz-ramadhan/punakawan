<script lang="ts">
  import { answerDeliveryQuestion, type AnswerDeliveryQuestionRequest, type DeliveryView } from "../../lib/api/client";
  import Button from "../../lib/components/Button.svelte";

  interface Props {
    orchestrationId: string;
    question: string;
    projectIds: string[];
    revision: number;
    onAnswered: (view: DeliveryView) => void;
  }
  let { orchestrationId, question, projectIds, revision, onAnswered }: Props = $props();

  let mode: "requirement" | "route" = $state("requirement");
  let provider = $state("jira");
  let externalId = $state("");
  let url = $state("");
  let title = $state("");
  let summary = $state("");
  let parentTaskId = $state("");
  let projectId = $state("");
  $effect(() => {
    if (!projectId) projectId = projectIds[0] ?? "";
  });
  let submitting = $state(false);
  let error: string | null = $state(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    submitting = true;
    error = null;
    try {
      const body: AnswerDeliveryQuestionRequest = { reference: question, expected_revision: revision };
      if (mode === "requirement") {
        body.provider = provider;
        body.external_id = externalId;
        if (url) body.url = url;
        if (title) body.title = title;
        if (summary) body.summary = summary;
      } else {
        body.parent_task_id = parentTaskId;
        body.project_id = projectId;
      }
      const view = await answerDeliveryQuestion(orchestrationId, body);
      onAnswered(view);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      submitting = false;
    }
  }
</script>

<form class="question" onsubmit={submit} aria-label={`Answer: ${question}`}>
  <p class="reference">{question}</p>

  <label class="field">
    Resolution
    <select bind:value={mode}>
      <option value="requirement">Link as requirement source</option>
      <option value="route">Route to a task directly</option>
    </select>
  </label>

  {#if mode === "requirement"}
    <div class="grid">
      <label class="field">
        Provider
        <select bind:value={provider}>
          <option value="jira">Jira</option>
          <option value="github">GitHub</option>
          <option value="url">URL</option>
        </select>
      </label>
      <label class="field">
        External ID
        <input type="text" bind:value={externalId} placeholder="PROJ-123" required />
      </label>
      <label class="field">
        URL
        <input type="url" bind:value={url} placeholder="https://…" />
      </label>
      <label class="field">
        Title
        <input type="text" bind:value={title} />
      </label>
    </div>
    <label class="field">
      Summary
      <textarea bind:value={summary} rows="2"></textarea>
    </label>
  {:else}
    <div class="grid">
      <label class="field">
        Project
        <select bind:value={projectId} required>
          {#each projectIds as id (id)}
            <option value={id}>{id}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        Parent task ID
        <input type="text" bind:value={parentTaskId} required />
      </label>
    </div>
  {/if}

  {#if error}
    <p role="alert" class="error">{error}</p>
  {/if}

  <Button type="submit" variant="primary" size="sm" disabled={submitting}>
    {submitting ? "Answering…" : "Answer"}
  </Button>
</form>

<style>
  .question {
    display: grid;
    gap: 0.55rem;
  }
  .reference {
    margin: 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.85rem;
    background: var(--color-surface-subtle);
    padding: 0.4rem 0.55rem;
    border-radius: 6px;
    overflow-wrap: anywhere;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: 0.5rem;
  }
  .field {
    display: grid;
    gap: 0.2rem;
    font-size: 0.78rem;
    color: var(--color-text-muted);
  }
  input,
  select,
  textarea {
    font: inherit;
    font-size: 0.85rem;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--color-border);
    border-radius: 6px;
    background: var(--color-surface);
    color: var(--color-text);
  }
  .error {
    color: var(--color-danger);
    font-size: 0.8rem;
    margin: 0;
  }
</style>
