<script lang="ts">
  // Renders one aligned diff line list two ways from the same data (§13.10
  // "side-by-side when sufficient width exists, unified diff on mobile" is
  // a rendering choice, not two different diff computations) - matches
  // ReviewMode.svelte's isDesktop/forceWidth seam so tests can force each
  // branch deterministically.
  import type { DiffLine } from "../../review/api";
  import Icon from "../Icon.svelte";

  interface Props {
    lines: DiffLine[];
    isDesktop: boolean;
  }
  let { lines, isDesktop }: Props = $props();

  // A run of consecutive "equal" lines longer than this collapses behind
  // a toggle by default - short runs stay inline as useful context.
  const COLLAPSE_THRESHOLD = 6;

  type Row = { kind: "line"; line: DiffLine; index: number } | { kind: "collapsed"; lines: DiffLine[]; index: number };

  function groupIntoRows(input: DiffLine[]): Row[] {
    const rows: Row[] = [];
    let i = 0;
    while (i < input.length) {
      const line = input[i];
      if (line.Kind !== "equal") {
        rows.push({ kind: "line", line, index: i });
        i++;
        continue;
      }
      let j = i;
      const run: DiffLine[] = [];
      while (j < input.length && input[j].Kind === "equal") {
        run.push(input[j]);
        j++;
      }
      if (run.length > COLLAPSE_THRESHOLD) {
        rows.push({ kind: "collapsed", lines: run, index: i });
      } else {
        for (const l of run) {
          rows.push({ kind: "line", line: l, index: i });
          i++;
        }
        continue;
      }
      i = j;
    }
    return rows;
  }

  let searchTerm = $state("");
  let expandedCollapsed = $state(new Set<number>());

  function toggleCollapsed(index: number) {
    const next = new Set(expandedCollapsed);
    if (next.has(index)) next.delete(index);
    else next.add(index);
    expandedCollapsed = next;
  }

  const rows = $derived(groupIntoRows(lines));

  function matchesSearch(text: string): boolean {
    if (!searchTerm.trim()) return true;
    return text.toLowerCase().includes(searchTerm.trim().toLowerCase());
  }

  // Filtering hides non-matching *changed/context* lines but always keeps
  // collapsed-run markers visible (expanding one reveals its own matches)
  // rather than trying to search inside not-yet-rendered collapsed text.
  const visibleRows = $derived(
    searchTerm.trim() ? rows.filter((r) => (r.kind === "collapsed" ? true : matchesSearch(r.line.Text))) : rows,
  );

  function highlightParts(text: string): { text: string; match: boolean }[] {
    const term = searchTerm.trim();
    if (!term) return [{ text, match: false }];
    const parts: { text: string; match: boolean }[] = [];
    const lower = text.toLowerCase();
    const needle = term.toLowerCase();
    let pos = 0;
    while (pos < text.length) {
      const idx = lower.indexOf(needle, pos);
      if (idx === -1) {
        parts.push({ text: text.slice(pos), match: false });
        break;
      }
      if (idx > pos) parts.push({ text: text.slice(pos, idx), match: false });
      parts.push({ text: text.slice(idx, idx + needle.length), match: true });
      pos = idx + needle.length;
    }
    return parts;
  }

  function prefixFor(kind: DiffLine["Kind"]): string {
    if (kind === "added") return "+";
    if (kind === "removed") return "-";
    return " ";
  }
</script>

