// Accent preset persistence and application (§13.3). AccentPicker presets
// override --color-accent/--color-accent-hover/--color-accent-soft/
// --color-accent-contrast only - warning/danger/success tokens are never
// touched here, per the plan's rule that accent selection must not
// recolor those semantics.

export type AccentPresetId = "wayang" | "indigo" | "teal" | "blue" | "violet" | "amber";

export interface AccentPreset {
  id: AccentPresetId;
  label: string;
  light: AccentVars;
  dark: AccentVars;
}

export interface AccentVars {
  "--color-accent": string;
  "--color-accent-hover": string;
  "--color-accent-soft": string;
  "--color-accent-contrast": string;
}

export const ACCENT_STORAGE_KEY = "punakawan.accent";

// Wayang reuses the default --color-accent token values from theme.css
// exactly (the batik indigo-blue), so selecting it (or having no stored
// preference) is visually identical to the un-overridden default theme.
// It is the default preset (see getStoredAccentPreset). The order here
// puts it first so it heads the AccentPicker list.
export const ACCENT_PRESETS: AccentPreset[] = [
  {
    id: "wayang",
    label: "Wayang",
    light: {
      "--color-accent": "#235fb5",
      "--color-accent-hover": "#1b4a90",
      "--color-accent-soft": "#e8eefb",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#7fa8ee",
      "--color-accent-hover": "#a0c1f5",
      "--color-accent-soft": "#1f2b40",
      "--color-accent-contrast": "#10131c",
    },
  },
  {
    id: "indigo",
    label: "Indigo",
    light: {
      "--color-accent": "#5b5bd6",
      "--color-accent-hover": "#4b4bc4",
      "--color-accent-soft": "#ececfe",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#8b8cf6",
      "--color-accent-hover": "#a0a1ff",
      "--color-accent-soft": "#29294f",
      "--color-accent-contrast": "#101321",
    },
  },
  {
    id: "teal",
    label: "Teal",
    light: {
      "--color-accent": "#0f9f9c",
      "--color-accent-hover": "#0c8583",
      "--color-accent-soft": "#e2f7f6",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#35c7c4",
      "--color-accent-hover": "#5cd8d5",
      "--color-accent-soft": "#123433",
      "--color-accent-contrast": "#071b1a",
    },
  },
  {
    id: "blue",
    label: "Blue",
    light: {
      "--color-accent": "#2878c7",
      "--color-accent-hover": "#1f63a3",
      "--color-accent-soft": "#e4effa",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#65a9ef",
      "--color-accent-hover": "#8ac0f4",
      "--color-accent-soft": "#132a41",
      "--color-accent-contrast": "#081420",
    },
  },
  {
    id: "violet",
    label: "Violet",
    light: {
      "--color-accent": "#8b5cf6",
      "--color-accent-hover": "#7c3aed",
      "--color-accent-soft": "#f1eafe",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#a78bfa",
      "--color-accent-hover": "#bda4fb",
      "--color-accent-soft": "#2c2350",
      "--color-accent-contrast": "#120d24",
    },
  },
  {
    id: "amber",
    label: "Amber",
    light: {
      "--color-accent": "#b76e00",
      "--color-accent-hover": "#9a5c00",
      "--color-accent-soft": "#fdf0dc",
      "--color-accent-contrast": "#ffffff",
    },
    dark: {
      "--color-accent": "#e5a940",
      "--color-accent-hover": "#efc06e",
      "--color-accent-soft": "#3a2b0f",
      "--color-accent-contrast": "#1a1305",
    },
  },
];

export function getPreset(id: AccentPresetId): AccentPreset {
  return ACCENT_PRESETS.find((p) => p.id === id) ?? ACCENT_PRESETS[0];
}

export function getStoredAccentPreset(): AccentPresetId {
  if (typeof window === "undefined") return "wayang";
  const stored = window.localStorage.getItem(ACCENT_STORAGE_KEY);
  if (stored && ACCENT_PRESETS.some((p) => p.id === stored)) return stored as AccentPresetId;
  return "wayang";
}

// Applies a preset's light+dark var pairs as inline custom properties on
// <html> and persists the choice, storing both pairs so the no-flash
// bootstrap script in index.html can re-apply the vars without waiting
// for this module to load, and so the vars stay correct across a
// light/dark toggle without needing to re-run AccentPicker's logic.
export function applyAccentPreset(id: AccentPresetId): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(ACCENT_STORAGE_KEY, id);
  setAccentCssVars(getPreset(id));
}

function setAccentCssVars(preset: AccentPreset): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  const resolved = root.getAttribute("data-theme") === "dark" ? preset.dark : preset.light;
  for (const [key, value] of Object.entries(resolved)) {
    root.style.setProperty(key, value);
  }
}

// Re-applies the stored accent preset's vars for the currently resolved
// theme. Call this whenever the resolved light/dark theme changes, since
// each preset has distinct light/dark hex pairs.
export function reapplyStoredAccent(): void {
  setAccentCssVars(getPreset(getStoredAccentPreset()));
}
