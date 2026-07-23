<script lang="ts">
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ErrorStateCard from "../../lib/components/cards/ErrorStateCard.svelte";
  import { createReview } from "../../lib/review/api";
  import { navigate } from "../../lib/router/router.svelte";
  import { SessionExpiredError } from "../../lib/session";

  // Minimal create-review entry point (§13.10's "Start Review" action).
  // There is no "browse all plans" page yet (explicitly out of scope for
  // this phase), so plan id / title / instruction are simple form
  // fields rather than a picker - the point of this phase is the review
  // mode experience itself, not plan discovery. Extended by
  // punokawan-q9r.6 to also open a retrieval_recipe review (the
  // "Request a correction to this recipe" entry point from the recipe
  // detail view) - same handler, per artifact.type's union, only the
  // label copy and pre-filled id differ.
  interface Props {
    artifactType?: "plan" | "retrieval_recipe";
  }
  let { artifactType = "plan" }: Props = $props();

  const isRecipe = $derived(artifactType === "retrieval_recipe");
  const idLabel = $derived(isRecipe ? "Recipe ID" : "Plan ID");
  const idPlaceholder = $derived(isRecipe ? "pkw:recipe/..." : "plan-panel");
  const description = $derived(
    isRecipe
      ? "Open a draft review requesting a correction to this recipe's current version."
      : "Open a draft review against a plan's current version.",
  );

  // Pre-fills the id field when linked from a detail view
  // (?id=<artifact-id>) - the field stays editable either way, since
  // there is still no artifact picker in this phase.
  function idFromQuery(): string {
    if (typeof window === "undefined") return "";
    return new URLSearchParams(window.location.search).get("id") ?? "";
  }

  let artifactId = $state(idFromQuery());
  let title = $state("");
  let instruction = $state("");
  let submitting = $state(false);
  let error = $state<string | null>(null);
  let sessionExpired = $state(false);

  const canSubmit = $derived(artifactId.trim().length > 0 && title.trim().length > 0 && !submitting);

  async function submit() {
    if (!canSubmit) return;
    submitting = true;
    error = null;
    try {
      const review = await createReview(artifactType, artifactId.trim(), {
        title: title.trim(),
        instruction: instruction.trim() || undefined,
      });
      navigate(`/reviews/${encodeURIComponent(review.metadata.id)}`);
    } catch (e) {
      if (e instanceof SessionExpiredError) {
        sessionExpired = true;
      } else {
        error = e instanceof Error ? e.message : String(e);
      }
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader title="Start Review" {description} />

{#if sessionExpired}
  <ErrorStateCard
    title="Session expired"
    message="Your session has expired - reopen the panel from the terminal to continue."
  />
{:else}
  <form class="start-form" onsubmit={(e) => (e.preventDefault(), submit())}>
    <label for="artifact-id">{idLabel}</label>
    <input
      id="artifact-id"
      type="text"
      bind:value={artifactId}
      placeholder={idPlaceholder}
      data-testid="plan-id-input"
    />

    <label for="review-title">Title</label>
    <input
      id="review-title"
      type="text"
      bind:value={title}
      placeholder="Review title"
      data-testid="review-title-input"
    />

    <label for="review-instruction">Instruction (optional)</label>
    <textarea
      id="review-instruction"
      bind:value={instruction}
      placeholder="General instruction for this review…"
      data-testid="review-instruction-input"
    ></textarea>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <button type="submit" disabled={!canSubmit} data-testid="start-review-submit">
      {submitting ? "Creating…" : "Start Review"}
    </button>
  </form>
{/if}

<style>
  .start-form {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    max-width: 480px;
  }
  label {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--color-text-muted);
    margin-top: 0.5rem;
  }
  input,
  textarea {
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: 0.5rem;
    font-family: inherit;
    font-size: 0.9rem;
    color: var(--color-text);
    background: var(--color-surface);
    box-sizing: border-box;
  }
  textarea {
    min-height: 4rem;
    resize: vertical;
  }
  .error {
    color: var(--color-danger);
    font-size: 0.85rem;
  }
  button {
    margin-top: 0.75rem;
    align-self: flex-start;
    border: none;
    border-radius: 6px;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    padding: 0.5rem 1rem;
    font-size: 0.9rem;
    cursor: pointer;
    min-height: 44px;
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
