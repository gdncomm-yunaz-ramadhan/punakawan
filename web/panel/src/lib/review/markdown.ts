// Minimal heading-aware Markdown -> section model, mirroring
// internal/artifact/blockid.go's ExtractBlocks/ResolveAnchor closely
// enough that anchors built here round-trip against the server. This is
// NOT a CommonMark parser (per the phase brief, a full parser is
// explicitly not required) - it only recognizes ATX headings (`#`..`######`),
// `<!-- pk:block:<id> -->` markers, and treats every other line as
// paragraph text.
//
// Two models are exposed:
//  - parseSections(): every heading's own section (for rendering + the
//    "click a heading to comment on this section" affordance), including
//    headings that have no pk:block marker at all.
//  - ExtractBlocks-equivalent block matching is NOT reimplemented here in
//    full generality; instead resolveHeadingPathAt() below gives the
//    heading_path for a DOM position, and buildAnchor() constructs a
//    server-shape anchor from a heading path + optional quoted text,
//    which is all the client needs to send - resolution itself is the
//    server's job (per §6).

export interface MarkdownBlockMarker {
  id: string;
}

export interface HeadingNode {
  level: number; // 1-6
  text: string;
  // Heading path leading up to (not including) this heading itself -
  // i.e. the path a block appearing immediately under this heading would
  // report, matching blockid.go's headingPathOf(stack-before-push).
  parentPath: string[];
}

export type DocNode =
  | { kind: "heading"; level: number; text: string; headingPath: string[]; id: string }
  | { kind: "paragraph"; text: string; headingPath: string[] }
  | { kind: "blockMarker"; blockId: string; headingPath: string[] };

const BLOCK_MARKER_RE = /^<!--\s*pk:block:([A-Za-z0-9._-]+)\s*-->\s*$/;
const HEADING_RE = /^(#{1,6})\s+(.*\S)\s*$/;

function slugify(text: string, seen: Map<string, number>): string {
  const base = text
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-");
  const count = seen.get(base) ?? 0;
  seen.set(base, count + 1);
  return count === 0 ? base : `${base}-${count}`;
}

// parseDocument walks markdown into a flat list of nodes (headings,
// paragraphs, block markers), each carrying the heading_path active at
// that point - the same "currently active nesting, not every heading
// seen so far" semantics as blockid.go's headingPathOf/headingStack
// (sibling headings replace each other in the stack, per its own test
// comment).
export function parseDocument(markdown: string): DocNode[] {
  const lines = markdown.split("\n");
  const nodes: DocNode[] = [];
  const headingStack: { level: number; text: string }[] = [];
  const slugSeen = new Map<string, number>();
  let paragraphBuf: string[] = [];

  function pathOf(): string[] {
    return headingStack.map((h) => h.text);
  }

  function flushParagraph() {
    const text = paragraphBuf.join("\n").trim();
    paragraphBuf = [];
    if (text) {
      nodes.push({ kind: "paragraph", text, headingPath: pathOf() });
    }
  }

  for (const line of lines) {
    const markerMatch = BLOCK_MARKER_RE.exec(line);
    if (markerMatch) {
      flushParagraph();
      nodes.push({ kind: "blockMarker", blockId: markerMatch[1], headingPath: pathOf() });
      continue;
    }
    const headingMatch = HEADING_RE.exec(line);
    if (headingMatch) {
      flushParagraph();
      const level = headingMatch[1].length;
      const text = headingMatch[2];
      while (headingStack.length > 0 && headingStack[headingStack.length - 1].level >= level) {
        headingStack.pop();
      }
      const parentPath = pathOf();
      headingStack.push({ level, text });
      nodes.push({ kind: "heading", level, text, headingPath: parentPath, id: slugify(text, slugSeen) });
      continue;
    }
    if (line.trim() === "") {
      flushParagraph();
      continue;
    }
    paragraphBuf.push(line);
  }
  flushParagraph();
  return nodes;
}

export interface Section {
  heading: Extract<DocNode, { kind: "heading" }> | null; // null for preamble content before any heading
  headingPath: string[]; // this section's own full path, i.e. parentPath + [text]
  nodes: DocNode[]; // paragraphs (and nested content) directly under this heading, before the next heading of <= level
  firstParagraphText: string | null;
}

// groupIntoSections re-groups parseDocument's flat node list into
// per-heading sections, each with its own full heading_path and the
// paragraph text immediately under it - enough to render the document
// and to derive a representative quoted_text snippet for "comment on
// this section" without a text selection (see buildSectionAnchor below).
export function groupIntoSections(nodes: DocNode[]): Section[] {
  const sections: Section[] = [];
  let current: Section | null = null;

  for (const node of nodes) {
    if (node.kind === "heading") {
      current = {
        heading: node,
        headingPath: [...node.headingPath, node.text],
        nodes: [],
        firstParagraphText: null,
      };
      sections.push(current);
      continue;
    }
    if (!current) {
      current = { heading: null, headingPath: [], nodes: [], firstParagraphText: null };
      sections.push(current);
    }
    current.nodes.push(node);
    if (node.kind === "paragraph" && current.firstParagraphText === null) {
      current.firstParagraphText = node.text;
    }
  }
  return sections;
}

export interface AnchorInput {
  baseRevisionHash: string;
  headingPath?: string[];
  quotedText?: string;
  blockId?: string;
}

export interface CommentAnchor {
  kind: "markdown_block";
  base_revision_hash: string;
  heading_path?: string[];
  quoted_text?: string;
  block_id?: string;
}

// buildAnchor constructs the exact JSON shape the server's
// POST /reviews/{id}/comments endpoint expects (protocol.ArtifactCommentAnchor).
export function buildAnchor(input: AnchorInput): CommentAnchor {
  const anchor: CommentAnchor = {
    kind: "markdown_block",
    base_revision_hash: input.baseRevisionHash,
  };
  if (input.headingPath && input.headingPath.length > 0) anchor.heading_path = input.headingPath;
  if (input.quotedText) anchor.quoted_text = input.quotedText;
  if (input.blockId) anchor.block_id = input.blockId;
  return anchor;
}

// snippetForSection derives a short representative quote from a
// section's own body text, trimmed to a bounded length. This exists
// because the server's anchor resolver (internal/artifact/blockid.go)
// has no "resolve by heading path alone" step - every non-block_id
// resolution path requires quoted_text to match against the section's
// content (§6 steps 3/4 both require heading_path AND quoted_text). A
// pure "comment on this heading, no text selected" anchor would 400 as
// AnchorConflicted without this. Deriving the snippet from the section's
// own first paragraph keeps the anchor truthful (it really is text
// found in that section) rather than fabricating unrelated text.
export function snippetForSection(section: Section, maxLen = 120): string | undefined {
  const text = section.firstParagraphText ?? section.heading?.text;
  if (!text) return undefined;
  const normalized = text.replace(/\s+/g, " ").trim();
  if (normalized.length <= maxLen) return normalized;
  return normalized.slice(0, maxLen).trim();
}
