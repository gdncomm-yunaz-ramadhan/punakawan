// Resolves the semantic theme.css custom properties (§13.2) into concrete
// color strings at runtime. Chart.js and Cytoscape.js both need actual
// color values (not `var(--color-x)` references) since they paint onto a
// <canvas>, so this is the one place both the chart and graph adapters go
// to read the live palette - and the one place that re-reads it after a
// theme toggle.
export interface ThemePalette {
  bg: string;
  surface: string;
  surfaceSubtle: string;
  surfaceRaised: string;
  text: string;
  textMuted: string;
  border: string;
  borderStrong: string;
  accent: string;
  accentHover: string;
  accentSoft: string;
  accentContrast: string;
  secondary: string;
  success: string;
  warning: string;
  danger: string;
  info: string;
  // Batik brand hues (§13.2 additive tokens). Used as the categorical
  // chart series palette (colorRoles.ts ROTATION) so multi-series charts
  // read as one wayang-themed system instead of a rainbow of semantics.
  gold: string;
  teal: string;
  terracotta: string;
  indigo: string;
  violet: string;
}

const PROPERTY_MAP: Record<keyof ThemePalette, string> = {
  bg: "--color-bg",
  surface: "--color-surface",
  surfaceSubtle: "--color-surface-subtle",
  surfaceRaised: "--color-surface-raised",
  text: "--color-text",
  textMuted: "--color-text-muted",
  border: "--color-border",
  borderStrong: "--color-border-strong",
  accent: "--color-accent",
  accentHover: "--color-accent-hover",
  accentSoft: "--color-accent-soft",
  accentContrast: "--color-accent-contrast",
  secondary: "--color-secondary",
  success: "--color-success",
  warning: "--color-warning",
  danger: "--color-danger",
  info: "--color-info",
  gold: "--color-gold",
  teal: "--color-teal",
  terracotta: "--color-terracotta",
  indigo: "--color-indigo",
  violet: "--color-violet",
};

// Fallback palette used when there is no document (SSR-less test contexts
// that skip DOM setup) - mirrors theme.css's light theme so charts/graphs
// still render something sane.
const FALLBACK: ThemePalette = {
  bg: "#f7f5f0",
  surface: "#ffffff",
  surfaceSubtle: "#f0ece3",
  surfaceRaised: "#ffffff",
  text: "#1f2430",
  textMuted: "#6b6459",
  border: "#e6ddcf",
  borderStrong: "#d3c8b6",
  accent: "#235fb5",
  accentHover: "#1b4a90",
  accentSoft: "#e8eefb",
  accentContrast: "#ffffff",
  secondary: "#128a5e",
  success: "#128a5e",
  warning: "#c07b12",
  danger: "#c7402a",
  info: "#235fb5",
  gold: "#c9880f",
  teal: "#128a5e",
  terracotta: "#d9531f",
  indigo: "#235fb5",
  violet: "#7a5bd0",
};

export function readThemePalette(): ThemePalette {
  if (typeof window === "undefined" || typeof document === "undefined") return { ...FALLBACK };
  const computed = window.getComputedStyle(document.documentElement);
  const palette = { ...FALLBACK };
  for (const key of Object.keys(PROPERTY_MAP) as (keyof ThemePalette)[]) {
    const value = computed.getPropertyValue(PROPERTY_MAP[key]).trim();
    if (value) palette[key] = value;
  }
  return palette;
}

export type ThemeChangeListener = () => void;

// Watches the `data-theme` attribute on <html> (the mechanism theme.ts
// actually uses - there is no pub/sub event, ThemeToggle just flips the
// attribute) and invokes the listener whenever it changes. Returns an
// unsubscribe function. Safe to call in non-browser test environments.
export function watchThemeChange(listener: ThemeChangeListener): () => void {
  if (typeof document === "undefined" || typeof MutationObserver === "undefined") {
    return () => {};
  }
  const observer = new MutationObserver((mutations) => {
    for (const mutation of mutations) {
      if (mutation.type === "attributes" && mutation.attributeName === "data-theme") {
        listener();
        return;
      }
    }
  });
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => observer.disconnect();
}

// Reads the live `prefers-reduced-motion` state. Checked directly rather
// than cached, since it can change while the app is open.
export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

// Watches for `prefers-reduced-motion` changes (e.g. the user flips the OS
// setting while the app is open) and invokes the listener. Returns an
// unsubscribe function.
export function watchReducedMotionChange(listener: (reduced: boolean) => void): () => void {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return () => {};
  const media = window.matchMedia("(prefers-reduced-motion: reduce)");
  const handler = () => listener(media.matches);
  media.addEventListener("change", handler);
  return () => media.removeEventListener("change", handler);
}
