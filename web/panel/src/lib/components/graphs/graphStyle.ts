import type { StylesheetJson } from "cytoscape";
import { readThemePalette, type ThemePalette } from "../charts/themeColors";
import type { GraphNode } from "./types";

// Maps a node's `type` field to a semantic color role, so distinct node
// kinds (e.g. workflow "task" vs "gate", relation "artifact" vs
// "version") get distinct, theme-aware coloring without any hardcoded
// hex values. Falls back to the plain accent color for unrecognized/
// unset types.
const TYPE_ROLE: Record<string, keyof ThemePalette> = {
  task: "accent",
  step: "accent",
  gate: "warning",
  dependency: "info",
  artifact: "secondary",
  version: "info",
  relation: "accent",
  focus: "danger",
};

export function nodeColor(node: Pick<GraphNode, "type">, palette: ThemePalette): string {
  const role = node.type ? TYPE_ROLE[node.type] : undefined;
  return palette[role ?? "accent"];
}

// Builds a Cytoscape stylesheet resolved from the live theme palette.
// Called on initial mount and again whenever the theme toggles.
export function buildGraphStylesheet(palette: ThemePalette): StylesheetJson {
  // One plain data-driven selector per known node type, instead of a JS
  // callback re-evaluated on every style application - nodeColor() still
  // owns the type->color-role mapping, it's just resolved once per type
  // here rather than once per element per render.
  const typeStyles: StylesheetJson = Object.keys(TYPE_ROLE).map((type) => {
    const color = nodeColor({ type }, palette);
    return {
      selector: `node[type = "${type}"]`,
      style: {
        "background-color": color === palette.accent ? palette.surface : palette.surfaceSubtle,
        "border-color": color,
      },
    };
  });

  return [
    {
      selector: "node",
      style: {
        "background-color": palette.surface,
        label: "data(label)",
        color: palette.text,
        "font-size": 13,
        "font-weight": 600,
        "font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
        "text-valign": "center",
        "text-halign": "center",
        "text-wrap": "wrap",
        // Real titles read as full sentences/phrases, not single words -
        // a fixed 136px wrap width paired with a fixed 152x58 box either
        // clipped long titles or crushed short ones into wasted space.
        // 200px keeps line count sane for sentence-length labels (roughly
        // 4 wrapped lines for a ~90-char task title at this font size)
        // without making every node an oversized banner.
        "text-max-width": "200px",
        // Our labels are natural-language phrases, so wrap on whitespace
        // (keep words intact) rather than "anywhere", which is meant for
        // scripts without spaces (e.g. CJK) and would break mid-word here.
        "text-overflow-wrap": "whitespace",
        // Slightly looser than the 1 (default) so a 3-4 line wrapped title
        // doesn't read as a solid block of cramped text.
        "line-height": 1.3,
        shape: "round-rectangle",
        // The actual fix for "still not readable": size each node to its
        // own label's rendered footprint instead of forcing every node -
        // whether its label is "v1" or a full sentence - into one fixed
        // 152x58 box. Short labels get a tidy small box; long ones grow
        // (wider up to text-max-width, then taller) instead of being
        // clipped or squeezed.
        width: "label",
        height: "label",
        // Breathing room between the wrapped text and the node border,
        // now that the border sits right at the label's own bounds
        // instead of a fixed box with built-in slack.
        padding: "12px",
        "border-width": 2,
        "border-color": palette.accent,
        "overlay-opacity": 0,
      },
    },
    {
      // Per-node manual resize (GraphCanvas's drag handle) writes
      // node.data({ width, height }) rather than touching the stylesheet -
      // these two selectors are what pick that up. They're plain data()
      // mappers, so only listed after the base "node" rule (and only
      // matching elements that actually carry the data field) they don't
      // disturb the label-driven default size for every other node.
      selector: "node[width]",
      style: { width: "data(width)" },
    },
    {
      selector: "node[height]",
      style: { height: "data(height)" },
    },
    ...typeStyles,
    {
      selector: "node:selected",
      style: {
        "border-color": palette.accent,
        "border-width": 3,
        "background-color": palette.accentSoft,
      },
    },
    {
      selector: "node.focus-node",
      style: {
        "border-color": palette.danger,
        "border-width": 3,
        "background-color": palette.surfaceSubtle,
      },
    },
    {
      selector: "edge",
      style: {
        width: 1.8,
        "line-color": palette.borderStrong,
        "target-arrow-color": palette.borderStrong,
        "target-arrow-shape": "triangle",
        "arrow-scale": 0.9,
        // "taxi" forces rectilinear (right-angle) routing, which reads
        // well on a strictly aligned grid/flowchart but fights cose's
        // organic, non-grid-aligned node placement - edges end up as
        // ungainly zigzags instead of clean lines. Bezier's gentle curve
        // (Cytoscape's own general-purpose default) looks natural for
        // both cose's scattered layout and breadthfirst's tiered one, and
        // it separates edges that share the same two endpoints instead of
        // stacking them exactly on top of each other the way taxi does.
        "curve-style": "bezier",
        label: "data(label)",
        color: palette.textMuted,
        "font-size": 11,
        "font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
        "text-background-color": palette.surface,
        "text-background-opacity": 1,
        "text-background-padding": "4px",
        "text-background-shape": "roundrectangle",
        // A faint outline around the label's background so it stays
        // legible where several curved edges cross near each other -
        // without this, an edge label can visually blend into a same-
        // colored line passing directly behind its background box.
        "text-border-width": 1,
        "text-border-color": palette.border,
        "text-border-opacity": 1,
        "text-rotation": "autorotate",
      },
    },
  ];
}

export { readThemePalette };
