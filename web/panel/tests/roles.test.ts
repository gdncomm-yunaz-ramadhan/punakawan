import { describe, it, expect } from "vitest";
import { roleLabel, ROLE_META, isRoleName } from "../src/lib/roles";

describe("roleLabel", () => {
  it("maps each known role token to its proper label", () => {
    expect(roleLabel("semar")).toBe("Semar");
    expect(roleLabel("gareng")).toBe("Gareng");
    expect(roleLabel("petruk")).toBe("Petruk");
    expect(roleLabel("bagong")).toBe("Bagong");
  });

  it("is case-insensitive on known tokens", () => {
    expect(roleLabel("PETRUK")).toBe("Petruk");
    expect(roleLabel("Bagong")).toBe("Bagong");
  });

  it("returns the fallback for empty tokens", () => {
    expect(roleLabel(null)).toBe("—");
    expect(roleLabel(undefined)).toBe("—");
    expect(roleLabel("", "none")).toBe("none");
  });

  it("title-cases an unknown non-empty token instead of dropping it", () => {
    expect(roleLabel("system")).toBe("System");
  });
});

describe("ROLE_META", () => {
  it("covers all four roles with a label, principle, and communication line", () => {
    for (const role of ["semar", "gareng", "petruk", "bagong"] as const) {
      expect(isRoleName(role)).toBe(true);
      expect(ROLE_META[role].label).toBeTruthy();
      expect(ROLE_META[role].principle).toBeTruthy();
      expect(ROLE_META[role].communication).toBeTruthy();
    }
  });
});
