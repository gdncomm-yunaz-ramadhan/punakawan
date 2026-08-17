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
        "text-max-width": "136px",
        shape: "round-rectangle",
        width: 152,
        height: 58,
        "border-width": 2,
        "border-color": palette.accent,
        "overlay-opacity": 0,
      },
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
        "curve-style": "taxi",
        "taxi-direction": "rightward",
        "taxi-turn": 28,
        "taxi-turn-min-distance": 14,
        label: "data(label)",
        color: palette.textMuted,
        "font-size": 11,
        "font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
        "text-background-color": palette.surface,
        "text-background-opacity": 1,
        "text-background-padding": "4px",
        "text-rotation": "autorotate",
      },
    },
  ];
}

export { readThemePalette };
