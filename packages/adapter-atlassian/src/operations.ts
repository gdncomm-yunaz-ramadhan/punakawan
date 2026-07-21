import { constants } from 'node:fs';
import { mkdir, open, readFile, realpath, stat } from 'node:fs/promises';
import path from 'node:path';
import { markdownToAdf } from 'marklassian';
import type { AtlassianRestClient, RestResponse } from './restClient.js';
import { jiraText, normalizeConfluencePage, normalizeJiraIssue, normalizeJiraSearchIssue, type NormalizedJiraIssue } from './normalize.js';

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

/**
 * Renders caller-supplied text (comment bodies, descriptions) as Atlassian
 * Document Format, parsing it as Markdown first so headings, bold/italic,
 * lists, and code blocks become real ADF nodes instead of literal "##"/"**"
 * characters in one plain-text paragraph - Jira's UI renders ADF structure,
 * not raw Markdown syntax. Plain, syntax-free text still round-trips to a
 * single paragraph, so this is a strict improvement over wrapping text in
 * one paragraph unconditionally.
 */
function markdownAdf(text: string): Record<string, unknown> {
  if (!text) return { type: 'doc', version: 1, content: [{ type: 'paragraph', content: [] }] };
  return markdownToAdf(text) as Record<string, unknown>;
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
  'labels', 'parent', 'subtasks', 'issuelinks', 'timetracking', 'updated',
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

export interface GetJiraCommentsParams {
  issueIdOrKey: string;
  startAt?: number;
  maxResults?: number;
}

export async function getJiraComments(client: AtlassianRestClient, params: GetJiraCommentsParams) {
  const startAt = Math.max(0, params.startAt ?? 0);
  const maxResults = Math.min(100, Math.max(1, params.maxResults ?? 20));
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/comment`,
    { query: { startAt, maxResults, orderBy: 'created' } },
  );
  const payload = asRecord(raw.data);
  const comments = Array.isArray(payload.comments) ? payload.comments : [];
  return {
    comments: comments.map((entry) => {
      const comment = asRecord(entry);
      const author = asRecord(comment.author);
      return {
        id: asString(comment.id),
        author: asString(author.displayName) ?? asString(author.accountId),
        body: jiraText(comment.body),
        created: asString(comment.created),
        updated: asString(comment.updated),
      };
    }),
    page: {
      startAt,
      returned: comments.length,
      total: typeof payload.total === 'number' ? payload.total : undefined,
    },
  };
}

export interface GetJiraRemoteLinksParams {
  issueIdOrKey: string;
  maxResults?: number;
}

export async function getJiraRemoteLinks(client: AtlassianRestClient, params: GetJiraRemoteLinksParams) {
  const raw = await client.jira<unknown[]>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/remotelink`);
  const all = Array.isArray(raw.data) ? raw.data : [];
  const maxResults = Math.min(100, Math.max(1, params.maxResults ?? 20));
  const links = all.slice(0, maxResults).map((entry) => {
    const link = asRecord(entry);
    const object = asRecord(link.object);
    return {
      id: typeof link.id === 'string' || typeof link.id === 'number' ? String(link.id) : undefined,
      globalId: asString(link.globalId),
      relationship: asString(link.relationship),
      title: asString(object.title),
      summary: asString(object.summary),
      url: asString(object.url),
    };
  });
  return { links, page: { returned: links.length, total: all.length, truncated: all.length > links.length } };
}

export interface GetJiraEpicParams {
  epicIdOrKey: string;
  maxChildren?: number;
}

