import type { AtlassianRestClient, RestResponse } from './restClient.js';
import { normalizeConfluencePage, normalizeJiraIssue, normalizeJiraSearchIssue, type NormalizedJiraIssue } from './normalize.js';

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function plainTextAdf(text: string): Record<string, unknown> {
  return {
    type: 'doc',
    version: 1,
    content: [{ type: 'paragraph', content: text ? [{ type: 'text', text }] : [] }],
  };
}

function successPayload(response: RestResponse): Record<string, unknown> {
  const payload = asRecord(response.data);
  return Object.keys(payload).length > 0 ? payload : { ok: true };
}

function encodePath(value: string): string {
  return encodeURIComponent(value);
}

export interface GetJiraIssueParams {
  issueIdOrKey: string;
  /** Exact Jira fields to fetch. Defaults to the compact planning-oriented set below. */
  fields?: string[];
  /** Raw REST envelopes are expensive; expose one only for diagnostics/reconciliation. */
  includeRaw?: boolean;
}

export const DEFAULT_JIRA_ISSUE_FIELDS = [
  'summary', 'description', 'status', 'issuetype', 'priority', 'assignee',
  'labels', 'parent', 'subtasks', 'timetracking', 'updated',
] as const;

export const DEFAULT_JIRA_SEARCH_FIELDS = [
  'summary', 'status', 'issuetype', 'priority', 'assignee', 'parent', 'updated',
] as const;

function optionalRaw<T extends Record<string, unknown>>(result: T, raw: RestResponse, includeRaw?: boolean): T & { raw?: RestResponse } {
  return includeRaw ? { ...result, raw } : result;
}

export async function getJiraIssue(client: AtlassianRestClient, params: GetJiraIssueParams) {
  const fields = params.fields ?? [...DEFAULT_JIRA_ISSUE_FIELDS];
  const raw = await client.jira<Record<string, unknown>>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    query: { fields: fields.join(',') },
  });
  const cloudId = await client.getCloudId();
  return optionalRaw({ normalized: normalizeJiraIssue(asRecord(raw.data), cloudId) }, raw, params.includeRaw);
}

export interface GetConfluencePageParams {
  pageId: string;
  contentFormat?: string;
  includeRaw?: boolean;
}

function confluenceRepresentation(requested: string | undefined): string {
  // Direct Confluence REST does not expose Rovo's synthetic markdown format.
  // Storage is lossless and available through the v1 content API.
  const supported = new Set(['storage', 'view', 'export_view', 'styled_view', 'editor', 'anonymous_export_view']);
  return requested && supported.has(requested) ? requested : 'storage';
}

export async function getConfluencePage(client: AtlassianRestClient, params: GetConfluencePageParams) {
  const format = confluenceRepresentation(params.contentFormat);
  const raw = await client.confluence<Record<string, unknown>>(`/wiki/rest/api/content/${encodePath(params.pageId)}`, {
    query: { expand: `body.${format},version,space` },
  });
  const payload = { ...asRecord(raw.data), contentFormat: format };
  return optionalRaw({ normalized: normalizeConfluencePage(payload, await client.getCloudId()) }, raw, params.includeRaw);
}

export interface SearchJiraParams {
  jql: string;
  fields?: string[];
  maxResults?: number;
  includeRaw?: boolean;
}

export async function searchJira(client: AtlassianRestClient, params: SearchJiraParams) {
  const body: Record<string, unknown> = { jql: params.jql };
  body.fields = params.fields ?? [...DEFAULT_JIRA_SEARCH_FIELDS];
  body.maxResults = params.maxResults ?? 20;

  const raw = await client.jira<Record<string, unknown>>('/rest/api/3/search/jql', { method: 'POST', body });
  const payload = asRecord(raw.data);
  const issues = Array.isArray(payload.issues) ? payload.issues : [];
  const page = {
    returned: issues.length,
    nextPageToken: asString(payload.nextPageToken),
    isLast: typeof payload.isLast === 'boolean' ? payload.isLast : undefined,
  };
  return optionalRaw({ normalized: issues.map((issue) => normalizeJiraSearchIssue(asRecord(issue))), page }, raw, params.includeRaw);
}

export interface SearchConfluenceParams {
  cql: string;
  includeRaw?: boolean;
}

export async function searchConfluence(client: AtlassianRestClient, params: SearchConfluenceParams) {
  const raw = await client.confluence<Record<string, unknown>>('/wiki/rest/api/content/search', {
    query: { cql: params.cql, expand: 'body.storage,version,space' },
  });
  const payload = asRecord(raw.data);
  const pages = Array.isArray(payload.results) ? payload.results : [];
  const cloudId = await client.getCloudId();
  return optionalRaw({
    normalized: pages.map((page) => normalizeConfluencePage({ ...asRecord(page), contentFormat: 'storage' }, cloudId)),
    page: { returned: pages.length },
  }, raw, params.includeRaw);
}

export interface AddJiraCommentParams {
  issueIdOrKey: string;
  commentBody: string;
}

