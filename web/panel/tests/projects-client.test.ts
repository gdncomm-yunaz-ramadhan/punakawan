import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  addMetadata,
  deleteMetadata,
  getProject,
  listMetadata,
  listProjects,
  updateMetadata,
} from "../src/lib/api/client";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

type FetchMock = ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("project reads", () => {
  it("listProjects GETs /api/v1/projects", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [] }));
    const result = await listProjects();
    expect(fetch).toHaveBeenCalledWith("/api/v1/projects", expect.any(Object));
    expect(result.items).toEqual([]);
  });

  it("getProject GETs the encoded project id", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({ id: "a/b", name: "AB", metadata: [], revision: 2 }),
    );
    const p = await getProject("a/b");
    expect(fetch).toHaveBeenCalledWith("/api/v1/projects/a%2Fb", expect.any(Object));
    expect(p.revision).toBe(2);
  });

  it("listMetadata GETs the metadata sub-resource", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ items: [], revision: 7 }));
    const res = await listMetadata("p1");
    expect(fetch).toHaveBeenCalledWith("/api/v1/projects/p1/metadata", expect.any(Object));
    expect(res.revision).toBe(7);
  });
});

describe("addMetadata", () => {
  it("POSTs key/description/value/base_revision with the CSRF header", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({ entry: { key: "env", description: "d", value: "prod" }, revision: 8 }, true, 201),
    );

    const res = await addMetadata("p1", { key: "env", description: "d", value: "prod", base_revision: 7 });

    const [url, init] = (fetch as unknown as FetchMock).mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1/metadata");
    expect((init as RequestInit).method).toBe("POST");
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("X-Csrf-Token")).toBe("csrf-test-token");
    expect(headers.get("Content-Type")).toBe("application/json");
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body).toEqual({ key: "env", description: "d", value: "prod", base_revision: 7 });
    expect(res.revision).toBe(8);
  });

  it("maps a 400 duplicate_key response to ApiError carrying status + code", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({ code: "duplicate_key", error: "key already exists" }, false, 400),
    );

    const err = await addMetadata("p1", { key: "env", description: "", value: "x", base_revision: 1 }).catch(
      (e) => e,
    );
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.code).toBe("duplicate_key");
    expect(err.message).toBe("key already exists");
  });

  it("maps a 409 conflict to ApiError with status 409", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({ error: "revision changed" }, false, 409),
    );

    const err = await addMetadata("p1", { key: "env", description: "", value: "x", base_revision: 1 }).catch(
      (e) => e,
    );
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
  });
});

describe("updateMetadata", () => {
  it("PATCHes description/value/base_revision at the encoded key", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(
      jsonResponse({ entry: { key: "a/b", description: "d2", value: 3 }, revision: 9 }),
    );

    const res = await updateMetadata("p1", "a/b", { description: "d2", value: 3, base_revision: 8 });

    const [url, init] = (fetch as unknown as FetchMock).mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1/metadata/a%2Fb");
    expect((init as RequestInit).method).toBe("PATCH");
    const body = JSON.parse((init as RequestInit).body as string);
    expect(body).toEqual({ description: "d2", value: 3, base_revision: 8 });
    expect(res.revision).toBe(9);
  });
});

describe("deleteMetadata", () => {
  it("DELETEs with base_revision in the query string and resolves on 204", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue({ ok: false, status: 204 } as Response);

    await expect(deleteMetadata("p1", "env", 9)).resolves.toBeUndefined();

    const [url, init] = (fetch as unknown as FetchMock).mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1/metadata/env?base_revision=9");
    expect((init as RequestInit).method).toBe("DELETE");
  });

  it("maps a non-204 error response to ApiError", async () => {
    (fetch as unknown as FetchMock).mockResolvedValue(jsonResponse({ error: "revision changed" }, false, 409));
    const err = await deleteMetadata("p1", "env", 1).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(409);
  });
});