<div class="diff-viewer" data-testid="diff-viewer">
  <div class="diff-toolbar">
    <span class="diff-title"><Icon name="diff" size={16} /> Changes</span>
    <label class="search-wrap">
      <Icon name="search" size={15} />
      <input
        type="search"
        class="search-input"
        placeholder="Search changes…"
        aria-label="Search diff"
        data-testid="diff-search"
        bind:value={searchTerm}
      />
    </label>
  </div>

  {#if isDesktop}
    <div class="side-by-side" data-testid="diff-side-by-side">
      <div class="pane pane-base">
        <div class="pane-title">Original</div>
        {#each visibleRows as row (row.index)}
          {#if row.kind === "collapsed"}
            <button
              type="button"
              class="collapsed-toggle"
              data-testid="diff-collapsed-toggle"
              onclick={() => toggleCollapsed(row.index)}
            >
              {expandedCollapsed.has(row.index) ? "Hide" : `Show ${row.lines.length} unchanged lines`}
            </button>
            {#if expandedCollapsed.has(row.index)}
              {#each row.lines as l}
                <div class="line line-equal">
                  {#each highlightParts(l.Text) as part}
                    {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
                  {/each}
                </div>
              {/each}
            {/if}
          {:else if row.line.Kind !== "added"}
            <div class="line" class:line-removed={row.line.Kind === "removed"} class:line-equal={row.line.Kind === "equal"}>
              {#each highlightParts(row.line.Text) as part}
                {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
              {/each}
            </div>
          {:else}
            <div class="line line-placeholder"></div>
          {/if}
        {/each}
      </div>
      <div class="pane pane-proposed">
        <div class="pane-title">Proposed</div>
        {#each visibleRows as row (row.index)}
          {#if row.kind === "collapsed"}
            <div class="collapsed-spacer"></div>
            {#if expandedCollapsed.has(row.index)}
              {#each row.lines as l}
                <div class="line line-equal">
                  {#each highlightParts(l.Text) as part}
                    {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
                  {/each}
                </div>
              {/each}
            {/if}
          {:else if row.line.Kind !== "removed"}
            <div class="line" class:line-added={row.line.Kind === "added"} class:line-equal={row.line.Kind === "equal"}>
              {#each highlightParts(row.line.Text) as part}
                {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
              {/each}
            </div>
          {:else}
            <div class="line line-placeholder"></div>
          {/if}
        {/each}
      </div>
    </div>
  {:else}
    <div class="unified" data-testid="diff-unified">
      {#each visibleRows as row (row.index)}
        {#if row.kind === "collapsed"}
          <button
            type="button"
            class="collapsed-toggle"
            data-testid="diff-collapsed-toggle"
            onclick={() => toggleCollapsed(row.index)}
          >
            {expandedCollapsed.has(row.index) ? "Hide" : `Show ${row.lines.length} unchanged lines`}
          </button>
          {#if expandedCollapsed.has(row.index)}
            {#each row.lines as l}
              <div class="line line-equal">
                <span class="gutter">{prefixFor(l.Kind)}</span>
                {#each highlightParts(l.Text) as part}
                  {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
                {/each}
              </div>
            {/each}
          {/if}
        {:else}
          <div
            class="line"
            class:line-added={row.line.Kind === "added"}
            class:line-removed={row.line.Kind === "removed"}
            class:line-equal={row.line.Kind === "equal"}
          >
            <span class="gutter">{prefixFor(row.line.Kind)}</span>
            {#each highlightParts(row.line.Text) as part}
              {#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}
            {/each}
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .diff-viewer {
    overflow: hidden;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-card);
    background: var(--color-surface);
    box-shadow: var(--shadow-sm);
  }
  .diff-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    min-height: 46px;
    padding: 0.45rem 0.65rem;
    border-bottom: 1px solid var(--color-border);
    background: var(--color-surface-subtle);
  }
  .diff-title {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    color: var(--color-text);
    font-size: 0.82rem;
    font-weight: 700;
  }
  .search-wrap {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    width: min(280px, 55%);
    min-height: 44px;
    padding: 0 0.55rem;
    border: 1px solid var(--color-border-strong);
    border-radius: 7px;
    background: var(--color-surface);
    color: var(--color-text-muted);
  }
  .search-input {
    width: 100%;
    border: none;
    padding: 0.3rem 0;
    font-size: 0.8rem;
    background: transparent;
    color: var(--color-text);
    min-height: 44px;
    box-sizing: border-box;
  }
  .side-by-side {
    display: grid;
    grid-template-columns: 1fr 1fr;
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 0.8rem;
  }
  .pane {
    overflow-x: auto;
    max-height: 60vh;
    overflow-y: auto;
    border-right: 1px solid var(--color-border);
  }
  .pane:last-child {
    border-right: none;
  }
  .pane-title {
    position: sticky;
    top: 0;
    z-index: 2;
    padding: 0.42rem 0.7rem;
    border-bottom: 1px solid var(--color-border);
    background: color-mix(in srgb, var(--color-surface-subtle) 94%, transparent);
    color: var(--color-text-muted);
    font-family: Inter, ui-sans-serif, system-ui, sans-serif;
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .unified {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 0.8rem;
    overflow-x: auto;
    max-height: 60vh;
    overflow-y: auto;
  }
  .line {
    position: relative;
    white-space: pre-wrap;
    word-break: break-word;
    min-height: 24px;
    padding: 0.16rem 0.65rem 0.16rem 1.65rem;
    border-bottom: 1px solid color-mix(in srgb, var(--color-border) 48%, transparent);
    line-height: 1.45;
  }
  .side-by-side .line::before {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.35rem;
    color: var(--color-text-muted);
    background: color-mix(in srgb, var(--color-surface-subtle) 72%, transparent);
    content: " ";
    user-select: none;
  }
  .side-by-side .line-added::before {
    content: "+";
    color: var(--color-success);
    background: color-mix(in srgb, var(--color-success) 18%, var(--color-surface));
  }
  .side-by-side .line-removed::before {
    content: "−";
    color: var(--color-danger);
    background: color-mix(in srgb, var(--color-danger) 18%, var(--color-surface));
  }
  .line-placeholder {
    background: repeating-linear-gradient(
      -45deg,
      var(--color-surface-subtle),
      var(--color-surface-subtle) 4px,
      color-mix(in srgb, var(--color-border) 45%, transparent) 4px,
      color-mix(in srgb, var(--color-border) 45%, transparent) 5px
    );
  }
  .line-added {
    background: color-mix(in srgb, var(--color-success) 12%, var(--color-surface));
  }
  .line-removed {
    background: color-mix(in srgb, var(--color-danger) 11%, var(--color-surface));
  }
  .gutter {
    display: inline-block;
    width: 1rem;
    color: var(--color-text-muted);
  }
  .collapsed-toggle {
    display: block;
    width: 100%;
    text-align: left;
    border: none;
    border-top: 1px solid color-mix(in srgb, var(--color-info) 22%, var(--color-border));
    border-bottom: 1px solid color-mix(in srgb, var(--color-info) 22%, var(--color-border));
    background: color-mix(in srgb, var(--color-info) 8%, var(--color-surface));
    color: var(--color-accent);
    font-size: 0.8rem;
    padding: 0.35rem 0.5rem;
    min-height: 44px;
    box-sizing: border-box;
    cursor: pointer;
  }
  .collapsed-spacer {
    height: 44px;
    border-top: 1px solid color-mix(in srgb, var(--color-info) 22%, var(--color-border));
    border-bottom: 1px solid color-mix(in srgb, var(--color-info) 22%, var(--color-border));
    background: color-mix(in srgb, var(--color-info) 8%, var(--color-surface));
  }
  mark {
    background: color-mix(in srgb, var(--color-warning) 35%, transparent);
    color: inherit;
  }

  @media (max-width: 639px) {
    .diff-toolbar {
      align-items: stretch;
      flex-direction: column;
    }
    .search-wrap {
      width: 100%;
    }
  }
</style>