export async function addJiraComment(client: AtlassianRestClient, params: AddJiraCommentParams) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/comment`,
    { method: 'POST', body: { body: plainTextAdf(params.commentBody) } },
  );
  const payload = asRecord(raw.data);
  const id = payload.id;
  return { ok: true, commentId: typeof id === 'string' || typeof id === 'number' ? String(id) : undefined };
}

export interface JiraTransition {
  id: string;
  name: string;
  toStatus: { id: string | undefined; name: string | undefined };
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
    };
  });
}

export interface GetTransitionsForJiraIssueParams {
  issueIdOrKey: string;
}

export async function getTransitionsForJiraIssue(client: AtlassianRestClient, params: GetTransitionsForJiraIssueParams) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/transitions`,
  );
  return { transitions: extractTransitions(asRecord(raw.data)) };
}

export interface TransitionJiraIssueParams {
  issueIdOrKey: string;
  transitionId: string;
}

export async function transitionJiraIssue(client: AtlassianRestClient, params: TransitionJiraIssueParams) {
  const raw = await client.jira(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/transitions`, {
    method: 'POST',
    body: { transition: { id: params.transitionId } },
  });
  return { ok: true, payload: successPayload(raw) };
}

export interface EditJiraIssueFieldsParams {
  issueIdOrKey: string;
  fields: Record<string, unknown>;
}

export async function editJiraIssueFields(client: AtlassianRestClient, params: EditJiraIssueFieldsParams) {
  await client.jira(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    method: 'PUT',
    body: { fields: params.fields },
  });
  return { ok: true, issueIdOrKey: params.issueIdOrKey, updatedFields: Object.keys(params.fields) };
}

export interface AddWorklogParams {
  issueIdOrKey: string;
  timeSpentSeconds: number;
  comment?: string;
}

export async function addWorklog(client: AtlassianRestClient, params: AddWorklogParams) {
  const body: Record<string, unknown> = { timeSpentSeconds: params.timeSpentSeconds };
  if (params.comment !== undefined) body.comment = plainTextAdf(params.comment);
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/worklog`,
    { method: 'POST', body },
  );
  const payload = asRecord(raw.data);
  const id = payload.id;
  return {
    ok: true,
    worklogId: typeof id === 'string' || typeof id === 'number' ? String(id) : undefined,
    timeSpentSeconds: params.timeSpentSeconds,
  };
}

export interface GetIssueTypeFieldMetaParams {
  projectIdOrKey: string;
  issueTypeId: string;
}

export async function getIssueTypeFieldMeta(client: AtlassianRestClient, params: GetIssueTypeFieldMetaParams) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/createmeta/${encodePath(params.projectIdOrKey)}/issuetypes/${encodePath(params.issueTypeId)}`,
  );
  return { payload: asRecord(raw.data) };
}

export interface CreateJiraIssueParams {
  projectKey: string;
  issueTypeName: string;
  summary: string;
  description?: string;
  parent?: string;
  additionalFields?: Record<string, unknown>;
}

export async function createJiraIssue(client: AtlassianRestClient, params: CreateJiraIssueParams) {
  const fields: Record<string, unknown> = {
    ...(params.additionalFields ?? {}),
    project: { key: params.projectKey },
    issuetype: { name: params.issueTypeName },
    summary: params.summary,
  };
  if (params.description !== undefined) fields.description = plainTextAdf(params.description);
  if (params.parent !== undefined) fields.parent = { key: params.parent };

  const createResponse = await client.jira<Record<string, unknown>>('/rest/api/3/issue', {
    method: 'POST',
    body: { fields },
  });
  const created = asRecord(createResponse.data);
  const key = asString(created.key) ?? asString(created.id);
  if (!key) throw new Error('Jira create issue response is missing both "key" and "id".');

  // Jira's create endpoint returns identifiers, not the fields needed by the
  // stable normalized result, so read the newly created issue once.
  const fetched = await getJiraIssue(client, { issueIdOrKey: key });
  return { normalized: fetched.normalized };
}

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
  issueTypeName: string;
  candidates: CreateJiraSubtaskCandidate[];
}

export interface CreateJiraSubtaskResult {
  created: NormalizedJiraIssue[];
  skipped: { summary: string; existingKey: string }[];
}

export async function createJiraSubtask(
  client: AtlassianRestClient,
  params: CreateJiraSubtaskParams,
): Promise<CreateJiraSubtaskResult> {
  const { normalized: existingChildren } = await searchJira(client, { jql: `parent = "${params.parentKey}"` });
  const existingBySummary = new Map<string, string>();
  for (const child of existingChildren) {
    existingBySummary.set(normalizeSummaryForComparison(child.summary), child.key);
  }

  const created: NormalizedJiraIssue[] = [];
  const skipped: { summary: string; existingKey: string }[] = [];
  for (const candidate of params.candidates) {
    const existingKey = existingBySummary.get(normalizeSummaryForComparison(candidate.summary));
    if (existingKey) {
      skipped.push({ summary: candidate.summary, existingKey });
      continue;
    }
    const result = await createJiraIssue(client, {
      projectKey: params.projectKey,
      issueTypeName: params.issueTypeName,
      summary: candidate.summary,
      description: candidate.description,
      parent: params.parentKey,
      additionalFields: candidate.additionalFields,
    });
    created.push(result.normalized);
  }
  return { created, skipped };
}
