import { describe, expect, it } from "vitest";
import { getPath, navigate } from "../src/lib/router/router.svelte";

describe("router", () => {
  it("navigate updates getPath and window.location", () => {
    navigate("/workspaces");
    expect(getPath()).toBe("/workspaces");
    expect(window.location.pathname).toBe("/workspaces");
  });

  it("navigate to a workspace detail path round-trips", () => {
    navigate("/workspaces/checkout-platform");
    expect(getPath()).toBe("/workspaces/checkout-platform");
  });
});
