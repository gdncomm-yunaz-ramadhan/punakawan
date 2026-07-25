import type { NodeSingular, StylesheetJson } from "cytoscape";
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
  return [
    {
      selector: "node",
      style: {
        "background-color": palette.surface,
        label: "data(label)",
        color: palette.text,
        "font-size": 11,
        "font-weight": 600,
        "font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
        "text-valign": "center",
        "text-halign": "center",
        "text-wrap": "wrap",
        "text-max-width": "112px",
        shape: "round-rectangle",
        width: 126,
        height: 46,
        "border-width": 2,
        "border-color": palette.accent,
        "overlay-opacity": 0,
      },
    },
    {
      selector: "node[type]",
      style: {
        "background-color": (el: NodeSingular) => {
          const color = nodeColor({ type: el.data("type") as string | undefined }, palette);
          return color === palette.accent ? palette.surface : palette.surfaceSubtle;
        },
        "border-color": (el: NodeSingular) => nodeColor({ type: el.data("type") as string | undefined }, palette),
      },
    },
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
        "font-size": 9,
        "font-family": "Inter, ui-sans-serif, system-ui, sans-serif",
        "text-background-color": palette.surface,
        "text-background-opacity": 0.94,
        "text-background-padding": "3px",
        "text-rotation": "autorotate",
      },
    },
  ];
}

export { readThemePalette };
