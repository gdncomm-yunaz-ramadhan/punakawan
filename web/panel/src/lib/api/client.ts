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
