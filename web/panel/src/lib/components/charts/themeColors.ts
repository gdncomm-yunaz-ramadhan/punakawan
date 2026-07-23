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
};

// Fallback palette used when there is no document (SSR-less test contexts
// that skip DOM setup) - mirrors theme.css's light theme so charts/graphs
// still render something sane.
const FALLBACK: ThemePalette = {
  bg: "#f6f7fb",
  surface: "#ffffff",
  surfaceSubtle: "#eef1f6",
  surfaceRaised: "#ffffff",
  text: "#172033",
  textMuted: "#667085",
  border: "#dce2ea",
  borderStrong: "#c7cfdb",
  accent: "#5b5bd6",
  accentHover: "#4b4bc4",
  accentSoft: "#ececfe",
  accentContrast: "#ffffff",
  secondary: "#0f9f9c",
  success: "#16875b",
  warning: "#b76e00",
  danger: "#c7374f",
  info: "#2878c7",
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
