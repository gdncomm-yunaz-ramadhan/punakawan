import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  createComment,
  createReview,
  deleteComment,
  getArtifactCurrent,
  getReview,
  listComments,
  updateComment,
  updateReview,
} from "../src/lib/review/api";
import { setCsrfToken } from "../src/lib/session";

function jsonResponse(body: unknown, ok = true, status = 200) {
  return { ok, status, json: async () => body } as Response;
}

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn());
  setCsrfToken("csrf-test-token");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getArtifactCurrent", () => {
  it("fetches the current version of a plan artifact", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        content: "# Plan\n",
        reference: {
          type: "plan",
          id: "plan-panel",
          version: 3,
          revision_hash: "sha256:abc",
          workspace_id: "ws-1",
          format: "markdown",
        },
      }),
    );

    const result = await getArtifactCurrent("plan", "plan-panel");

    expect(fetch).toHaveBeenCalledWith("/api/v1/artifacts/plan/plan-panel/current", expect.any(Object));
    expect(result.content).toBe("# Plan\n");
    expect(result.reference.version).toBe(3);
  });

  it("throws ApiError with the server message on 404", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({ error: "artifact: plan not found" }, false, 404),
    );

    await expect(getArtifactCurrent("plan", "missing")).rejects.toThrow(ApiError);
  });
});

describe("createReview", () => {
  it("posts title/instruction and attaches the CSRF header", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        metadata: { id: "review-1", workspace_id: "ws-1", status: "draft", created_by: "local", created_at: "now" },
        artifact: { type: "plan", id: "plan-panel", version: 1, revision_hash: "sha256:x" },
        review: { title: "My review" },
      }),
    );

    const review = await createReview("plan", "plan-panel", { title: "My review" });

    const [url, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe("/api/v1/artifacts/plan/plan-panel/reviews");
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("X-Csrf-Token")).toBe("csrf-test-token");
    expect(review.metadata.id).toBe("review-1");
  });
});

describe("getReview / updateReview", () => {
  it("getReview issues a plain GET with no CSRF header", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        metadata: { id: "review-1", workspace_id: "ws-1", status: "draft", created_by: "local", created_at: "now" },
        artifact: { type: "plan", id: "plan-panel", version: 1, revision_hash: "sha256:x" },
        review: { title: "My review" },
      }),
    );
    await getReview("review-1");
    expect(fetch).toHaveBeenCalledWith("/api/v1/reviews/review-1", expect.any(Object));
  });

  it("updateReview PATCHes with the CSRF header", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        metadata: { id: "review-1", workspace_id: "ws-1", status: "draft", created_by: "local", created_at: "now" },
        artifact: { type: "plan", id: "plan-panel", version: 1, revision_hash: "sha256:x" },
        review: { title: "My review", instruction: "Be thorough" },
      }),
    );

    const updated = await updateReview("review-1", { instruction: "Be thorough" });

    const [, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((init as RequestInit).method).toBe("PATCH");
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("X-Csrf-Token")).toBe("csrf-test-token");
    expect(updated.review.instruction).toBe("Be thorough");
  });
});

describe("comments", () => {
  it("listComments fetches the items array", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse({ items: [] }));
    const result = await listComments("review-1");
    expect(result.items).toEqual([]);
  });

  it("createComment sends the client-generated id and anchor shape", async () => {
    const comment = {
      id: "comment-abc",
      review_id: "review-1",
      author: "local",
      status: "open" as const,
      anchor: { kind: "markdown_block" as const, base_revision_hash: "sha256:x", heading_path: ["A"] },
      body: "hello",
    };
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(jsonResponse(comment, true, 201));

    const result = await createComment("review-1", {
      id: "comment-abc",
      anchor: { kind: "markdown_block", base_revision_hash: "sha256:x", heading_path: ["A"] },
      body: "hello",
    });

    const [, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    const sentBody = JSON.parse((init as RequestInit).body as string);
    expect(sentBody.id).toBe("comment-abc");
    expect(sentBody.anchor.kind).toBe("markdown_block");
    expect(result.id).toBe("comment-abc");
  });

  it("updateComment PATCHes body/status", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue(
      jsonResponse({
        id: "comment-abc",
        review_id: "review-1",
        author: "local",
        status: "resolved_by_user",
        anchor: { kind: "markdown_block", base_revision_hash: "sha256:x" },
        body: "hello",
      }),
    );

    const result = await updateComment("review-1", "comment-abc", { status: "resolved_by_user" });
    expect(result.status).toBe("resolved_by_user");
  });

  it("deleteComment issues a DELETE and resolves on 204", async () => {
    (fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({ ok: false, status: 204 } as Response);
    await expect(deleteComment("review-1", "comment-abc")).resolves.toBeUndefined();
    const [, init] = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((init as RequestInit).method).toBe("DELETE");
  });
});

describe("idempotent comment-create retry", () => {
  it("reuses the same client-generated id when retrying after a network failure", async () => {
    const clientId = "comment-retry-1";
    const anchor = { kind: "markdown_block" as const, base_revision_hash: "sha256:x", heading_path: ["A"] };

    (fetch as unknown as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new TypeError("network error"))
      .mockResolvedValueOnce(
        jsonResponse(
          { id: clientId, review_id: "review-1", author: "local", status: "open", anchor, body: "hi" },
          true,
          201,
        ),
      );

    const req = { id: clientId, anchor, body: "hi" };

    await expect(createComment("review-1", req)).rejects.toThrow("network error");
    const result = await createComment("review-1", req);

    const firstBody = JSON.parse((fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0][1].body);
    const secondBody = JSON.parse((fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[1][1].body);
    expect(firstBody.id).toBe(clientId);
    expect(secondBody.id).toBe(clientId);
    expect(firstBody).toEqual(secondBody);
    expect(result.id).toBe(clientId);
  });
});
