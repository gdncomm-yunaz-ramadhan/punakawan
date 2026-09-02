// Thin fetch wrapper for /api/v1, mirroring internal/panel/api's Go
// response shapes.

import { fetchWithCsrf } from "../session";
import { ApiError, getJSON, mutateJSON } from "./transport";
import type { DeliveryDetail, DeliverySummary } from "@punakawan/schema-types";

export type { DeliveryDetail, DeliverySummary };
export { ApiError };

export interface SystemInfo {
  panel_version: string;
  punakawan_version: string;
  server_start_time: string;
  read_only: boolean;
  bound_address: string;
  registered_workspaces: number;
}

export type Availability = "available" | "partially_available" | "busy" | "unavailable" | "invalid";

export interface WorkspaceSummary {
  id: string;
  path: string;
  display_name: string;
  availability: Availability;
  repository_count: number;
  active_session_count: number;
  last_activity_at: string;
  pinned: boolean;
  // True only for the single workspace this panel instance serves.
  primary: boolean;
}

export interface SourceHealth {
  source: string;
  availability: Availability;
  message?: string;
  checked_at: string;
}

export function getSystem(): Promise<SystemInfo> {
  return getJSON<SystemInfo>("/system");
}

// --- Projects (Phase 2, plan §5) -----------------------------------------
//
// Projects are becoming the primary entity. A project's snapshot counts
// mirror internal/panel's ProjectSummary; its editable metadata is an
// optimistically-locked list guarded by a monotonically increasing
// `revision` (send the last-loaded revision as base_revision on every
// mutation; a stale one 409s).

export interface ProjectSummary {
  id: string;
  name: string;
  description: string;
  path: string;
  pinned: boolean;
  primary: boolean;
  // A plain string (not the Availability union) per the backend contract;
  // its values happen to overlap Availability, so StatusBadge can render
  // it after a narrowing cast.
  availability: string;
  repository_count: number;
  active_session_count: number;
  metadata_count: number;
}

// A single editable project-metadata field. `value` is intentionally
// `unknown` - metadata holds arbitrary JSON (strings, numbers, objects),
// and the UI stringifies/parses it at the edges.
export interface MetadataEntry {
  key: string;
  description: string;
  value: unknown;
}

export interface ProjectDetail extends ProjectSummary {
  metadata: MetadataEntry[];
  revision: number;
}

// The write endpoints (POST/PATCH) echo the persisted entry plus the new
// revision the caller must carry into its next optimistic-locked write.
export interface MetadataMutationResult {
  entry: MetadataEntry;
  revision: number;
}

// The 400 validation codes the metadata write endpoints can return
// (plan §5). Kept as a union so the UI's message map is exhaustive.
export type MetadataErrorCode = "duplicate_key" | "secret_rejected" | "invalid_value" | "missing_field";

export function listProjects(): Promise<{ items: ProjectSummary[] }> {
  return getJSON<{ items: ProjectSummary[] }>("/projects");
}

export function getProject(id: string): Promise<ProjectDetail> {
  return getJSON<ProjectDetail>(`/projects/${encodeURIComponent(id)}`);
}

export function listMetadata(id: string): Promise<{ items: MetadataEntry[]; revision: number }> {
  return getJSON<{ items: MetadataEntry[]; revision: number }>(`/projects/${encodeURIComponent(id)}/metadata`);
}

export interface AddMetadataRequest {
  key: string;
  description: string;
  value: unknown;
  base_revision: number;
}

export function addMetadata(id: string, body: AddMetadataRequest): Promise<MetadataMutationResult> {
  return mutateJSON<MetadataMutationResult>(`/projects/${encodeURIComponent(id)}/metadata`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export interface UpdateMetadataRequest {
  description?: string;
  value?: unknown;
  base_revision: number;
}

export function updateMetadata(id: string, key: string, body: UpdateMetadataRequest): Promise<MetadataMutationResult> {
  return mutateJSON<MetadataMutationResult>(
    `/projects/${encodeURIComponent(id)}/metadata/${encodeURIComponent(key)}`,
    { method: "PATCH", body: JSON.stringify(body) },
  );
}

// DELETE returns 204 with no body; base_revision rides in the query
// string (the DELETE has no request body) for optimistic locking.
export async function deleteMetadata(id: string, key: string, baseRevision: number): Promise<void> {
  const res = await fetchWithCsrf(
    `/api/v1/projects/${encodeURIComponent(id)}/metadata/${encodeURIComponent(key)}?base_revision=${baseRevision}`,
    { method: "DELETE" },
  );
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}) as { error?: string; message?: string; code?: string });
    throw new ApiError(res.status, body.error ?? body.message ?? res.statusText, body.code);
  }
}

