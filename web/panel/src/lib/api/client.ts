// Thin fetch wrapper for /api/v1, mirroring internal/panel/api's Go
// response shapes. Kept deliberately small for Phase 1 (system +
// workspaces only); later phases add sessions/tasks/knowledge/evidence/
// approvals here as their own endpoints land.

export interface SystemInfo {
  panel_version: string;
  punakawan_version: string;
  server_start_time: string;
  read_only: boolean;
  bound_address: string;
  registered_workspaces: number;
  watcher_status: string;
  feature_flags: string[];
}

export type Availability = "available" | "partially_available" | "busy" | "unavailable" | "invalid";

export interface WorkspaceSummary {
  id: string;
  path: string;
  display_name: string;
  availability: Availability;
  repository_count: number;
  active_session_count: number;
  open_task_count: number;
  blocked_task_count: number;
  knowledge_count: number;
  last_activity_at: string;
  pinned: boolean;
}

export interface SourceHealth {
  source: string;
  availability: Availability;
  message?: string;
  checked_at: string;
}

export interface WorkspaceDetail extends WorkspaceSummary {
  health: SourceHealth[];
}

export interface PanelSessionSummary {
  id: string;
  workspace_id: string;
  workflow: string;
  status: string;
  started_at: string;
  updated_at: string;
  initiator?: string;
  objective?: string;
  active_role?: "semar" | "gareng" | "petruk" | "bagong";
  task_counts?: {
    total?: number;
    open?: number;
    in_progress?: number;
    blocked?: number;
    closed?: number;
  };
  evidence_count?: number;
  warning_count?: number;
  error_count?: number;
}

export interface ApprovalRecord {
  id: string;
  run_id: string;
  operation: string;
  target?: string;
  reason?: string;
  requested_by: string;
  status: string;
  created_at: string;
  resolved_at?: string;
  approved_by?: string;
  preview?: string;
  policy_level?: string;
  // approve_command/deny_command are only present on GET .../approvals'
  // response (internal/panel/api/approval_handler.go), and only for a
  // still-pending record - the panel's read-only MVP has no
  // approve/deny endpoint of its own, so this is the concrete next step
  // ("run this in your terminal") it can offer instead.
  approve_command?: string;
  deny_command?: string;
}

export type NeedsAttentionKind =
  | "failed_session"
  | "pending_approval"
  | "blocked_tasks"
  | "unavailable_workspace"
  | "source_failure"
  | "stale_session";

export interface NeedsAttentionItem {
  kind: NeedsAttentionKind;
  workspace_id: string;
  entity_id?: string;
  message: string;
}

export interface Overview {
  active_sessions: PanelSessionSummary[];
  pending_approvals: ApprovalRecord[];
  blocked_tasks: number;
  available_workspaces: number;
  needs_attention: NeedsAttentionItem[];
  workspace_health: WorkspaceSummary[];
  recent_sessions: PanelSessionSummary[];
}

export interface TimelineEvent {
  id: string;
  run_id: string;
  timestamp: string;
  operation: string;
  type: string;
  result: "success" | "failure" | "cancelled" | "timeout";
  role?: "semar" | "gareng" | "petruk" | "bagong";
  task?: string;
  tool?: string;
  adapter?: string;
  approval_id?: string;
  duration_ms?: number;
  repository?: string;
}

export interface SessionDetail extends PanelSessionSummary {
  Timeline?: TimelineEvent[] | null;
}

