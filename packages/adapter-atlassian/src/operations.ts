import type { AtlassianMcpClient } from './mcpClient.js';
import { normalizeConfluencePage, normalizeJiraIssue, type NormalizedJiraIssue } from './normalize.js';

/**
 * `execute` handlers for each operation declared in the manifest. Each
 * calls the corresponding real Atlassian MCP tool (names confirmed via
 * Context7 `/atlassian/atlassian-mcp-server`) and returns both the raw tool
 * result and this adapter's own normalization, per
 * punakawan-go-typescript-detailed-plan.md §13.2.
 */

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

/** Extracts a structured JSON payload from a CallToolResult, preferring structuredContent. */
function extractPayload(result: { content: unknown; structuredContent?: Record<string, unknown> }): Record<string, unknown> {
  if (result.structuredContent) return result.structuredContent;

  const blocks = Array.isArray(result.content) ? result.content : [];
  for (const block of blocks) {
    if (block && typeof block === 'object' && (block as { type?: unknown }).type === 'text') {
      const text = (block as { text?: unknown }).text;
      if (typeof text === 'string') {
        try {
          return asRecord(JSON.parse(text));
        } catch {
          // Not JSON; fall through to try other blocks.
        }
      }
    }
  }
  return {};
}

export interface GetJiraIssueParams {
  issueIdOrKey: string;
}

export async function getJiraIssue(client: AtlassianMcpClient, params: GetJiraIssueParams) {
  const cloudId = await client.getCloudId();
  const result = await client.callTool('getJiraIssue', { cloudId, issueIdOrKey: params.issueIdOrKey });
  const payload = extractPayload(result);
  return { raw: result, normalized: normalizeJiraIssue(payload, cloudId) };
}

export interface GetConfluencePageParams {
  pageId: string;
  contentFormat?: string;
}

export async function getConfluencePage(client: AtlassianMcpClient, params: GetConfluencePageParams) {
  const cloudId = await client.getCloudId();
  const args: Record<string, unknown> = { cloudId, pageId: params.pageId };
  if (params.contentFormat) args.contentFormat = params.contentFormat;

  const result = await client.callTool('getConfluencePage', args);
  const payload = extractPayload(result);
  return { raw: result, normalized: normalizeConfluencePage(payload, cloudId) };
}

export interface SearchJiraParams {
  jql: string;
  fields?: string[];
  maxResults?: number;
}

export async function searchJira(client: AtlassianMcpClient, params: SearchJiraParams) {
  const cloudId = await client.getCloudId();
  const args: Record<string, unknown> = { cloudId, jql: params.jql };
  if (params.fields) args.fields = params.fields;
  if (params.maxResults !== undefined) args.maxResults = params.maxResults;

  const result = await client.callTool('searchJiraIssuesUsingJql', args);
  const payload = extractPayload(result);
  const issues = Array.isArray(payload.issues) ? payload.issues : [];
  return {
    raw: result,
    normalized: issues.map((issue) => normalizeJiraIssue(asRecord(issue), cloudId)),
  };
}

export interface SearchConfluenceParams {
  cql: string;
}

export async function searchConfluence(client: AtlassianMcpClient, params: SearchConfluenceParams) {
  const cloudId = await client.getCloudId();
  const result = await client.callTool('searchConfluenceUsingCql', { cloudId, cql: params.cql });
  const payload = extractPayload(result);
  const pages = Array.isArray(payload.results) ? payload.results : [];
  return {
    raw: result,
    normalized: pages.map((page) => normalizeConfluencePage(asRecord(page), cloudId)),
  };
}

export interface AddJiraCommentParams {
  issueIdOrKey: string;
  commentBody: string;
}

export async function addJiraComment(client: AtlassianMcpClient, params: AddJiraCommentParams) {
  const result = await client.callTool('addCommentToJiraIssue', {
    cloudId: await client.getCloudId(),
    issueIdOrKey: params.issueIdOrKey,
    commentBody: params.commentBody,
  });
  const payload = extractPayload(result);
  return { raw: result, commentId: typeof payload.id === 'string' ? payload.id : undefined };
}

