<script lang="ts">
  // General review instruction textarea (§13.10 "General review
  // instruction"): auto-saves via a debounced PATCH, with an explicit
  // "unsaved changes" indicator so the save behavior is obvious even
  // though it's automatic (per the phase brief's explicit call-out).

  interface Props {
    instruction: string;
    onsave: (instruction: string) => Promise<void>;
    debounceMs?: number;
  }
  let { instruction, onsave, debounceMs = 800 }: Props = $props();

  // Populated for real by the $effect below (which also runs once on
  // mount) - not read from `instruction` here, since that would only
  // capture the prop's initial value rather than staying in sync.
  let draft = $state("");
  let dirty = $state(false);
  let saving = $state(false);
  let saveError = $state<string | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;

  // Keep the local draft in sync when the parent hands us a freshly
  // fetched instruction (e.g. on route entry / resume) - but only while
  // there's no unsaved local edit in flight, so a slow refetch never
  // clobbers something the user just typed.
  $effect(() => {
    if (!dirty) {
      draft = instruction;
    }
  });

  function scheduleSave() {
    dirty = true;
    saveError = null;
    if (timer) clearTimeout(timer);
    timer = setTimeout(doSave, debounceMs);
  }

  async function doSave() {
    saving = true;
    try {
      await onsave(draft);
      dirty = false;
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  function handleInput(e: Event) {
    draft = (e.target as HTMLTextAreaElement).value;
    scheduleSave();
  }

  // Exposed so a parent (ReviewMode) can check "is there a pending save"
  // for its own beforeunload/navigate-away guard.
  export function hasUnsavedChanges(): boolean {
    return dirty;
  }
</script>

<div class="panel" data-testid="review-instruction-panel">
  <div class="head">
    <div>
      <span class="eyebrow">Review focus</span>
      <h2 id="review-instruction-heading">What should the next revision prioritize?</h2>
    </div>
    {#if saving}
      <span class="status saving" data-testid="instruction-status">Saving…</span>
    {:else if dirty}
      <span class="status unsaved" data-testid="instruction-status">Unsaved changes</span>
    {:else}
      <span class="status saved" data-testid="instruction-status">Saved</span>
    {/if}
  </div>
  <textarea
    class="instruction-input"
    placeholder="Add overall guidance, priorities, or constraints for the reviewer…"
    aria-labelledby="review-instruction-heading"
    value={draft}
    oninput={handleInput}
    data-testid="instruction-input"
  ></textarea>
  {#if saveError}
    <p class="error" role="alert">Failed to save: {saveError}</p>
  {/if}
</div>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
    margin-bottom: 1rem;
    padding: 0.9rem 1rem;
    border: 1px solid color-mix(in srgb, var(--color-info) 24%, var(--color-border));
    border-radius: var(--radius-card);
    background:
      linear-gradient(120deg, color-mix(in srgb, var(--color-info) 7%, transparent), transparent 42%),
      var(--color-surface);
    box-shadow: var(--shadow-sm);
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  h2 {
    font-size: 0.92rem;
    margin: 0;
    color: var(--color-text);
  }
  .eyebrow {
    display: block;
    margin-bottom: 0.1rem;
    color: var(--color-info);
    font-size: 0.66rem;
    font-weight: 750;
    letter-spacing: 0.07em;
    text-transform: uppercase;
  }
  .status {
    font-size: 0.75rem;
    font-weight: 600;
  }
  .status.saved {
    color: var(--color-success);
  }
  .status.unsaved {
    color: var(--color-warning);
  }
  .status.saving {
    color: var(--color-text-muted);
  }
  .instruction-input {
    width: 100%;
    box-sizing: border-box;
    min-height: 4.5rem;
    border: 1px solid var(--color-border);
    border-radius: 8px;
    padding: 0.65rem 0.75rem;
    font-family: inherit;
    font-size: 0.85rem;
    color: var(--color-text);
    background: var(--color-surface);
    resize: vertical;
    box-shadow: inset 0 1px 2px rgb(16 24 40 / 0.04);
  }
  .instruction-input:focus {
    border-color: var(--color-accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 13%, transparent);
  }
  .error {
    margin: 0;
    color: var(--color-danger);
    font-size: 0.8rem;
  }
</style>
