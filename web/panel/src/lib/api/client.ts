// Thin fetch wrapper for /api/v1, mirroring internal/panel/api's Go
// response shapes.

import { fetchWithCsrf } from "../session";

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
  knowledge_count: number;
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

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // Machine-readable error code carried on validation (400) and
    // conflict (409) responses so the UI can render a specific message
    // (e.g. "duplicate_key", "secret_rejected") rather than only the
    // human-readable string. Undefined for errors that carry no code.
    public code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

export function getSystem(): Promise<SystemInfo> {
  return getJSON<SystemInfo>("/system");
}

// --- Panel Settings (runtime pool) ---------------------------------------
//
// Panel-wide runtime settings. Each active project workspace runs its own
// `dolt sql-server`; `max_active_runtimes` caps how many are live at once
// (LRU eviction of idle non-primary projects) and
// `runtime_idle_timeout_seconds` is how long an idle non-primary project
// lingers before it is shut down. The PATCH is a session-gated mutation
// (goes through mutateJSON so it carries the CSRF header); the server
// rejects values < 1 with a 400 whose message surfaces as an ApiError.

export interface PanelSettings {
  max_active_runtimes: number;
  runtime_idle_timeout_seconds: number;
}

export function getPanelSettings(): Promise<PanelSettings> {
  return getJSON<PanelSettings>("/system/settings");
}