/**
 * A single available workflow transition, as returned by
 * `getTransitionsForJiraIssue`.
 *
 * ASSUMPTION (pending verification against a real MCP server): the exact
 * response shape of this tool is not documented beyond "list available
 * workflow transitions for an issue" (support.atlassian.com/atlassian-rovo-mcp-server/docs/supported-tools/).
 * Jira's underlying REST API (`GET /issue/{id}/transitions`) shapes its
 * response as `{ transitions: [{ id, name, to: { id, name } }] }`, and since
 * this MCP tool almost certainly wraps that REST endpoint 1:1, we assume the
 * same field names here: each transition has a string `id`, a string `name`,
 * and a `to` object describing the destination status (`id`/`name`). If the
 * real MCP tool's response differs, only `extractTransitions` below should
 * need correcting.
 */
export interface JiraTransition {
  id: string;
  name: string;
  toStatus: { id: string | undefined; name: string | undefined };
  raw: Record<string, unknown>;
}

function extractTransitions(payload: Record<string, unknown>): JiraTransition[] {
  const transitions = Array.isArray(payload.transitions) ? payload.transitions : [];
  return transitions.map((entry) => {
    const record = asRecord(entry);
    const to = asRecord(record.to);
    return {
      id: asString(record.id) ?? '',
      name: asString(record.name) ?? '',
      toStatus: { id: asString(to.id), name: asString(to.name) },
      raw: record,
    };
  });
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

export interface GetTransitionsForJiraIssueParams {
  issueIdOrKey: string;
}

export async function getTransitionsForJiraIssue(client: AtlassianMcpClient, params: GetTransitionsForJiraIssueParams) {
  const result = await client.callTool('getTransitionsForJiraIssue', {
    cloudId: await client.getCloudId(),
    issueIdOrKey: params.issueIdOrKey,
  });
  const payload = extractPayload(result);
  return { raw: result, transitions: extractTransitions(payload) };
}

export interface TransitionJiraIssueParams {
  issueIdOrKey: string;
  transitionId: string;
}

/**
 * Performs a workflow transition on a Jira issue. Transitions are id-based,
 * not status-name-based, because the same target status can be reachable via
 * different transition ids depending on the issue's workflow — callers must
 * discover the id first via `getTransitionsForJiraIssue`.
 */
export async function transitionJiraIssue(client: AtlassianMcpClient, params: TransitionJiraIssueParams) {
  const result = await client.callTool('transitionJiraIssue', {
    cloudId: await client.getCloudId(),
    issueIdOrKey: params.issueIdOrKey,
    transitionId: params.transitionId,
  });
  const payload = extractPayload(result);
  return { raw: result, payload };
}

export interface EditJiraIssueFieldsParams {
  issueIdOrKey: string;
  /**
   * Arbitrary Jira fields map, e.g. `{"customfield_10016": 5}` for a
   * site-specific story-points custom field, or
   * `{"timetracking": {"originalEstimate": "8h"}}` for Jira's native
   * original-estimate field. This operation is a thin, honest passthrough —
   * it does not know or hardcode which custom field id means "story points"
   * for any given Jira site (that varies per site); callers should discover
   * field ids via `getIssueTypeFieldMeta` first.
   */
  fields: Record<string, unknown>;
}

export async function editJiraIssueFields(client: AtlassianMcpClient, params: EditJiraIssueFieldsParams) {
  const result = await client.callTool('editJiraIssue', {
    cloudId: await client.getCloudId(),
    issueIdOrKey: params.issueIdOrKey,
    fields: params.fields,
  });
  const payload = extractPayload(result);
  return { raw: result, payload };
}

export interface AddWorklogParams {
  issueIdOrKey: string;
  timeSpentSeconds: number;
  comment?: string;
}

export async function addWorklog(client: AtlassianMcpClient, params: AddWorklogParams) {
  const args: Record<string, unknown> = {
    cloudId: await client.getCloudId(),
    issueIdOrKey: params.issueIdOrKey,
    timeSpentSeconds: params.timeSpentSeconds,
  };
  if (params.comment !== undefined) args.comment = params.comment;

  const result = await client.callTool('addWorklogToJiraIssue', args);
  const payload = extractPayload(result);
  return { raw: result, payload };
}

export interface GetIssueTypeFieldMetaParams {
  projectIdOrKey: string;
  issueTypeId: string;
}

/**
 * Fetches create-field metadata for a project and issue type, used by
 * callers to discover a site's actual story-points custom field id (or any
 * other field) before calling `editJiraIssueFields`.
 */
export async function getIssueTypeFieldMeta(client: AtlassianMcpClient, params: GetIssueTypeFieldMetaParams) {
  const result = await client.callTool('getJiraIssueTypeMetaWithFields', {
    cloudId: await client.getCloudId(),
    projectIdOrKey: params.projectIdOrKey,
    issueTypeId: params.issueTypeId,
  });
  const payload = extractPayload(result);
  return { raw: result, payload };
}

export interface CreateJiraIssueParams {
  projectKey: string;
  issueTypeName: string;
  summary: string;
  description?: string;
  parent?: string;
  additionalFields?: Record<string, unknown>;
}

/**
 * Wraps the real `createJiraIssue(cloudId, projectKey, issueTypeName,
 * summary, description, parent, additional_fields)` MCP tool. Referenced
 * throughout the atlassian-mcp-server skills docs but not previously wrapped
 * in this adapter. Used directly by callers that already know there's no
 * duplicate, and internally by `createJiraSubtask` for genuinely-new
 * candidates.
 */
export async function createJiraIssue(client: AtlassianMcpClient, params: CreateJiraIssueParams) {
  const cloudId = await client.getCloudId();
  const args: Record<string, unknown> = {
    cloudId,
    projectKey: params.projectKey,
    issueTypeName: params.issueTypeName,
    summary: params.summary,
  };
  if (params.description !== undefined) args.description = params.description;
  if (params.parent !== undefined) args.parent = params.parent;
  if (params.additionalFields !== undefined) args.additional_fields = params.additionalFields;

  const result = await client.callTool('createJiraIssue', args);
  const payload = extractPayload(result);
  return { raw: result, normalized: normalizeJiraIssue(payload, cloudId) };
}

/** Trims, collapses internal whitespace, and lowercases a summary for exact (non-fuzzy) comparison. */
function normalizeSummaryForComparison(summary: string): string {
  return summary.trim().replace(/\s+/g, ' ').toLowerCase();
}

export interface CreateJiraSubtaskCandidate {
  summary: string;
  description?: string;
  additionalFields?: Record<string, unknown>;
}

export interface CreateJiraSubtaskParams {
  parentKey: string;
  projectKey: string;
  /**
   * The real issue-type name for subtasks (varies per Jira project — the
   * caller must discover it via `getJiraProjectIssueTypesMetadata`; this
   * operation does not call that tool itself and does not hardcode
   * "Sub-task").
   */
  issueTypeName: string;
  candidates: CreateJiraSubtaskCandidate[];
}

export interface CreateJiraSubtaskResult {
  created: NormalizedJiraIssue[];
  skipped: { summary: string; existingKey: string }[];
}

/**
 * Creates subtasks under `parentKey`, deduplicating against already-existing
 * children first. Existing children are discovered via `searchJira` (JQL
 * `parent = "<parentKey>"`); a candidate is skipped only on an exact match
 * (trimmed, whitespace-collapsed, case-insensitive) against an existing
 * child's summary — deliberately no fuzzy matching, since a false-positive
 * skip silently drops real work, which is worse than an occasional missed
 * dedup.
 */
export async function createJiraSubtask(
  client: AtlassianMcpClient,
  params: CreateJiraSubtaskParams,
): Promise<CreateJiraSubtaskResult> {
  const { normalized: existingChildren } = await searchJira(client, {
    jql: `parent = "${params.parentKey}"`,
  });

  const existingBySummary = new Map<string, string>();
  for (const child of existingChildren) {
    existingBySummary.set(normalizeSummaryForComparison(child.summary), child.key);
  }

  const created: NormalizedJiraIssue[] = [];
  const skipped: { summary: string; existingKey: string }[] = [];

  for (const candidate of params.candidates) {
    const normalizedSummary = normalizeSummaryForComparison(candidate.summary);
    const existingKey = existingBySummary.get(normalizedSummary);
    if (existingKey) {
      skipped.push({ summary: candidate.summary, existingKey });
      continue;
    }

    const { normalized } = await createJiraIssue(client, {
      projectKey: params.projectKey,
      issueTypeName: params.issueTypeName,
      summary: candidate.summary,
      description: candidate.description,
      parent: params.parentKey,
      additionalFields: candidate.additionalFields,
    });
    created.push(normalized);
  }

  return { created, skipped };
}
