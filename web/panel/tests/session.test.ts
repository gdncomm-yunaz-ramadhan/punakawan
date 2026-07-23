import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  exchangeBootstrapToken,
  fetchWithCsrf,
  getCsrfToken,
  initSessionFromUrl,
  setCsrfToken,
  SessionExpiredError,
} from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken(null);
  window.history.replaceState({}, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("exchangeBootstrapToken", () => {
  it("posts to /api/v1/session/exchange and stores the csrf token", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ csrf_token: "csrf-abc", expires_at: "2026-07-24T00:00:00Z" }),
    );

    const result = await exchangeBootstrapToken("boot-123");

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/session/exchange",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ bootstrap_token: "boot-123" }),
      }),
    );
    expect(result.csrfToken).toBe("csrf-abc");
    expect(getCsrfToken()).toBe("csrf-abc");
  });

  it("throws with the server's error message on failure", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ error: "invalid bootstrap token" }, false, 401),
    );

    await expect(exchangeBootstrapToken("bad-token")).rejects.toThrow("invalid bootstrap token");
  });
});

describe("initSessionFromUrl", () => {
  it("does nothing when there is no bootstrap param", async () => {
    window.history.replaceState({}, "", "/some/path");
    const result = await initSessionFromUrl();
    expect(result.attempted).toBe(false);
    expect(fetch).not.toHaveBeenCalled();
  });

  it("exchanges the token and strips it from the URL", async () => {
    window.history.replaceState({}, "", "/reviews/review-1?bootstrap=tok-xyz&foo=bar");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ csrf_token: "csrf-1", expires_at: "2026-07-24T00:00:00Z" }),
    );

    const result = await initSessionFromUrl();

    expect(result.attempted).toBe(true);
    expect(result.error).toBeUndefined();
    expect(getCsrfToken()).toBe("csrf-1");
    expect(window.location.search).not.toContain("bootstrap");
    expect(window.location.search).toContain("foo=bar");
    expect(window.location.pathname).toBe("/reviews/review-1");
  });

  it("strips the token from the URL even when the exchange fails", async () => {
    window.history.replaceState({}, "", "/?bootstrap=expired-token");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ error: "invalid or expired bootstrap token" }, false, 401),
    );

    const result = await initSessionFromUrl();

    expect(result.attempted).toBe(true);
    expect(result.error).toContain("invalid or expired bootstrap token");
    expect(window.location.search).not.toContain("bootstrap");
  });
});

describe("fetchWithCsrf", () => {
  it("attaches the X-Csrf-Token header on mutating requests", async () => {
    setCsrfToken("csrf-token-1");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ ok: true }));

    await fetchWithCsrf("/api/v1/reviews/review-1", { method: "PATCH", body: "{}" });

    const [, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("X-Csrf-Token")).toBe("csrf-token-1");
  });

  it("throws SessionExpiredError on 401", async () => {
    setCsrfToken("csrf-token-1");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({}, false, 401));

    await expect(fetchWithCsrf("/api/v1/reviews/review-1")).rejects.toThrow(SessionExpiredError);
  });

  it("throws SessionExpiredError on 403", async () => {
    setCsrfToken("csrf-token-1");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({}, false, 403));

    await expect(fetchWithCsrf("/api/v1/reviews/review-1")).rejects.toThrow(SessionExpiredError);
  });

  it("returns the response normally on success", async () => {
    setCsrfToken("csrf-token-1");
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ hello: "world" }));

    const res = await fetchWithCsrf("/api/v1/reviews/review-1");
    expect(res.ok).toBe(true);
  });
});
