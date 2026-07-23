import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  prefersReducedMotion,
  readThemePalette,
  watchThemeChange,
} from "../src/lib/components/charts/themeColors";

beforeEach(() => {
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => {
  document.documentElement.removeAttribute("data-theme");
});

describe("readThemePalette", () => {
  it("resolves a full set of semantic color roles with non-empty string values", () => {
    const palette = readThemePalette();
    for (const value of Object.values(palette)) {
      expect(typeof value).toBe("string");
      expect(value.length).toBeGreaterThan(0);
    }
  });
});

describe("prefersReducedMotion", () => {
  it("returns false when matchMedia reports no preference", () => {
    const spy = vi
      .spyOn(window, "matchMedia")
      .mockImplementation((q: string) => ({ matches: false, media: q }) as unknown as MediaQueryList);
    expect(prefersReducedMotion()).toBe(false);
    spy.mockRestore();
  });

  it("returns true when matchMedia reports a reduced-motion preference", () => {
    const spy = vi
      .spyOn(window, "matchMedia")
      .mockImplementation((q: string) => ({ matches: true, media: q }) as unknown as MediaQueryList);
    expect(prefersReducedMotion()).toBe(true);
    spy.mockRestore();
  });
});

describe("watchThemeChange", () => {
  it("invokes the listener when data-theme changes, and stops after unsubscribing", async () => {
    const listener = vi.fn();
    const unsubscribe = watchThemeChange(listener);

    document.documentElement.setAttribute("data-theme", "dark");
    await new Promise((resolve) => queueMicrotask(() => resolve(undefined)));
    expect(listener).toHaveBeenCalled();

    listener.mockClear();
    unsubscribe();
    document.documentElement.setAttribute("data-theme", "light");
    await new Promise((resolve) => queueMicrotask(() => resolve(undefined)));
    expect(listener).not.toHaveBeenCalled();
  });
});
