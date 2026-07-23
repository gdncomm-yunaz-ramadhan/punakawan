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
        "background-color": palette.accent,
        label: "data(label)",
        color: palette.text,
        "font-size": 10,
        "text-valign": "bottom",
        "text-margin-y": 4,
        width: 28,
        height: 28,
        "border-width": 2,
        "border-color": palette.surface,
      },
    },
    {
      selector: "node[type]",
      style: {
        "background-color": (el: NodeSingular) => nodeColor({ type: el.data("type") as string | undefined }, palette),
      },
    },
    {
      selector: "node:selected",
      style: {
        "border-color": palette.text,
        "border-width": 3,
      },
    },
    {
      selector: "node.focus-node",
      style: {
        "border-color": palette.danger,
        "border-width": 3,
      },
    },
    {
      selector: "edge",
      style: {
        width: 1.5,
        "line-color": palette.border,
        "target-arrow-color": palette.border,
        "target-arrow-shape": "triangle",
        "curve-style": "bezier",
        label: "data(label)",
        color: palette.textMuted,
        "font-size": 8,
      },
    },
  ];
}

export { readThemePalette };
