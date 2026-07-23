<script lang="ts">
  import PageHeader from "../../lib/components/PageHeader.svelte";
  import ThemeToggle from "../../lib/components/ThemeToggle.svelte";
  import AccentPicker from "../../lib/components/AccentPicker.svelte";
  import ResponsiveToolbar, { type ToolbarAction } from "../../lib/components/ResponsiveToolbar.svelte";
  import StickyActionBar from "../../lib/components/StickyActionBar.svelte";
  import Drawer from "../../lib/components/overlay/Drawer.svelte";
  import Dialog from "../../lib/components/overlay/Dialog.svelte";
  import BottomSheet from "../../lib/components/overlay/BottomSheet.svelte";
  import PlanDocument from "../../lib/components/review/PlanDocument.svelte";
  import CommentRail from "../../lib/components/review/CommentRail.svelte";
  import type { ArtifactComment } from "../../lib/review/api";

  let drawerOpen = $state(false);
  let dialogOpen = $state(false);
  let sheetOpen = $state(false);
  let lastAction: string | null = $state(null);

  const samplePlanMarkdown = `# Sample Plan

## Security Model

<!-- pk:block:panel.security.network-boundary -->
## Network Boundary

Default binding: 127.0.0.1 only.

## Rollout

The panel ships behind a feature flag.
`;

  const sampleComments: ArtifactComment[] = [
    {
      id: "comment-showcase-1",
      review_id: "review-showcase",
      author: "local",
      status: "open",
      anchor: {
        kind: "markdown_block",
        base_revision_hash: "sha256:showcase",
        heading_path: ["Sample Plan", "Network Boundary"],
        quoted_text: "Default binding: 127.0.0.1 only.",
      },
      body: "Should we support an authenticated LAN mode here?",
    },
    {
      id: "comment-showcase-2",
      review_id: "review-showcase",
      author: "local",
      status: "addressed",
      anchor: {
        kind: "markdown_block",
        base_revision_hash: "sha256:showcase",
        heading_path: ["Sample Plan", "Rollout"],
      },
      body: "Please name the feature flag explicitly.",
    },
    {
      id: "comment-showcase-3",
      review_id: "review-showcase",
      author: "local",
      status: "obsolete",
      anchor: { kind: "markdown_block", base_revision_hash: "sha256:showcase", heading_path: ["Sample Plan"] },
      body: "This comment was deleted.",
    },
  ];

  const toolbarActions: ToolbarAction[] = [
    { id: "approve", label: "Approve", onSelect: () => (lastAction = "Approve") },
    { id: "reject", label: "Reject", onSelect: () => (lastAction = "Reject") },
    { id: "comment", label: "Comment", onSelect: () => (lastAction = "Comment") },
    { id: "assign", label: "Assign", onSelect: () => (lastAction = "Assign") },
    { id: "flag", label: "Flag", onSelect: () => (lastAction = "Flag") },
  ];
</script>

<!--
  Dev/QA tool (UI-008): one instance of every new Phase 0 UI-foundation
  component, in both themes, so a human can visually verify them without
  reading source. Not part of the operational nav flow.
-->
<PageHeader
  title="Component Showcase"
  description="One instance of every new UI-foundation component (theme, accent, overlays, toolbars). Dev/QA use only."
/>

<section aria-labelledby="theme-heading">
  <h2 id="theme-heading">Theme</h2>
  <ThemeToggle />
</section>

<section aria-labelledby="accent-heading">
  <h2 id="accent-heading">Accent</h2>
  <AccentPicker />
</section>

<section aria-labelledby="toolbar-heading">
  <h2 id="toolbar-heading">ResponsiveToolbar</h2>
  <ResponsiveToolbar actions={toolbarActions} visibleCount={2} />
  {#if lastAction}
    <p class="muted" data-testid="last-action">Last action: {lastAction}</p>
  {/if}
</section>

<section aria-labelledby="overlay-heading">
  <h2 id="overlay-heading">Overlays</h2>
  <div class="button-row">
    <button type="button" onclick={() => (drawerOpen = true)}>Open Drawer</button>
    <button type="button" onclick={() => (dialogOpen = true)}>Open Dialog</button>
    <button type="button" onclick={() => (sheetOpen = true)}>Open BottomSheet</button>
  </div>
</section>

<Drawer open={drawerOpen} title="Drawer example" onclose={() => (drawerOpen = false)}>
  <p>This is a Drawer primitive: side-anchored, closes on backdrop click or Escape.</p>
</Drawer>

<Dialog open={dialogOpen} title="Dialog example" onclose={() => (dialogOpen = false)}>
  <p>This is a Dialog primitive: centered, closes on backdrop click or Escape.</p>
</Dialog>

<BottomSheet open={sheetOpen} title="Bottom sheet example" onclose={() => (sheetOpen = false)}>
  <p>This is a BottomSheet primitive: bottom-anchored with rounded top corners and a drag-handle affordance.</p>
</BottomSheet>

<section aria-labelledby="sticky-heading">
  <h2 id="sticky-heading">StickyActionBar</h2>
  <p class="muted">Sticks to the bottom of the viewport below 640px width; renders inline above that.</p>
  <StickyActionBar>
    <button type="button" onclick={() => (lastAction = "Sticky primary")}>Primary action</button>
    <button type="button" onclick={() => (lastAction = "Sticky secondary")}>Secondary</button>
  </StickyActionBar>
</section>

<section aria-labelledby="review-heading">
  <h2 id="review-heading">Review Mode Components (Artifact Review Phase 3)</h2>
  <p class="muted">
    PlanDocument (heading-aware markdown renderer with section/selection comment affordances) and CommentRail
    (status chips, ordered by document position), shown with sample data. See /reviews/{"{reviewId}"} for the full
    two-pane experience.
  </p>
  <div class="review-showcase-grid">
    <div class="review-doc">
      <PlanDocument content={samplePlanMarkdown} onCommentSection={() => {}} onCommentSelection={() => {}} />
    </div>
    <div class="review-rail">
      <CommentRail
        comments={sampleComments}
        documentHeadingOrder={["Sample Plan", "Sample Plan › Network Boundary", "Sample Plan › Rollout"]}
        editable={true}
        onEditComment={() => {}}
        onDeleteComment={() => {}}
      />
    </div>
  </div>
</section>

<style>
  section {
    margin-bottom: 1.75rem;
  }
  h2 {
    font-size: 0.95rem;
    margin: 0 0 0.5rem;
    color: var(--color-text);
  }
  .muted {
    color: var(--color-text-muted);
    font-size: 0.85rem;
  }
  .button-row {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .button-row button {
    border: 1px solid var(--color-border);
    background: var(--color-surface);
    color: var(--color-text);
    border-radius: 6px;
    padding: 0.4rem 0.75rem;
    cursor: pointer;
    min-height: 44px;
  }
  .review-showcase-grid {
    display: grid;
    grid-template-columns: 1fr 320px;
    gap: 1rem;
  }
  .review-doc {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    padding: 1rem 1.25rem;
    background: var(--color-surface);
  }
  @media (max-width: 1023px) {
    .review-showcase-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
