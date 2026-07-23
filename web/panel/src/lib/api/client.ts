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