// deleteProject removes a project from this panel's workspace registry, so
// the panel stops listing and serving it. It deletes nothing the project
// owns: the workspace directory, its .punakawan tree, knowledge database,
// tasks, evidence, and repositories all stay on disk, and registering the
// same path again brings it back. Registry rows carry no revision, so there
// is no base_revision to send. Answers 204; 404 for an unknown id and 409
// for the primary workspace, which cannot be removed.
export async function deleteProject(id: string): Promise<void> {
  const res = await fetchWithCsrf(`/api/v1/projects/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({}) as { error?: string; message?: string; code?: string });
    throw new ApiError(res.status, body.error ?? body.message ?? res.statusText, body.code);
  }
}

// --- Project Roles (role prompt preferences) -----------------------------
//
// A project's four Punakawan roles (Semar/Gareng/Petruk/Bagong) each carry a
// `style` (strict|balanced|creative) and free-text `instructions`. That is
// the entire effect a project has on a role's prompt - it never authorizes a
// tool or changes what a workflow requires. Writes are optimistically locked
// with the same monotonically increasing `revision` pattern as project
// metadata: send the last-loaded revision as base_revision on every
// mutation; a stale one 409s (code "revision_conflict").

export interface RolePreference {
  style: string;
  instructions: string;
}

export interface RolesConfiguration {
  semar: RolePreference;
  gareng: RolePreference;
  petruk: RolePreference;
  bagong: RolePreference;
}

export interface RolesResponse {
  roles: RolesConfiguration;
  revision: number;
}

// The write endpoints echo the full role map plus the new revision the
// caller must carry into its next optimistic-locked write.
export interface RolesMutationResult {
  roles: RolesConfiguration;
  revision: number;
}

// The 4xx error codes the role write endpoints can return. Kept as a union
// so the UI's message map stays exhaustive.
export type RoleErrorCode = "revision_conflict" | "unknown_role" | "invalid_style" | "instructions_too_long";

export function getRoles(projectId: string): Promise<RolesResponse> {
  return getJSON<RolesResponse>(`/projects/${encodeURIComponent(projectId)}/roles`);
}

export interface UpdateRolePatch {
  style?: string;
  instructions?: string;
}

export function updateRole(
  projectId: string,
  role: string,
  patch: UpdateRolePatch,
  baseRevision: number,
): Promise<RolesMutationResult> {
  return mutateJSON<RolesMutationResult>(
    `/projects/${encodeURIComponent(projectId)}/roles/${encodeURIComponent(role)}`,
    { method: "PATCH", body: JSON.stringify({ ...patch, base_revision: baseRevision }) },
  );
}

export function resetRole(projectId: string, role: string, baseRevision: number): Promise<RolesMutationResult> {
  return mutateJSON<RolesMutationResult>(
    `/projects/${encodeURIComponent(projectId)}/roles/${encodeURIComponent(role)}/reset`,
    { method: "POST", body: JSON.stringify({ base_revision: baseRevision }) },
  );
}

// --- Project Workflows (Phase 6, plan §6) --------------------------------
//
// A workflow Definition is a declarative, versioned recipe: an ordered
// list of capability steps the run engine executes. It is enabled/disabled
// as a unit and invoked with a set of named inputs; `revision` guards
// concurrent edits the same way project metadata does.

export interface WorkflowInput {
  name: string;
  type: string;
  required?: boolean;
  default?: unknown;
}

export interface WorkflowStep {
  id: string;
  capability: string;
  intent?: string;
  // The ids of earlier steps whose output feeds this one.
  input_from?: string[];
}

export interface WorkflowDefinition {
  version: string;
  id: string;
  name: string;
  description: string;
  enabled: boolean;
  required_metadata?: string[];
  inputs?: WorkflowInput[];
  steps: WorkflowStep[];
  allowed_capabilities?: string[];
  revision: number;
}

// The 400 validation codes the workflow create endpoint can return; kept
// as a union so the UI's message map is exhaustive. "revision_conflict"
// arrives on a 409 rather than a 400.
export type WorkflowErrorCode = "unknown_capability" | "command_not_allowed" | "invalid" | "revision_conflict";

export function listWorkflows(id: string): Promise<{ items: WorkflowDefinition[] }> {
  return getJSON<{ items: WorkflowDefinition[] }>(`/projects/${encodeURIComponent(id)}/workflows`);
}

export function getWorkflow(id: string, workflowId: string): Promise<WorkflowDefinition> {
  return getJSON<WorkflowDefinition>(
    `/projects/${encodeURIComponent(id)}/workflows/${encodeURIComponent(workflowId)}`,
  );
}

export function createWorkflow(id: string, definition: unknown): Promise<WorkflowDefinition> {
  return mutateJSON<WorkflowDefinition>(`/projects/${encodeURIComponent(id)}/workflows`, {
    method: "POST",
    body: JSON.stringify(definition),
  });
}

export function enableWorkflow(id: string, workflowId: string): Promise<WorkflowDefinition> {
  return mutateJSON<WorkflowDefinition>(
    `/projects/${encodeURIComponent(id)}/workflows/${encodeURIComponent(workflowId)}/enable`,
    { method: "POST" },
  );
}

export function disableWorkflow(id: string, workflowId: string): Promise<WorkflowDefinition> {
  return mutateJSON<WorkflowDefinition>(
    `/projects/${encodeURIComponent(id)}/workflows/${encodeURIComponent(workflowId)}/disable`,
    { method: "POST" },
  );
}

export interface InvokeWorkflowResult {
  run_id: string;
}

// invoke queues a run with the given named inputs. NOTE: until the run
// engine wiring lands, the backend answers with an error carrying the
// message "not connected to the run engine" - that surfaces here as a
// normal ApiError the UI shows verbatim. A 409 {code:"disabled"} means the
// workflow must be enabled first.
export function invokeWorkflow(
  id: string,
  workflowId: string,
  inputs: Record<string, unknown>,
): Promise<InvokeWorkflowResult> {
  return mutateJSON<InvokeWorkflowResult>(
    `/projects/${encodeURIComponent(id)}/workflows/${encodeURIComponent(workflowId)}/invoke`,
    { method: "POST", body: JSON.stringify({ inputs }) },
  );
}

// --- Project Plans --------------------------------------------------------
//
// Plans are read-only from the panel's perspective: they are the
// internal/plan aggregate's own immutable plan_revisions, authored and
// versioned through plan_save elsewhere. Here we only list their
// summaries (each with every delivery that links one of its revisions)
// and render one exact revision - the plan's current head by default, or
// the exact revision a specific delivery link named, via ?revision=.

export interface LinkedDeliveryRef {
  orchestration_id: string;
  scope: string;
  plan_revision: number;
}

export interface PlanSummary {
  id: string;
  objective: string;
  status?: string;
  current_revision: number;
  project_ids?: string[];
  linked_deliveries?: LinkedDeliveryRef[];
}

export interface PlanRevision {
  id: string;
  objective: string;
  status?: string;
  revision: number;
  project_ids?: string[];
  legacy_markdown?: string;
  requirements?: string[];
  acceptance_criteria?: string[];
}

export interface PlanDetail {
  plan: PlanRevision;
  linked_deliveries?: LinkedDeliveryRef[];
}

export function listPlans(id: string): Promise<{ items: PlanSummary[] }> {
  return getJSON<{ items: PlanSummary[] }>(`/projects/${encodeURIComponent(id)}/plans`);
}

// getPlan fetches planId's current head revision, or the exact revision
// named by revision when given - a delivery link's own plan_revision, so
// following that link always renders the revision the delivery actually
// used rather than whatever the lineage has moved on to since.
export function getPlan(id: string, planId: string, revision?: number): Promise<PlanDetail> {
  const query = revision !== undefined ? `?${new URLSearchParams({ revision: String(revision) })}` : "";
  return getJSON<PlanDetail>(`/projects/${encodeURIComponent(id)}/plans/${encodeURIComponent(planId)}${query}`);
}

// --- Project Health (Phase 8, plan §8) -----------------------------------
//
// Per-source availability for the project's data sources. GET may serve a
// cached snapshot (X-Cache header, `stale:true`); the refresh endpoint
// forces a fresh probe and returns the same shape.

// A single project data-source health entry. Structurally identical to
// SourceHealth (workspace health) - reusing that shape keeps StatusBadge
// availability rendering consistent across the workspace and project
// health views.
export type HealthEntry = SourceHealth;

export interface HealthResponse {
  health: HealthEntry[];
  stale: boolean;
}

export function getHealth(id: string): Promise<HealthResponse> {
  return getJSON<HealthResponse>(`/projects/${encodeURIComponent(id)}/health`);
}

export function refreshHealth(id: string): Promise<HealthResponse> {
  return mutateJSON<HealthResponse>(`/projects/${encodeURIComponent(id)}/health/refresh`, { method: "POST" });
}

// --- Deliveries (multi-project orchestration) ----------------------------
//
// A delivery's list and detail rows are DeliverySummary/DeliveryDetail
// (generated from protocol/deliverysummary.schema.json and
// protocol/deliverydetail.schema.json into @punakawan/schema-types) -
// the one live projection every delivery route returns. DeliveryDetail
// names the delivery's lanes, since a reader who cannot see them cannot
// tell a delivery that decomposed correctly from one that produced
// nothing, but neither type carries any other scheduler-internal concept
// (blocked counts, pending questions, a lane-derived next action); this
// module only wraps the HTTP calls, it declares no parallel type of its
// own.

export interface ListDeliveriesResult {
  items: DeliverySummary[];
  snapshot_revision: number;
}

export function listDeliveries(): Promise<ListDeliveriesResult> {
  return getJSON<ListDeliveriesResult>("/deliveries");
}

export function getDeliveryDetail(orchestrationId: string, signal?: AbortSignal): Promise<DeliveryDetail> {
  return getJSON<DeliveryDetail>(`/deliveries/${encodeURIComponent(orchestrationId)}`, signal);
}

// watchDeliveryDetail long-polls the daemon (via the panel) for up to
// waitSeconds for orchestrationId's projection_revision to advance past
// sinceRevision, returning the current DeliveryDetail either way. signal
// aborts the in-flight request - the live-refresh loop's cancellation
// point on unmount or a changed orchestrationId.
export function watchDeliveryDetail(
  orchestrationId: string,
  sinceRevision: number,
  waitSeconds: number,
  signal?: AbortSignal,
): Promise<DeliveryDetail> {
  const qs = new URLSearchParams({
    since_revision: String(sinceRevision),
    wait_seconds: String(waitSeconds),
  });
  return getJSON<DeliveryDetail>(`/deliveries/${encodeURIComponent(orchestrationId)}/watch?${qs}`, signal);
}

// deliveryEvidenceUrl builds a lane evidence artifact's raw-bytes URL
// directly, so callers can hand it straight to a plain <a href> link
// without round-tripping the bytes through JS.
export function deliveryEvidenceUrl(orchestrationId: string, evidenceId: string): string {
  return `/api/v1/deliveries/${encodeURIComponent(orchestrationId)}/evidence/${encodeURIComponent(evidenceId)}`;
}

export interface CancelDeliveryRequest {
  expected_revision: number;
  reason?: string;
}

export function cancelDelivery(orchestrationId: string, body: CancelDeliveryRequest): Promise<DeliveryDetail> {
  return mutateJSON<DeliveryDetail>(`/deliveries/${encodeURIComponent(orchestrationId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