function quoteJql(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

export async function getJiraEpic(client: AtlassianRestClient, params: GetJiraEpicParams) {
  const maxChildren = Math.min(100, Math.max(1, params.maxChildren ?? 50));
  const [epic, children] = await Promise.all([
    getJiraIssue(client, { issueIdOrKey: params.epicIdOrKey }),
    searchJira(client, { jql: `parent = "${quoteJql(params.epicIdOrKey)}" ORDER BY key`, maxResults: maxChildren }),
  ]);
  return { epic: epic.normalized, children: children.normalized, page: children.page };
}

export interface JiraAttachment {
  id: string;
  filename?: string;
  mediaType?: string;
  size?: number;
  created?: string;
  author?: string;
}

function compactAttachment(value: unknown): JiraAttachment | undefined {
  const attachment = asRecord(value);
  const id = typeof attachment.id === 'string' || typeof attachment.id === 'number' ? String(attachment.id) : undefined;
  if (!id) return undefined;
  const author = asRecord(attachment.author);
  return {
    id,
    filename: asString(attachment.filename),
    mediaType: asString(attachment.mimeType),
    size: typeof attachment.size === 'number' ? attachment.size : undefined,
    created: asString(attachment.created),
    author: asString(author.displayName) ?? asString(author.accountId),
  };
}

export interface ListJiraAttachmentsParams {
  issueIdOrKey: string;
  maxResults?: number;
}

export async function listJiraAttachments(client: AtlassianRestClient, params: ListJiraAttachmentsParams) {
  const raw = await client.jira<Record<string, unknown>>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    query: { fields: 'attachment' },
  });
  const values = asRecord(raw.data).fields;
  const all = Array.isArray(asRecord(values).attachment) ? asRecord(values).attachment as unknown[] : [];
  const maxResults = Math.min(100, Math.max(1, params.maxResults ?? 20));
  const attachments = all.slice(0, maxResults).flatMap((entry) => {
    const compact = compactAttachment(entry);
    return compact ? [compact] : [];
  });
  return { attachments, page: { returned: attachments.length, total: all.length, truncated: all.length > attachments.length } };
}

function workspacePath(workspaceRoot: string, requestedPath: string): { absolute: string; relative: string } {
  if (!workspaceRoot) throw new Error('PUNAKAWAN_WORKSPACE_ROOT is required for attachment file access.');
  if (!requestedPath) throw new Error('Attachment file path must not be empty.');
  const root = path.resolve(workspaceRoot);
  const absolute = path.resolve(root, requestedPath);
  const relative = path.relative(root, absolute);
  if (!relative || relative.startsWith(`..${path.sep}`) || relative === '..' || path.isAbsolute(relative)) {
    throw new Error(`Attachment path must resolve to a file inside the Punakawan workspace: ${requestedPath}`);
  }
  return { absolute, relative };
}