export function updatePanelSettings(patch: Partial<PanelSettings>): Promise<PanelSettings> {
  return mutateJSON<PanelSettings>("/system/settings", {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

export interface KnowledgeRelation {
  target: string;
  type: string;
}

// RetrievalRecipeSelectorClause mirrors
// pkg/protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem (and its
// Any-side sibling) structurally: go-jsonschema explodes each nesting
// level into its own named Go type, but every level shares this same
// field/operator/value-or-nested-group shape, so one recursive TS type
// covers the whole two-level-bounded AST rather than naming each level.
export interface RetrievalRecipeSelectorClause {
  field?: string;
  operator?: "equals" | "not_equals" | "phrase_contains" | "contains" | "in" | "not_in" | "greater_than" | "less_than";
  value?: unknown;
  all?: RetrievalRecipeSelectorClause[];
  any?: RetrievalRecipeSelectorClause[];
}

export interface RetrievalRecipeSelector {
  all?: RetrievalRecipeSelectorClause[];
  any?: RetrievalRecipeSelectorClause[];
}

export interface RetrievalRecipeInput {
  name: string;
  type: string;
  required?: boolean;
  default?: string;
}

export interface RetrievalRecipeOrdering {
  field: string;
  direction: "ascending" | "descending";
}

export interface RetrievalRecipeOutput {
  entity_type: string;
  identity_field: string;
  fields: string[];
}

export interface RetrievalRecipeLastExecution {
  status?: "success" | "failure";
  executed_at?: string;
  result_count?: number;
  compiled_query_hash?: string;
  evidence_id?: string;
  provider_request_id?: string;
  session_id?: string;
  task_id?: string;
  bindings?: Record<string, unknown>;
}

export interface RetrievalRecipeValidation {
  status?: "pending" | "passed" | "failed";
  validation_id?: string;
  compiled_query_hash?: string;
  sample_size?: number;
  accepted_at?: string;
  accepted_by?: string;
  accepted_result_count?: number;
  provider_instance_fingerprint?: string;
  evidence_ids?: string[];
}

// RetrievalRecipe mirrors pkg/protocol.KnowledgeRecordRetrievalRecipe -
// present on KnowledgeRecord.retrieval_recipe when
// KnowledgeRecord.type === "retrieval_recipe".
export interface RetrievalRecipe {
  capability: string;
  intent: string;
  provider: string;
  resource: string;
  operation: string;
  read_only: boolean;
  recipe_version?: number;
  selector: RetrievalRecipeSelector;
  inputs?: RetrievalRecipeInput[];
  ordering?: RetrievalRecipeOrdering[];
  output: RetrievalRecipeOutput;
  applies_to?: {
    workspace_ids?: string[];
    repository_ids?: string[];
  };
  last_execution?: RetrievalRecipeLastExecution;
  validation?: RetrievalRecipeValidation;
}

export interface KnowledgeRecord {
  id: string;
  type: string;
  status: string;
  title: string;
  summary?: string;
  content?: string;
  tags?: string[];
  aliases?: string[];
  scope?: {
    project?: string;
    organization?: string;
    module?: string;
    path?: string;
    repository?: string;
  };
  source: {
    provider: string;
    external_id?: string;
    uri?: string;
    version?: unknown;
    section?: string;
    content_hash?: string;
    retrieved_at: string;
  };
  extraction: {
    method: string;
    confidence?: number;
    extractor_version?: string;
  };
  validity: {
    state: string;
    verified_at?: string;
    verified_by?: string[];
  };
  relations?: KnowledgeRelation[];
  superseded_by?: string;
  // Present when type === "retrieval_recipe" (punakawan-procedural-
  // knowledge-retrieval-recipe-plan-final.md Phase 0/5).
  retrieval_recipe?: RetrievalRecipe;
  // Type-specific structured bodies carried by role/context records. The
  // panel renders whichever is present as the record's substance (a record
  // often has no free-form summary/content — its body lives here). Kept as
  // `unknown` because the panel only pretty-prints them generically.
  requirement?: unknown;
  petruk_plan?: unknown;
  context_dossier?: unknown;
  semar_synthesis?: unknown;
  gareng_review?: unknown;
  bagong_review?: unknown;
  convention_profile?: unknown;
}

export interface KnowledgeEvent {
  type: "put" | "supersede" | "delete";
  record_id: string;
  record_type: string;
  superseded_by?: string;
  timestamp: string;
}

export interface SearchMatch {
  Kind: "identifier" | "alias" | "bm25" | "fuzzy" | "related";
  Fields?: string[];
  Terms?: string[];
}

export interface SearchResult {
  Id: string;
  Title: string;
  Summary: string;
  Type: string;
  Score: number;
  Match: SearchMatch;
  Explanation?: string[];
  Record: KnowledgeRecord;
}

export interface KnowledgeFilter {
  type?: string;
  state?: string;
  repository?: string;
  source?: string;
  stale?: boolean;
  has_relation?: boolean;
  has_conflict?: boolean;
  q?: string;
  limit?: number;
}

function buildKnowledgeQuery(filter: KnowledgeFilter): string {
  const params = new URLSearchParams();
  if (filter.type) params.set("type", filter.type);
  if (filter.state) params.set("state", filter.state);
  if (filter.repository) params.set("repository", filter.repository);
  if (filter.source) params.set("source", filter.source);
  if (filter.stale) params.set("stale", "true");
  if (filter.has_relation) params.set("has_relation", "true");
  if (filter.has_conflict) params.set("has_conflict", "true");
  if (filter.q) params.set("q", filter.q);
  if (filter.limit) params.set("limit", String(filter.limit));
  return params.toString();
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
  knowledge_count: number;
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

// mutateJSON is the write-side sibling of getJSON: it goes through
// fetchWithCsrf (attaching the session CSRF header and mapping 401/403 to
// SessionExpiredError) and, on any other non-2xx, throws an ApiError that
// preserves both the server's `code` (409 conflict / 400 validation) and
// its human-readable message so the UI can branch on either.
async function mutateJSON<T>(path: string, init: RequestInit): Promise<T> {
  const res = await fetchWithCsrf(`/api/v1${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", Accept: "application/json", ...(init.headers ?? {}) },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { error?: string; message?: string; code?: string });
    throw new ApiError(res.status, body.error ?? body.message ?? res.statusText, body.code);
  }
  return res.json() as Promise<T>;
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

// --- Project Roles (role configuration) ----------------------------------
//
// A project's four Punakawan roles (Semar/Gareng/Petruk/Bagong) each carry
// an enabled flag, a `style` (strict|balanced|creative), a `mode`
// (assist|propose|execute) and a set of capability toggles. Writes are
// optimistically locked with the same monotonically increasing `revision`
// pattern as project metadata: send the last-loaded revision as
// base_revision on every mutation; a stale one 409s (code
// "revision_conflict"). `owned` declares which capability keys each role is
// allowed to render — a role never shows another role's toggles.

export interface RoleConfig {
  enabled: boolean;
  style: string;
  mode: string;
  capabilities: Record<string, boolean>;
}

export interface RolesConfiguration {
  semar: RoleConfig;
  gareng: RoleConfig;
  petruk: RoleConfig;
  bagong: RoleConfig;
}

// The capability toggle keys a given role owns (and may render). Only these
// keys are shown for that role.
export interface RoleCapabilityInfo {
  role: string;
  capabilities: string[];
}

export interface RolesResponse {
  roles: RolesConfiguration;
  revision: number;
  owned: RoleCapabilityInfo[];
}

// The write endpoints echo the full role map plus the new revision the
// caller must carry into its next optimistic-locked write.
export interface RolesMutationResult {
  roles: RolesConfiguration;
  revision: number;
}

// The 4xx error codes the role write endpoints can return. Kept as a union
// so the UI's message map stays exhaustive.
export type RoleErrorCode =
  | "revision_conflict"
  | "unknown_role"
  | "invalid_style"
  | "invalid_mode"
  | "unowned_capability";

export function getRoles(projectId: string): Promise<RolesResponse> {
  return getJSON<RolesResponse>(`/projects/${encodeURIComponent(projectId)}/roles`);
}

export interface UpdateRolePatch {
  enabled?: boolean;
  style?: string;
  mode?: string;
  capabilities?: Record<string, boolean>;
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

// --- Project Plans (Phase 7, plan §7) ------------------------------------
//
// Plans are read-only from the panel's perspective: they are authored and
// versioned through the review protocol elsewhere. Here we only list their
// summaries and render a selected plan's manifest + current version text.

export interface PlanDerivedFrom {
  knowledge?: string[];
  workflows?: string[];
  metadata?: string[];
}

export interface PlanSummary {
  id: string;
  title: string;
  status: string;
  current_version: string;
  related_tasks?: string[];
  derived_from?: PlanDerivedFrom;
}

// The plan manifest carries at least the summary fields; the backend may
// attach further descriptive fields, so the known summary shape is
// extended rather than re-listed. Optional fields are surfaced when
// present and skipped otherwise.
export interface PlanManifest extends PlanSummary {
  description?: string;
  versions?: string[];
}

export interface PlanDetail {
  manifest: PlanManifest;
  current_version_content?: string;
}

export function listPlans(id: string): Promise<{ items: PlanSummary[] }> {
  return getJSON<{ items: PlanSummary[] }>(`/projects/${encodeURIComponent(id)}/plans`);
}

export function getPlan(id: string, planId: string): Promise<PlanDetail> {
  return getJSON<PlanDetail>(`/projects/${encodeURIComponent(id)}/plans/${encodeURIComponent(planId)}`);
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

// --- Project-scoped knowledge reads ---------------------------------------

export function listProjectKnowledge(
  id: string,
  filter: KnowledgeFilter = {},
): Promise<{ items: (KnowledgeRecord | SearchResult)[] }> {
  const qs = buildKnowledgeQuery(filter);
  return getJSON<{ items: (KnowledgeRecord | SearchResult)[] }>(
    `/projects/${encodeURIComponent(id)}/knowledge${qs ? `?${qs}` : ""}`,
  );
}

export function getProjectKnowledge(id: string, knowledgeId: string): Promise<KnowledgeRecord> {
  return getJSON<KnowledgeRecord>(
    `/projects/${encodeURIComponent(id)}/knowledge/${encodeURIComponent(knowledgeId)}`,
  );
}

export function getProjectKnowledgeRelations(id: string, knowledgeId: string): Promise<{ items: KnowledgeRecord[] }> {
  return getJSON<{ items: KnowledgeRecord[] }>(
    `/projects/${encodeURIComponent(id)}/knowledge/${encodeURIComponent(knowledgeId)}/relations`,
  );
}

export function getProjectKnowledgeHistory(id: string, knowledgeId: string): Promise<{ items: KnowledgeEvent[] }> {
  return getJSON<{ items: KnowledgeEvent[] }>(
    `/projects/${encodeURIComponent(id)}/knowledge/${encodeURIComponent(knowledgeId)}/history`,
  );
}

// --- Deliveries (multi-project orchestration) ----------------------------
//
// A DeliveryOrchestration coordinates one lane per parent task across
// however many projects it touches. DeliveryView (internal/delivery
// /deliveryview.go) is the single read model every delivery endpoint
// returns; its Projects/Lanes/Blockers are reduced summaries derived from
// protocol.DeliveryLane, not that full struct.

export type DeliveryOrchestrationStatus = "pending" | "active" | "cancelled" | "completed";

export interface DeliveryOrchestration {
  id: string;
  // Optional because orchestrations stored before titles existed carry none.
  // A delivery's id is an opaque hash, so the UI must always be able to
  // derive a readable label instead of depending on this being set.
  title?: string;
  revision: number;
  status: DeliveryOrchestrationStatus;
  unresolved_inputs: unknown[];
  created_at: string;
  updated_at: string;
  workflow_definition_id?: string;
}

export type DeliveryLaneStatus = "accepted" | "blocked" | "failed" | "leased" | "review" | "runnable" | "running" | "waiting";

export interface DeliveryProjectSummary {
  project_id: string;
  project_slug: string;
  // Distinguishes the two ways a project shows up here. True means the
  // delivery explicitly attached it, so it is listed even with no lanes at
  // all. False means it only appears because some lane names it - including a
  // project detached after its lanes finished, whose completed work the
  // delivery still reports. Always sent, so the UI never has to guess.
  attached: boolean;
  // Always sent, empty rather than absent, for a project with no lanes.
  lane_ids: string[];
  counts_by_status: Partial<Record<DeliveryLaneStatus, number>>;
}

export interface DeliveryEvidenceRef {
  id: string;
  kind: string;
  media_type: string;
  byte_size: number;
  content_hash: string;
}

export interface DeliveryLaneSummary {
  lane_id: string;
  project_id: string;
  parent_task_id?: string;
  status: DeliveryLaneStatus;
  blocked_by?: string[];
  pr_url?: string;
  pr_number?: number;
  pr_provider?: string;
  repository?: string;
  branch?: string;
  commits?: string[];
  worker?: string;
  worktree_path?: string;
  base_sha?: string;
  base_remote?: string;
  semar_record_id?: string;
  gareng_record_id?: string;
  petruk_record_id?: string;
  bagong_record_id?: string;
  attempt?: number;
  repair_cycle_count?: number;
  escalated_at?: string;
  // The session that opened this lane. Deliberately a different question from
  // `worker`, which is whoever holds the lane's lease right now - the two are
  // never interchangeable and must not be shown as one thing.
  session_id?: string;
  evidence?: DeliveryEvidenceRef[];
  verification?: DeliveryVerificationMatrix;
  bagong_review?: DeliveryReviewConclusion;
}

export interface DeliveryVerificationMatrix {
  computed_at: string;
  dimensions: Array<{
    name: "logic" | "unit" | "integration" | "quality" | "e2e" | "ci";
    status: "pending" | "passed" | "failed";
    evidence_id?: string;
    summary?: string;
    checked_at?: string;
  }>;
}

export interface DeliveryReviewConclusion {
  outcome: "approved" | "blocked" | "changes_requested";
  independence_level: "different_session" | "different_worker" | "same_session";
  reviewer_worker_id: string;
  reviewer_session_id: string;
  reviewer_model?: string;
  reviewer_provider?: string;
  blocking_finding_ids: string[];
  evidence_ids: string[];
  recorded_at: string;
}

export interface DeliveryAuditEvent {
  sequence: number;
  type: string;
  entity_id?: string;
  occurred_at: string;
}

export interface DeliveryJiraActivity {
  event_type: string;
  entity_id?: string;
  issue_key: string;
  fired_at: string;
}

export interface DeliveryBlockerSummary {
  lane_id: string;
  parent_task_id?: string;
  blocked_by: string[];
}

export type ApprovalManifestStatus = "pending" | "approved" | "rejected";

export interface ApprovalManifestCheck {
  name: string;
  status: string;
  classification: string;
  detail?: string;
}

export interface ApprovalManifestWorklogEntry {
  bucket: string;
  hours: number;
  subtask_key: string;
  subtask_name?: string;
}

export interface ApprovalManifest {
  id: string;
  orchestration_id: string;
  project_id: string;
  parent_task_ids: string[];
  planned_base_ref: string;
  planned_branches?: string[];
  checks: ApprovalManifestCheck[];
  expects_commits?: boolean;
  expects_jira_writes?: boolean;
  expects_prs?: boolean;
  expects_pushes?: boolean;
  proposed_worklog?: ApprovalManifestWorklogEntry[];
  proposed_worklog_total_hours?: number;
  proposed_worklog_unmapped_hours?: number;
  status: ApprovalManifestStatus;
  approved_by?: string;
  created_at: string;
  decided_at?: string;
  revision: number;
}

export interface DeliveryWorkLog {
  id: string;
  orchestration_id: string;
  lane_id: string;
  parent_task_id?: string;
  session_id?: string;
  jira_issue_key: string;
  started_at: string;
  duration_seconds: number;
  summary: string;
  sync_status: "pending" | "synced" | "failed";
  jira_worklog_id?: string;
  synced_at?: string;
  created_at: string;
}

export interface DeliveryLifecycle {
  case: { id: string; jira_source_key: string; jira_issue_key: string; status: string; created_at: string; updated_at: string };
  execution: { id: string; case_id: string; orchestration_id: string; ordinal: number; status: string; session_id?: string; started_at: string; ended_at?: string };
  sessions: { id: string; case_id: string; execution_id: string; orchestration_id: string; resumed_from_id?: string; participant: string; worktree_path?: string; provider?: string; status: string; started_at: string; ended_at?: string }[];
  checkpoints: { id: string; case_id: string; execution_id: string; session_id: string; sequence: number; summary: string; progress_percent?: number; handoff_to?: string; created_at: string }[];
  usage: { id: string; case_id: string; execution_id: string; session_id: string; kind: string; category: string; model?: string; quantity: number; unit: string; unit_price?: number; cost_amount?: number; cost_currency?: string; price_source?: string; recorded_at: string }[];
  budgets: { id: string; case_id: string; execution_id: string; session_id?: string; category?: string; amount: number; currency: string; created_at: string }[];
  jira_snapshots: { id: string; case_id: string; execution_id: string; session_id?: string; jira_issue_key: string; version: number; title: string; body: string; content_hash: string; captured_at: string }[];
  jira_assessments: { id: string; case_id: string; execution_id: string; session_id?: string; snapshot_id?: string; clarity: string; approval: string; rationale: string; assessed_at: string }[];
  jira_work_items: { id: string; case_id: string; execution_id: string; session_id?: string; orchestration_id: string; parent_task_id: string; requirement_source_id: string; jira_issue_key: string; created_at: string }[];
  jira_write_intents: { id: string; case_id: string; execution_id: string; session_id?: string; jira_issue_key: string; action: string; payload: Record<string, unknown>; idempotency_key: string; status: string; attempt_count: number; retry_at?: string; last_error?: string; external_id?: string; created_at: string; updated_at: string }[];
  progress_reports: { id: string; case_id: string; execution_id: string; session_id: string; summary: string; progress_percent?: number; reported_at: string }[];
  known_cost_by_currency: Record<string, number>;
  unknown_priced_usage: boolean;
}

export interface DeliveryProjectPlanLink {
  project_id: string;
  plan_id: string;
  plan_revision: number;
  created_at: string;
}

export interface DeliveryView {
  orchestration: DeliveryOrchestration;
  // Never empty: whatever title the delivery was started with, or one derived
  // from its own requirement references when it was started without one. The
  // UI can show it directly instead of choosing a fallback of its own.
  title: string;
  // None of these three is ever derived, so an absent one honestly means
  // nobody recorded it: no prose was written, no final plan was submitted, no
  // session drove the delivery.
  description?: string;
  plan_record_id?: string;
  plan_id?: string;
  plan_revision?: number;
  session_id?: string;
  projects: DeliveryProjectSummary[];
  project_plans: DeliveryProjectPlanLink[];
  lanes: DeliveryLaneSummary[];
  blockers: DeliveryBlockerSummary[];
  pending_approvals: ApprovalManifest[];
  pending_questions: string[];
  next_action: string;
  timeline?: DeliveryAuditEvent[];
  jira_activity?: DeliveryJiraActivity[];
  worklogs: DeliveryWorkLog[];
  worklog_seconds: number;
  lifecycle?: DeliveryLifecycle;
  latest_seq: number;
  newly_runnable_lane_ids: string[];
}

export function listDeliveries(): Promise<{ items: DeliveryOrchestration[] }> {
  return getJSON<{ items: DeliveryOrchestration[] }>("/deliveries");
}

export function getDeliveryView(orchestrationId: string, sinceSeq?: number): Promise<DeliveryView> {
  const qs = sinceSeq ? `?since_seq=${encodeURIComponent(String(sinceSeq))}` : "";
  return getJSON<DeliveryView>(`/deliveries/${encodeURIComponent(orchestrationId)}${qs}`);
}

// deliveryEvidenceUrl builds a lane evidence artifact's raw-bytes URL
// directly (mirroring evidencePreviewUrl above), so callers can hand it
// straight to a plain <a href> link without round-tripping the bytes
// through JS.
export function deliveryEvidenceUrl(orchestrationId: string, evidenceId: string): string {
  return `/api/v1/deliveries/${encodeURIComponent(orchestrationId)}/evidence/${encodeURIComponent(evidenceId)}`;
}

// answerDeliveryQuestion resolves one entry from DeliveryView.pending_questions
// - reference is that entry's exact string. Set provider (+ external_id/url/
// title/summary) to resolve it as a requirement source, or parent_task_id +
// project_id to route it directly to a task instead (deliveries_handler.go's
// answerDeliveryQuestionBody documents both shapes).
export interface AnswerDeliveryQuestionRequest {
  reference: string;
  expected_revision?: number;
  provider?: string;
  external_id?: string;
  url?: string;
  title?: string;
  summary?: string;
  parent_task_id?: string;
  project_id?: string;
}

export function answerDeliveryQuestion(orchestrationId: string, body: AnswerDeliveryQuestionRequest): Promise<DeliveryView> {
  return mutateJSON<DeliveryView>(`/deliveries/${encodeURIComponent(orchestrationId)}/answer-question`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

// approveProjectDelivery resolves one project's ApprovalManifest
// independently of any other project's - reject:false approves,
// reject:true rejects. There is no project_id field: manifest_id alone
// identifies which project's approval this decides.
export interface ApproveProjectDeliveryRequest {
  manifest_id: string;
  approved_by: string;
  reject?: boolean;
}

export function approveProjectDelivery(orchestrationId: string, body: ApproveProjectDeliveryRequest): Promise<DeliveryView> {
  return mutateJSON<DeliveryView>(`/deliveries/${encodeURIComponent(orchestrationId)}/approve`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}

export interface CancelDeliveryRequest {
  expected_revision: number;
  reason?: string;
}

export function cancelDelivery(orchestrationId: string, body: CancelDeliveryRequest): Promise<DeliveryView> {
  return mutateJSON<DeliveryView>(`/deliveries/${encodeURIComponent(orchestrationId)}/cancel`, {
    method: "POST",
    body: JSON.stringify(body),
  });
}
