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
  // "markdown" is a plan's format; "json" is a retrieval_recipe's
  // canonical serialization (internal/recipe.RecipeStore.MarshalCanonical
  // - indented JSON matching `punakawan knowledge recipe show`).
  format: "markdown" | "json";
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
  kind: "markdown_block" | "recipe_field_path";
  block_id?: string;
  heading_path?: string[];
  base_revision_hash: string;
  quoted_text?: string;
  // recipe_field_path anchors only: a dotted gjson-syntax path into the
  // recipe's canonical JSON (e.g. "retrieval_recipe.selector.all.0.value.literal").
  // See internal/artifact/recipefieldpath.go.
  field_path?: string;
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

// --- Artifact Review Phase 5: proposal review (internal/panel/api/proposal_handler.go) ---

// DiffLine mirrors internal/artifact/diff.go's DiffLine struct exactly -
// it carries no json tags, so Go's default marshaling capitalizes the
// field names ("Kind"/"Text"), unlike every other snake_case shape in
// this file. Do not "fix" this to lowercase - it would silently stop
// matching the wire format.
export interface DiffLine {
  Kind: "equal" | "added" | "removed";
  Text: string;
}

export interface DiffSummary {
  added: number;
  removed: number;
}

export interface DiffResponse {
  lines: DiffLine[];
  summary: DiffSummary;
}

export interface ValidationIssue {
  check: string;
  message: string;
}

export interface StructuralReport {
  passed: boolean;
  issues: ValidationIssue[];
}

export interface ReviewComplianceReport {
  passed: boolean;
  issues: ValidationIssue[];
  unresolved_comment_ids?: string[];
}

export interface ValidationResponse {
  structural: StructuralReport;
  compliance: ReviewComplianceReport;
}

// Attempt-level status of one stored proposal record - distinct from
// ReviewStatus, which tracks the review as a whole. "pending"/"failed" are
// not currently produced by CreateProposalHandler (it always writes
// "ready"), but the enum is generated from the JSON schema
// (pkg/protocol/types_generated.go) so all four values are modeled here.
export type ProposalStatus = "pending" | "ready" | "failed" | "superseded";

export type CommentResolutionStatus = "addressed" | "partially_addressed" | "rejected" | "not_applicable";

export interface CommentResolution {
  comment_id: string;
  status: CommentResolutionStatus;
  explanation?: string;
  changed_block_ids?: string[];
}

export type ProposalValidationStatus = "pending" | "passed" | "failed";

export interface ArtifactRevisionProposalResults {
  addressed_comments?: number;
  partially_addressed_comments?: number;
  unresolved_comments?: number;
  validation_status?: ProposalValidationStatus;
  comment_resolutions?: CommentResolution[];
}

export interface ArtifactRevisionProposal {
  metadata: {
    id: string;
    review_id: string;
    revision_request_id: string;
    attempt: number;
    status: ProposalStatus;
  };
  base: {
    artifact_id: string;
    version: number;
    revision_hash: string;
  };
  proposed: {
    version: number;
    content_hash: string;
    content_location: string;
    change_summary?: string;
  };
  results?: ArtifactRevisionProposalResults;
}

// listProposals is a plain read - every stored attempt for the review, in
// order (attempt 1 first), so callers can build the version-lineage view
// or find the latest attempt without a separate "give me the last one"
// endpoint.
export async function listProposals(reviewId: string): Promise<{ items: ArtifactRevisionProposal[] }> {
  const res = await fetch(`/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals`, {
    headers: { Accept: "application/json" },
  });
  return parseJSON<{ items: ArtifactRevisionProposal[] }>(res);
}

// getProposal's proposalId is actually an attempt number (e.g. "1", "2"),
// per the backend's parseAttempt - not an opaque id, despite the name in
// the URL path.
export async function getProposal(reviewId: string, proposalId: string): Promise<ArtifactRevisionProposal> {
  const res = await fetch(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}`,
    { headers: { Accept: "application/json" } },
  );
  return parseJSON<ArtifactRevisionProposal>(res);
}

export async function getProposalDiff(reviewId: string, proposalId: string): Promise<DiffResponse> {
  const res = await fetch(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}/diff`,
    { headers: { Accept: "application/json" } },
  );
  return parseJSON<DiffResponse>(res);
}

// getProposalValidation recomputes both reports live server-side rather
// than reading a stored snapshot (see ProposalValidationHandler's own
// comment) - callers should treat this as the authoritative,
// up-to-the-moment pass/fail state, not just a historical record.
export async function getProposalValidation(reviewId: string, proposalId: string): Promise<ValidationResponse> {
  const res = await fetch(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}/validation`,
    { headers: { Accept: "application/json" } },
  );
  return parseJSON<ValidationResponse>(res);
}

export interface AcceptProposalResponse {
  review: ArtifactReview;
  new_version: unknown;
}

// acceptProposal can 409 for two distinct reasons the caller must tell
// apart: the canonical artifact moved out from under this proposal's base
// (review flips to "conflicted" server-side - the fix is POST
// .../rebase) or the proposal failed validation (the fix is requesting
// changes instead). ApiError only carries a message string, so callers
// that need to branch on which 409 this was should also have already
// checked results.validation_status themselves before calling accept.
export async function acceptProposal(reviewId: string, proposalId: string): Promise<AcceptProposalResponse> {
  const res = await fetchWithCsrf(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}/accept`,
    { method: "POST" },
  );
  return parseJSON<AcceptProposalResponse>(res);
}

// rejectProposal never touches the canonical artifact (§16's
// non-destructive rejection) - it only moves the review to "rejected".
export async function rejectProposal(reviewId: string, proposalId: string): Promise<ArtifactReview> {
  const res = await fetchWithCsrf(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}/reject`,
    { method: "POST" },
  );
  return parseJSON<ArtifactReview>(res);
}

export interface RequestChangesResponse {
  review: ArtifactReview;
  run: RunReference;
}

// requestChanges dispatches another revision attempt under the same
// review (status -> "revision_requested") - the instruction is optional
// free-text appended server-side to the review's existing instruction,
// not a replacement for it.
export async function requestChanges(
  reviewId: string,
  proposalId: string,
  instruction?: string,
): Promise<RequestChangesResponse> {
  const res = await fetchWithCsrf(
    `/api/v1/reviews/${encodeURIComponent(reviewId)}/proposals/${encodeURIComponent(proposalId)}/request-changes`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ instruction }),
    },
  );
  return parseJSON<RequestChangesResponse>(res);
}

// rebaseReview re-points a conflicted review at the artifact's current
// canonical version and resets status to "draft" - it deliberately does
// not touch comments or re-run a revision automatically (§12); the user
// reviews the refreshed document and resubmits explicitly via the normal
// draft flow.
export async function rebaseReview(reviewId: string): Promise<ArtifactReview> {
  const res = await fetchWithCsrf(`/api/v1/reviews/${encodeURIComponent(reviewId)}/rebase`, {
    method: "POST",
  });
  return parseJSON<ArtifactReview>(res);
}
