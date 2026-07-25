<script lang="ts">
  import { groupIntoSections, parseDocument, snippetForSection, type Section } from "../../review/markdown";
  import Icon from "../Icon.svelte";

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
  <div class="document-toolbar">
    <span class="document-type"><Icon name="file" size={16} /> Plan review document</span>
    <span class="document-hint">Select text or review a section to leave focused feedback</span>
  </div>
  <div class="document-body">
  {#each sections as section, i (i)}
    <section class:lead-section={i === 0} data-heading-path={JSON.stringify(section.headingPath)}>
      {#if section.heading}
        <div class="heading-row">
          <span class="section-index" aria-hidden="true">{String(i + 1).padStart(2, "0")}</span>
          <svelte:element this={headingTag(section.heading.level)} class="heading">
            {section.heading.text}
          </svelte:element>
          <button
            type="button"
            class="add-comment-affordance"
            data-testid="add-section-comment"
            onclick={() => commentOnSection(section)}
          >
            <Icon name="comment" size={14} /> Comment
          </button>
        </div>
      {/if}
      {#each section.nodes as node, j (j)}
        {#if node.kind === "paragraph"}
          <p>{node.text}</p>
        {/if}
        <!-- blockMarker nodes are deliberately invisible: they are an
             anchoring aid (a pk:block HTML marker), not visible content. -->
      {/each}
    </section>
  {/each}
  </div>

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
    line-height: 1.68;
    overflow: hidden;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: var(--color-surface);
  }
  .document-toolbar {
    position: sticky;
    top: 0;
    z-index: 4;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    min-height: 48px;
    padding: 0.55rem 0.85rem;
    border-bottom: 1px solid var(--color-border);
    background: color-mix(in srgb, var(--color-surface-subtle) 92%, transparent);
    backdrop-filter: blur(10px);
  }
  .document-type {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    color: var(--color-text);
    font-size: 0.78rem;
    font-weight: 700;
  }
  .document-hint {
    color: var(--color-text-muted);
    font-size: 0.72rem;
  }
  .document-body {
    max-width: 860px;
    margin: 0 auto;
    padding: clamp(1.2rem, 3vw, 2.25rem);
  }
  section {
    position: relative;
    margin: 0 0 1.35rem;
    padding: 0 0 1.35rem 2.85rem;
    border-bottom: 1px solid color-mix(in srgb, var(--color-border) 72%, transparent);
  }
  section:last-child {
    border-bottom: none;
    margin-bottom: 0;
  }
  section::before {
    content: "";
    position: absolute;
    left: 1.02rem;
    top: 2.15rem;
    bottom: -0.45rem;
    width: 1px;
    background: linear-gradient(var(--color-border-strong), transparent);
  }
  section:last-child::before {
    display: none;
  }
  .heading-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
    min-height: 38px;
  }
  .section-index {
    position: absolute;
    left: -2.85rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.05rem;
    height: 2.05rem;
    border: 1px solid color-mix(in srgb, var(--color-accent) 22%, var(--color-border));
    border-radius: 9px;
    color: var(--color-accent);
    background: var(--color-accent-soft);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.69rem;
    font-weight: 750;
  }
  .heading {
    margin: 0;
    color: var(--color-text);
    letter-spacing: -0.015em;
    line-height: 1.3;
  }
  .add-comment-affordance {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-accent);
    border-radius: 7px;
    padding: 0.25rem 0.55rem;
    font-size: 0.75rem;
    font-weight: 650;
    cursor: pointer;
    min-height: 32px;
  }
  .add-comment-affordance:hover {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  /* Hover-reveal is a nice-to-have declutter on pointer-capable/hover
     devices only (§13.4 "no essential action depends on hover") - touch
     devices (and any device without a fine hover pointer) always show
     the affordance instead of gating it behind an interaction they can't
     perform. The opacity transition itself is nonessential motion, so it
     only applies for users who haven't asked for reduced motion. */
  @media (hover: hover) and (pointer: fine) {
    .add-comment-affordance {
      opacity: 0;
    }
    .heading-row:hover .add-comment-affordance,
    .add-comment-affordance:focus-visible {
      opacity: 1;
    }
  }
  @media (hover: hover) and (pointer: fine) and (prefers-reduced-motion: no-preference) {
    .add-comment-affordance {
      transition: opacity 120ms ease;
    }
  }
  p {
    color: var(--color-text);
    margin: 0.65rem 0;
    max-width: 76ch;
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
    min-height: 44px;
  }

  @media (max-width: 639px) {
    .document-hint {
      display: none;
    }
    .document-body {
      padding: 1rem;
    }
    section {
      padding-left: 2.5rem;
    }
    .section-index {
      left: -2.5rem;
    }
  }
</style>