export interface ContextCapsule {
  id: string;
  task_id: string;
  created_at: string;
  digest: string;
  role: "semar" | "gareng" | "petruk" | "bagong";
  objective: string;
  allowed_tools: string[];
  forbidden_actions: string[];
  relevant_knowledge?: { id: string; summary?: string }[];
  evidence?: { id: string; summary?: string }[];
  token_budget?: number;
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

export function listWorkspaces(): Promise<{ items: WorkspaceSummary[] }> {
  return getJSON<{ items: WorkspaceSummary[] }>("/workspaces");
}

export function getWorkspace(id: string): Promise<WorkspaceDetail> {
  return getJSON<WorkspaceDetail>(`/workspaces/${encodeURIComponent(id)}`);
}

export function getOverview(): Promise<Overview> {
  return getJSON<Overview>("/overview");
}

export interface SessionFilter {
  status?: string;
  workflow?: string;
  role?: string;
  limit?: number;
}

export function listSessions(
  workspaceId: string,
  filter: SessionFilter = {},
): Promise<{ items: PanelSessionSummary[] }> {
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  if (filter.workflow) params.set("workflow", filter.workflow);
  if (filter.role) params.set("role", filter.role);
  if (filter.limit) params.set("limit", String(filter.limit));
  const qs = params.toString();
  return getJSON<{ items: PanelSessionSummary[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/sessions${qs ? `?${qs}` : ""}`,
  );
}

export function getSession(workspaceId: string, sessionId: string): Promise<SessionDetail> {
  return getJSON<SessionDetail>(
    `/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(sessionId)}`,
  );
}

export function listCapsules(workspaceId: string, taskId: string): Promise<{ items: ContextCapsule[] }> {
  return getJSON<{ items: ContextCapsule[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/capsules?task_id=${encodeURIComponent(taskId)}`,
  );
}

export interface TaskDependencyEdge {
  issue_id: string;
  depends_on_id: string;
  type: string;
}

export interface TaskSummary {
  id: string;
  title: string;
  description?: string;
  status: string;
  priority: number;
  issue_type: string;
  owner?: string;
  assignee?: string;
  labels?: string[];
  parent?: string;
  dependencies?: TaskDependencyEdge[];
  created_at: string;
  created_by?: string;
  updated_at: string;
  started_at?: string;
  external_ref?: string;
  board_status: string;
  blocking_reasons?: string[];
  stale: boolean;
}

export interface RelatedTask {
  id: string;
  title: string;
  status: string;
  priority: number;
  issue_type: string;
  dependency_type: string;
}

export interface TaskDetail {
  id: string;
  title: string;
  description?: string;
  acceptance_criteria?: string;
  status: string;
  priority: number;
  issue_type: string;
  owner?: string;
  assignee?: string;
  labels?: string[];
  parent?: string;
  dependencies?: RelatedTask[];
  dependents?: RelatedTask[];
  created_at: string;
  created_by?: string;
  updated_at: string;
  closed_at?: string;
  external_ref?: string;
}

export interface TaskGraphEdge {
  From: string;
  To: string;
  Type: string;
}

export interface TaskGraph {
  Nodes: TaskSummary[];
  Edges: TaskGraphEdge[];
  Cycles: string[][];
}

export interface TaskFilter {
  status?: string;
  priority?: string;
  type?: string;
  blocked?: boolean;
  query?: string;
  limit?: number;
}

export function listTasks(workspaceId: string, filter: TaskFilter = {}): Promise<{ items: TaskSummary[] }> {
  const params = new URLSearchParams();
  if (filter.status) params.set("status", filter.status);
  if (filter.priority) params.set("priority", filter.priority);
  if (filter.type) params.set("type", filter.type);
  if (filter.blocked) params.set("blocked", "true");
  if (filter.query) params.set("query", filter.query);
  if (filter.limit) params.set("limit", String(filter.limit));
  const qs = params.toString();
  return getJSON<{ items: TaskSummary[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/tasks${qs ? `?${qs}` : ""}`);
}

export function getTask(workspaceId: string, taskId: string): Promise<TaskDetail> {
  return getJSON<TaskDetail>(`/workspaces/${encodeURIComponent(workspaceId)}/tasks/${encodeURIComponent(taskId)}`);
}

export function getTaskGraph(workspaceId: string): Promise<TaskGraph> {
  return getJSON<TaskGraph>(`/workspaces/${encodeURIComponent(workspaceId)}/task-graph`);
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

export async function listKnowledge(
  workspaceId: string,
  filter: KnowledgeFilter = {},
): Promise<{ items: (KnowledgeRecord | SearchResult)[] }> {
  const qs = buildKnowledgeQuery(filter);
  return getJSON<{ items: (KnowledgeRecord | SearchResult)[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/knowledge${qs ? `?${qs}` : ""}`,
  );
}

export function getKnowledge(workspaceId: string, knowledgeId: string): Promise<KnowledgeRecord> {
  return getJSON<KnowledgeRecord>(`/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(knowledgeId)}`);
}

export function getKnowledgeRelations(workspaceId: string, knowledgeId: string): Promise<{ items: KnowledgeRecord[] }> {
  return getJSON<{ items: KnowledgeRecord[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(knowledgeId)}/relations`,
  );
}

export function getKnowledgeHistory(workspaceId: string, knowledgeId: string): Promise<{ items: KnowledgeEvent[] }> {
  return getJSON<{ items: KnowledgeEvent[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/knowledge/${encodeURIComponent(knowledgeId)}/history`,
  );
}

export interface GlobalSearchResult {
  WorkspaceID: string;
  Result: SearchResult;
  RRFScore: number;
}

export function globalSearch(
  query: string,
  opts: { type?: string; repo?: string; limit?: number } = {},
): Promise<{ items: GlobalSearchResult[] }> {
  const params = new URLSearchParams({ q: query });
  if (opts.type) params.set("type", opts.type);
  if (opts.repo) params.set("repo", opts.repo);
  if (opts.limit) params.set("limit", String(opts.limit));
  return getJSON<{ items: GlobalSearchResult[] }>(`/search?${params.toString()}`);
}

export type EvidenceRecordType =
  | "source-excerpt"
  | "repository-snapshot"
  | "command-output"
  | "test-report"
  | "playwright-trace"
  | "screenshot"
  | "api-diff"
  | "git-diff"
  | "commit"
  | "user-answer"
  | "approval-record"
  | "external-response";

export interface EvidenceRecord {
  id: string;
  run_id: string;
  task_id?: string;
  type: EvidenceRecordType;
  path?: string;
  content_hash?: string;
  summary?: string;
  created_at: string;
}

export interface DiffSummary {
  files_changed: number;
  insertions: number;
  deletions: number;
  truncated: boolean;
}

export interface EvidenceTextPreview {
  content_type: string;
  text: string;
  offset: number;
  total_size: number;
  truncated: boolean;
  diff_summary: DiffSummary | null;
}

// binaryEvidenceTypes mirrors internal/panel/sources/evidence_source.go's
// binaryEvidenceTypes: these are served as a raw blob (an <img> can point
// straight at evidencePreviewUrl), never as the JSON text-preview shape.
const binaryEvidenceTypes: ReadonlySet<EvidenceRecordType> = new Set(["screenshot", "playwright-trace"]);

export function isBinaryEvidence(type: EvidenceRecordType): boolean {
  return binaryEvidenceTypes.has(type);
}

export function listEvidence(workspaceId: string, sessionId: string): Promise<{ items: EvidenceRecord[] }> {
  return getJSON<{ items: EvidenceRecord[] }>(
    `/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(sessionId)}/evidence`,
  );
}

export function getEvidence(workspaceId: string, evidenceId: string): Promise<EvidenceRecord> {
  return getJSON<EvidenceRecord>(`/workspaces/${encodeURIComponent(workspaceId)}/evidence/${encodeURIComponent(evidenceId)}`);
}

// evidencePreviewUrl builds the preview URL directly (rather than
// fetching through getJSON) so callers can hand it straight to an <img
// src> for binary evidence (screenshots) without round-tripping the
// bytes through JS.
export function evidencePreviewUrl(workspaceId: string, evidenceId: string, opts: { offset?: number; limit?: number } = {}): string {
  const params = new URLSearchParams();
  if (opts.offset) params.set("offset", String(opts.offset));
  if (opts.limit) params.set("limit", String(opts.limit));
  const qs = params.toString();
  return `/api/v1/workspaces/${encodeURIComponent(workspaceId)}/evidence/${encodeURIComponent(evidenceId)}/preview${qs ? `?${qs}` : ""}`;
}

export function getEvidenceTextPreview(
  workspaceId: string,
  evidenceId: string,
  opts: { offset?: number; limit?: number } = {},
): Promise<EvidenceTextPreview> {
  const params = new URLSearchParams();
  if (opts.offset) params.set("offset", String(opts.offset));
  if (opts.limit) params.set("limit", String(opts.limit));
  const qs = params.toString();
  return getJSON<EvidenceTextPreview>(
    `/workspaces/${encodeURIComponent(workspaceId)}/evidence/${encodeURIComponent(evidenceId)}/preview${qs ? `?${qs}` : ""}`,
  );
}

export function listApprovals(workspaceId: string, status?: string): Promise<{ items: ApprovalRecord[] }> {
  const qs = status ? `?status=${encodeURIComponent(status)}` : "";
  return getJSON<{ items: ApprovalRecord[] }>(`/workspaces/${encodeURIComponent(workspaceId)}/approvals${qs}`);
}
