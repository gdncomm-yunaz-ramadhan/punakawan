// Typed fetch wrappers for the artifact-review-plan-mutation-v2 endpoints
// (internal/panel/api/{artifact_handler,review_handler,comment_handler}.go),
// mirroring the shapes packages/schema-types/src/generated/artifact*.ts
// describe. GET requests use plain fetch (no session needed, per the
// backend contract); mutating requests go through fetchWithCsrf so the
// CSRF header and 401/403 -> SessionExpiredError mapping are automatic.

import { fetchWithCsrf } from "../session";

export interface ArtifactReference {
  type: "plan" | "retrieval_recipe";
  id: string;
  version: number;
  revision_hash: string;
  workspace_id: string;
  format: "markdown";
  canonical_location?: string;
}

export interface ArtifactContent {
  content: string;
  reference: ArtifactReference;
}

export type ReviewStatus =
  | "draft"
  | "submitted"
  | "queued"
  | "revising"
  | "awaiting_clarification"
  | "proposal_ready"
  | "revision_requested"
  | "accepted"
  | "rejected"
  | "cancelled"
  | "failed"
  | "conflicted";

export interface ArtifactReview {
  metadata: {
    id: string;
    workspace_id: string;
    status: ReviewStatus;
    created_by: string;
    created_at: string;
    updated_at?: string;
  };
  artifact: {
    type: "plan" | "retrieval_recipe";
    id: string;
    version: number;
    revision_hash: string;
  };
  review: {
    title: string;
    instruction?: string;
    comment_count?: number;
  };
}

export type CommentStatus =
  | "open"
  | "addressed"
  | "partially_addressed"
  | "rejected_by_agent"
  | "needs_clarification"
  | "obsolete"
  | "resolved_by_user";

export interface ArtifactCommentAnchor {
  kind: "markdown_block";
  block_id?: string;
  heading_path?: string[];
  base_revision_hash: string;
  quoted_text?: string;
}

export interface ArtifactComment {
  id: string;
  review_id: string;
  author: string;
  status: CommentStatus;
  anchor: ArtifactCommentAnchor;
  body: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function parseJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export async function getArtifactCurrent(type: string, id: string): Promise<ArtifactContent> {
  const res = await fetch(`/api/v1/artifacts/${encodeURIComponent(type)}/${encodeURIComponent(id)}/current`, {
    headers: { Accept: "application/json" },
  });
  return parseJSON<ArtifactContent>(res);
}

export async function createReview(
  type: string,
  id: string,
  body: { title: string; instruction?: string },
): Promise<ArtifactReview> {
  const res = await fetchWithCsrf(`/api/v1/artifacts/${encodeURIComponent(type)}/${encodeURIComponent(id)}/reviews`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return parseJSON<ArtifactReview>(res);
}

export async function getReview(reviewId: string): Promise<ArtifactReview> {
  const res = await fetch(`/api/v1/reviews/${encodeURIComponent(reviewId)}`, {
    headers: { Accept: "application/json" },
  });
  return parseJSON<ArtifactReview>(res);
}

export async function updateReview(
  reviewId: string,
  body: { title?: string; instruction?: string },
): Promise<ArtifactReview> {
  const res = await fetchWithCsrf(`/api/v1/reviews/${encodeURIComponent(reviewId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return parseJSON<ArtifactReview>(res);
}

export async function listComments(reviewId: string): Promise<{ items: ArtifactComment[] }> {
  const res = await fetch(`/api/v1/reviews/${encodeURIComponent(reviewId)}/comments`, {
    headers: { Accept: "application/json" },
  });
  return parseJSON<{ items: ArtifactComment[] }>(res);
}

export interface CreateCommentRequest {
  // Client-generated id (crypto.randomUUID()) so a retried create after a
  // network failure is idempotent: re-posting the same id + body folds to
  // the same logical comment server-side rather than duplicating it.
  id: string;
  anchor: ArtifactCommentAnchor;
  body: string;
}

export async function createComment(reviewId: string, req: CreateCommentRequest): Promise<ArtifactComment> {
  const res = await fetchWithCsrf(`/api/v1/reviews/${encodeURIComponent(reviewId)}/comments`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req),
  });
  return parseJSON<ArtifactComment>(res);
}

export async function updateComment(
  reviewId: string,
  commentId: string,
  body: { body?: string; status?: CommentStatus },
): Promise<ArtifactComment> {
  const res = await fetchWithCsrf(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/comments/${encodeURIComponent(commentId)}`,
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );
  return parseJSON<ArtifactComment>(res);
}

export async function deleteComment(reviewId: string, commentId: string): Promise<void> {
  const res = await fetchWithCsrf(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/comments/${encodeURIComponent(commentId)}`,
    { method: "DELETE" },
  );
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
}

// --- §8 Submit/Cancel/Timeline (submit-and-retrigger phase) ---

export interface ArtifactRevisionRequest {
  metadata: {
    id: string;
    review_id: string;
    submitted_at: string;
    submitted_by: string;
  };
  base_artifact: {
    type: "plan" | "retrieval_recipe";
    id: string;
    version: number;
    revision_hash: string;
  };
  workflow: {
    type: string;
    auto_start?: boolean;
    max_revision_attempts?: number;
    require_final_acceptance?: boolean;
  };
  comments: {
    snapshot_hash: string;
    count: number;
  };
}

export interface RunReference {
  run_id: string;
  parent_task_id: string;
}

export interface SubmitReviewResponse {
  revision_request: ArtifactRevisionRequest;
  run: RunReference;
}

export interface TimelineResponse {
  review: ArtifactReview;
  comment_count: number;
  revision_request?: ArtifactRevisionRequest | null;
  run?: RunReference | null;
}

// submitReview posts an empty body to §8's Automatic Retrigger Flow
// endpoint. Both 200 (idempotent replay) and 201 (first submission)
// resolve the same typed shape - callers treat them identically per the
// backend contract, so this wrapper doesn't surface the status code.
export async function submitReview(reviewId: string): Promise<SubmitReviewResponse> {
  const res = await fetchWithCsrf(`/api/v1/reviews/${encodeURIComponent(reviewId)}/submit`, {
    method: "POST",
  });
  return parseJSON<SubmitReviewResponse>(res);
}

// cancelReview posts to the cancel endpoint. 200 returns the updated
// ArtifactReview (status "cancelled"); calling it again is a no-op that
// still returns 200.
export async function cancelReview(reviewId: string): Promise<ArtifactReview> {
  const res = await fetchWithCsrf(`/api/v1/reviews/${encodeURIComponent(reviewId)}/cancel`, {
    method: "POST",
  });
  return parseJSON<ArtifactReview>(res);
}

// getTimeline is a plain read (no session/CSRF needed, like other GETs)
// returning the review's full recoverable submission-lifecycle state in
// one call - the client never needs to reconnect to anything else.
export async function getTimeline(reviewId: string): Promise<TimelineResponse> {
  const res = await fetch(`/api/v1/reviews/${encodeURIComponent(reviewId)}/timeline`, {
    headers: { Accept: "application/json" },
  });
  return parseJSON<TimelineResponse>(res);
}
