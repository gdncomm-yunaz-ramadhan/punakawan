import type { ThemePalette } from "./themeColors";
import type { ChartSeries } from "./types";

// Maps a series' semantic colorRole (§13.2 tokens) to the live palette
// value, falling back to the batik categorical palette (gold, teal,
// terracotta, indigo, violet) so callers that don't specify a role still
// get distinct, theme-aware, on-brand colors instead of a hardcoded hex.
// Series that DO set a semantic colorRole (e.g. added=success,
// removed=danger) keep that meaning and never touch this rotation.
const ROTATION: (keyof ThemePalette)[] = ["gold", "teal", "terracotta", "indigo", "violet"];

export function resolveSeriesColor(series: ChartSeries, index: number, palette: ThemePalette): string {
  if (series.colorRole) return palette[series.colorRole];
  return palette[ROTATION[index % ROTATION.length]];
}
