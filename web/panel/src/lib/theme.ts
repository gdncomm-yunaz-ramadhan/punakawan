// Theme preference persistence and resolution (§13.3).
//
// The no-flash bootstrap in index.html cannot import this module - it must
// run standalone, synchronously, before Svelte (or any module graph) has
// loaded, so it duplicates the ~5 lines of resolution logic inline. This
// module is the single source of truth for everything that runs *after*
// that point: ThemeToggle reads/writes through here, and applyTheme() here
// is what re-applies the resolved theme whenever the user changes their
// selection or the OS-level `prefers-color-scheme` changes while `system`
// is selected.

export type ThemePreference = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

export const THEME_STORAGE_KEY = "punakawan.theme";

export function getStoredThemePreference(): ThemePreference {
  if (typeof window === "undefined") return "system";
  const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") return stored;
  return "system";
}

export function setStoredThemePreference(pref: ThemePreference): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(THEME_STORAGE_KEY, pref);
}

export function resolveTheme(pref: ThemePreference): ResolvedTheme {
  if (pref === "system") {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return "light";
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return pref;
}

// Applies a theme preference to the document and persists it. Used by
// ThemeToggle on every selection change.
export function applyTheme(pref: ThemePreference): void {
  setStoredThemePreference(pref);
  if (typeof document === "undefined") return;
  document.documentElement.setAttribute("data-theme", resolveTheme(pref));
}

// Re-resolves and applies the currently stored preference. Useful for
// reacting to `prefers-color-scheme` changes while "system" is selected.
export function reapplyStoredTheme(): void {
  if (typeof document === "undefined") return;
  document.documentElement.setAttribute("data-theme", resolveTheme(getStoredThemePreference()));
}
