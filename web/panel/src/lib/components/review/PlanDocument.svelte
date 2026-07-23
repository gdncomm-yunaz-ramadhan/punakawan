<script lang="ts">
  import { groupIntoSections, parseDocument, snippetForSection, type Section } from "../../review/markdown";

  export interface SectionCommentRequest {
    headingPath: string[];
    quotedText?: string;
  }

  interface Props {
    content: string;
    // Called when the user clicks the "Add Comment" affordance on a
    // section heading with no text selected (§13.10 "section ...
    // commenting"). quotedText is a derived snippet from the section's
    // own body (see markdown.ts's snippetForSection) so the anchor still
    // resolves server-side - see that function's doc comment for why a
    // pure heading-only anchor can't resolve.
    onCommentSection: (req: SectionCommentRequest) => void;
    // Called when the user selects text inside the document and chooses
    // "Comment on selection" (§13.10 "selected-text commenting").
    onCommentSelection: (req: SectionCommentRequest) => void;
  }
  let { content, onCommentSection, onCommentSelection }: Props = $props();

  const sections = $derived(groupIntoSections(parseDocument(content)));

  let containerEl: HTMLDivElement | undefined;
  let selectionText = $state<string | null>(null);
  let selectionHeadingPath = $state<string[]>([]);
  let selectionRect = $state<{ top: number; left: number } | null>(null);

  function headingPathAt(el: Element | null): string[] {
    const sectionEl = el?.closest("[data-heading-path]");
    if (!sectionEl) return [];
    try {
      return JSON.parse(sectionEl.getAttribute("data-heading-path") ?? "[]");
    } catch {
      return [];
    }
  }

  // onselectionchange fires for every selection change document-wide, so
  // this only reacts when the selection lies inside our own container
  // and is non-empty/non-collapsed - anything else disables the
  // "Comment on selection" affordance rather than risk sending a bad
  // anchor (per the phase brief's selection-validation requirement).
  function handleSelectionChange() {
    if (typeof window === "undefined" || !containerEl) return;
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) {
      selectionText = null;
      return;
    }
    const range = sel.getRangeAt(0);
    if (!containerEl.contains(range.commonAncestorContainer)) {
      selectionText = null;
      return;
    }
    const text = sel.toString().trim();
    if (!text) {
      selectionText = null;
      return;
    }
    selectionText = text;
    selectionHeadingPath = headingPathAt(
      range.commonAncestorContainer.nodeType === Node.ELEMENT_NODE
        ? (range.commonAncestorContainer as Element)
        : range.commonAncestorContainer.parentElement,
    );
    // jsdom (used by the test suite) has no layout engine and doesn't
    // implement Range.getBoundingClientRect at all - guard so tests can
    // exercise selection handling without a real browser layout.
    if (typeof range.getBoundingClientRect === "function") {
      const rect = range.getBoundingClientRect();
      selectionRect = { top: rect.bottom + window.scrollY, left: rect.left + window.scrollX };
    } else {
      selectionRect = { top: 0, left: 0 };
    }
  }

  function commentOnSelection() {
    if (!selectionText) return;
    onCommentSelection({ headingPath: selectionHeadingPath, quotedText: selectionText });
    selectionText = null;
    if (typeof window !== "undefined") window.getSelection()?.removeAllRanges();
  }

  function commentOnSection(section: Section) {
    onCommentSection({ headingPath: section.headingPath, quotedText: snippetForSection(section) });
  }

  const headingTag = (level: number) => `h${Math.min(Math.max(level, 1), 6)}`;
</script>

<svelte:document onselectionchange={handleSelectionChange} />

<div class="plan-document" bind:this={containerEl} data-testid="plan-document">
  {#each sections as section, i (i)}
    <section data-heading-path={JSON.stringify(section.headingPath)}>
      {#if section.heading}
        <div class="heading-row">
          <svelte:element this={headingTag(section.heading.level)} class="heading">
            {section.heading.text}
          </svelte:element>
          <button
            type="button"
            class="add-comment-affordance"
            data-testid="add-section-comment"
            onclick={() => commentOnSection(section)}
          >
            + Comment on section
          </button>
        </div>
      {/if}
      {#each section.nodes as node, j (j)}
        {#if node.kind === "paragraph"}
          <p>{node.text}</p>
        {/if}
        <!-- blockMarker nodes are deliberately invisible: they are an
             anchoring aid (<!-- pk:block:... -->), not visible content. -->
      {/each}
    </section>
  {/each}

  {#if selectionText}
    <div
      class="selection-popover"
      style:top={selectionRect ? `${selectionRect.top}px` : undefined}
      style:left={selectionRect ? `${selectionRect.left}px` : undefined}
      data-testid="selection-popover"
    >
      <button type="button" onclick={commentOnSelection}>Comment on selection</button>
    </div>
  {/if}
</div>

<style>
  .plan-document {
    color: var(--color-text);
    line-height: 1.6;
  }
  section {
    margin-bottom: 1.25rem;
  }
  .heading-row {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .heading {
    margin: 0;
    color: var(--color-text);
  }
  .add-comment-affordance {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text-muted);
    border-radius: 6px;
    padding: 0.15rem 0.5rem;
    font-size: 0.75rem;
    cursor: pointer;
    opacity: 0;
    transition: opacity 120ms ease;
  }
  .heading-row:hover .add-comment-affordance,
  .add-comment-affordance:focus-visible {
    opacity: 1;
  }
  p {
    color: var(--color-text);
  }
  .selection-popover {
    position: absolute;
    z-index: 20;
    background: var(--color-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    box-shadow: var(--shadow-card);
    padding: 0.25rem;
  }
  .selection-popover button {
    border: none;
    background: var(--color-accent);
    color: var(--color-accent-contrast);
    border-radius: 4px;
    padding: 0.35rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
  }
</style>