function isInside(root: string, candidate: string): boolean {
  const relative = path.relative(root, candidate);
  return relative !== '' && relative !== '..' && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

export interface DownloadJiraAttachmentParams {
  attachmentId: string;
  outputPath: string;
}

export async function downloadJiraAttachment(
  client: AtlassianRestClient,
  params: DownloadJiraAttachmentParams,
  workspaceRoot: string,
) {
  const target = workspacePath(workspaceRoot, params.outputPath);
  const response = await client.jiraBytes(`/rest/api/3/attachment/content/${encodePath(params.attachmentId)}`);
  await mkdir(path.dirname(target.absolute), { recursive: true });
  const [realRoot, realParent] = await Promise.all([realpath(workspaceRoot), realpath(path.dirname(target.absolute))]);
  if (realParent !== realRoot && !isInside(realRoot, realParent)) {
    throw new Error(`Attachment output parent escapes the Punakawan workspace through a symlink: ${target.relative}`);
  }
  const safeTarget = path.join(realParent, path.basename(target.absolute));
  const handle = await open(
    safeTarget,
    constants.O_WRONLY | constants.O_CREAT | constants.O_TRUNC | constants.O_NOFOLLOW,
    0o600,
  );
  try {
    await handle.writeFile(response.data);
  } finally {
    await handle.close();
  }
  return {
    ok: true,
    attachmentId: params.attachmentId,
    path: target.relative,
    bytes: response.data.byteLength,
    mediaType: response.contentType,
  };
}

export interface UploadJiraAttachmentParams {
  issueIdOrKey: string;
  filePath: string;
}

export async function uploadJiraAttachment(
  client: AtlassianRestClient,
  params: UploadJiraAttachmentParams,
  workspaceRoot: string,
) {
  const source = workspacePath(workspaceRoot, params.filePath);
  const [realRoot, realSource] = await Promise.all([realpath(workspaceRoot), realpath(source.absolute)]);
  if (!isInside(realRoot, realSource)) {
    throw new Error(`Attachment source escapes the Punakawan workspace through a symlink: ${source.relative}`);
  }
  const info = await stat(realSource);
  if (!info.isFile()) throw new Error(`Attachment source is not a regular file: ${source.relative}`);
  const maxBytes = 100 * 1024 * 1024;
  if (info.size > maxBytes) throw new Error(`Attachment exceeds Punakawan's 100 MiB in-memory upload limit: ${source.relative}`);
  const data = await readFile(realSource);
  const form = new FormData();
  form.append('file', new Blob([data]), path.basename(realSource));
  const raw = await client.jira<unknown[]>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/attachments`, {
    method: 'POST',
    multipart: form,
    headers: { 'X-Atlassian-Token': 'no-check' },
  });
  const uploaded = (Array.isArray(raw.data) ? raw.data : []).flatMap((entry) => {
    const compact = compactAttachment(entry);
    return compact ? [compact] : [];
  });
  return { ok: true, issueIdOrKey: params.issueIdOrKey, uploaded };
}

export interface DeleteJiraAttachmentParams {
  attachmentId: string;
}

export async function deleteJiraAttachment(client: AtlassianRestClient, params: DeleteJiraAttachmentParams) {
  await client.jira(`/rest/api/3/attachment/${encodePath(params.attachmentId)}`, { method: 'DELETE' });
  return { ok: true, attachmentId: params.attachmentId, deleted: true };
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
    { method: 'POST', body: { body: markdownAdf(params.commentBody) } },
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

export interface EditJiraIssueParams {
  issueIdOrKey: string;
  /** Jira calls its title field "summary"; title is accepted as a convenience alias. */
  summary?: string;
  title?: string;
  /** Plain text converted to Atlassian Document Format. */
  description?: string;
  /** Jira duration strings such as "8h" or "2d". */
  originalEstimate?: string;
  remainingEstimate?: string;
  /** Story points require the site's field id, discoverable through getIssueTypeFieldMeta. */
  storyPoints?: number;
  storyPointsFieldId?: string;
  /** Escape hatch for arbitrary Jira fields. Convenience fields above override matching keys. */
  fields?: Record<string, unknown>;
}

export async function editJiraIssue(client: AtlassianRestClient, params: EditJiraIssueParams) {
  if (params.summary !== undefined && params.title !== undefined && params.summary !== params.title) {
    throw new Error('atlassian.editJiraIssue received conflicting "summary" and "title" values.');
  }
  const fields: Record<string, unknown> = { ...(params.fields ?? {}) };
  const summary = params.summary ?? params.title;
  if (summary !== undefined) fields.summary = summary;
  if (params.description !== undefined) fields.description = markdownAdf(params.description);

  if (params.originalEstimate !== undefined || params.remainingEstimate !== undefined) {
    const existing = asRecord(fields.timetracking);
    fields.timetracking = {
      ...existing,
      ...(params.originalEstimate !== undefined ? { originalEstimate: params.originalEstimate } : {}),
      ...(params.remainingEstimate !== undefined ? { remainingEstimate: params.remainingEstimate } : {}),
    };
  }
  if (params.storyPoints !== undefined) {
    if (!params.storyPointsFieldId?.startsWith('customfield_')) {
      throw new Error('atlassian.editJiraIssue requires "storyPointsFieldId" (customfield_...) when "storyPoints" is set.');
    }
    fields[params.storyPointsFieldId] = params.storyPoints;
  }
  if (Object.keys(fields).length === 0) {
    throw new Error('atlassian.editJiraIssue requires at least one editable field.');
  }
  return editJiraIssueFields(client, { issueIdOrKey: params.issueIdOrKey, fields });
}

export interface AddWorklogParams {
  issueIdOrKey: string;
  timeSpentSeconds: number;
  comment?: string;
}

export async function addWorklog(client: AtlassianRestClient, params: AddWorklogParams) {
  const body: Record<string, unknown> = { timeSpentSeconds: params.timeSpentSeconds };
  if (params.comment !== undefined) body.comment = markdownAdf(params.comment);
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
  if (params.description !== undefined) fields.description = markdownAdf(params.description);
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
