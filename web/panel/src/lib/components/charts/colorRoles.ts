import type { ThemePalette } from "./themeColors";
import type { ChartSeries } from "./types";

// Maps a series' semantic colorRole (§13.2 tokens) to the live palette
// value, falling back to a rotating set of the "neutral" semantic roles
// so callers that don't specify a role still get distinct, theme-aware
// colors instead of a hardcoded hex.
const ROTATION: (keyof ThemePalette)[] = ["accent", "secondary", "info", "success", "warning", "danger"];

export function resolveSeriesColor(series: ChartSeries, index: number, palette: ThemePalette): string {
  if (series.colorRole) return palette[series.colorRole];
  return palette[ROTATION[index % ROTATION.length]];
}
