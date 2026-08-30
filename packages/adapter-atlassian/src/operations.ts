import { constants } from 'node:fs';
import { mkdir, open, readFile, realpath, stat } from 'node:fs/promises';
import path from 'node:path';
import { markdownToAdf } from 'marklassian';
import type { AtlassianRestClient, RestResponse } from './restClient.js';
import { jiraText, normalizeConfluencePage, normalizeJiraIssue, normalizeJiraSearchIssue, type NormalizedJiraIssue } from './normalize.js';
import { collectStartAtPages, DEFAULT_HARD_LIMIT_ITEMS } from './pagination.js';

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

/**
 * Decodes the handful of named HTML entities a caller's Markdown source can
 * end up containing (observed in practice: an upstream MCP client sending
 * "->" as the literal 4 characters "-&gt;" rather than "->" or "→").
 * markdownToAdf does not decode entities - it is a Markdown parser, not an
 * HTML one - so left alone they land verbatim in the rendered comment as
 * "&gt;" instead of ">". &amp; must decode last, or a source ampersand
 * combined with a real entity elsewhere could get double-unescaped.
 */
function decodeHtmlEntities(text: string): string {
  return text
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;|&apos;/g, "'")
    .replace(/&amp;/g, '&');
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
  return markdownToAdf(decodeHtmlEntities(text)) as Record<string, unknown>;
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
  'summary', 'description', 'status', 'issuetype', 'project', 'priority', 'assignee',
  'labels', 'parent', 'subtasks', 'issuelinks', 'timetracking', 'updated',
] as const;

export const DEFAULT_JIRA_SEARCH_FIELDS = [
  'summary', 'status', 'issuetype', 'priority', 'assignee', 'parent', 'updated',
] as const;

function optionalRaw<T extends Record<string, unknown>>(result: T, raw: RestResponse, includeRaw?: boolean): T & { raw?: RestResponse } {
  return includeRaw ? { ...result, raw } : result;
}

export async function getJiraIssue(client: AtlassianRestClient, params: GetJiraIssueParams, signal?: AbortSignal) {
  const fields = params.fields ?? [...DEFAULT_JIRA_ISSUE_FIELDS];
  const raw = await client.jira<Record<string, unknown>>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    query: { fields: fields.join(',') },
    signal,
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

export async function getConfluencePage(client: AtlassianRestClient, params: GetConfluencePageParams, signal?: AbortSignal) {
  const format = confluenceRepresentation(params.contentFormat);
  const raw = await client.confluence<Record<string, unknown>>(`/wiki/rest/api/content/${encodePath(params.pageId)}`, {
    query: { expand: `body.${format},version,space` },
    signal,
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

export async function searchJira(client: AtlassianRestClient, params: SearchJiraParams, signal?: AbortSignal) {
  const body: Record<string, unknown> = { jql: params.jql };
  body.fields = params.fields ?? [...DEFAULT_JIRA_SEARCH_FIELDS];
  body.maxResults = params.maxResults ?? 20;

  const raw = await client.jira<Record<string, unknown>>('/rest/api/3/search/jql', { method: 'POST', body, signal });
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
  /** Page size requested per underlying REST call; every page is still collected up to the hard limit below. */
  maxResults?: number;
}

function normalizeComment(entry: unknown) {
  const comment = asRecord(entry);
  const author = asRecord(comment.author);
  return {
    id: asString(comment.id),
    author: asString(author.displayName) ?? asString(author.accountId),
    body: jiraText(comment.body),
    created: asString(comment.created),
    updated: asString(comment.updated),
  };
}

/**
 * Fetches every comment on an issue, walking Jira's startAt/maxResults
 * pagination (collectStartAtPages) instead of returning only the first
 * page - a reconciler searching for one intent's marker comment must see
 * every comment, not just however many fit the first response, or it would
 * wrongly conclude a write never applied and replay it.
 */
export async function getJiraComments(client: AtlassianRestClient, params: GetJiraCommentsParams, signal?: AbortSignal) {
  const pageSize = Math.min(100, Math.max(1, params.maxResults ?? 100));
  const result = await collectStartAtPages(async (startAt) => {
    const raw = await client.jira<Record<string, unknown>>(
      `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/comment`,
      { query: { startAt, maxResults: pageSize, orderBy: 'created' }, signal },
    );
    const payload = asRecord(raw.data);
    const comments = Array.isArray(payload.comments) ? payload.comments : [];
    return { values: comments, total: typeof payload.total === 'number' ? payload.total : undefined };
  }, DEFAULT_HARD_LIMIT_ITEMS);

  return {
    comments: result.items.map(normalizeComment),
    page: {
      returned: result.items.length,
      total: result.items.length,
      complete: result.complete,
      pages: result.pages,
      ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}),
    },
  };
}

export interface GetJiraRemoteLinksParams {
  issueIdOrKey: string;
  maxResults?: number;
}

export async function getJiraRemoteLinks(client: AtlassianRestClient, params: GetJiraRemoteLinksParams, signal?: AbortSignal) {
  const raw = await client.jira<unknown[]>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/remotelink`, { signal });
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

export async function getJiraEpic(client: AtlassianRestClient, params: GetJiraEpicParams, signal?: AbortSignal) {
  const maxChildren = Math.min(100, Math.max(1, params.maxChildren ?? 50));
  const [epic, children] = await Promise.all([
    getJiraIssue(client, { issueIdOrKey: params.epicIdOrKey }, signal),
    searchJira(client, { jql: `parent = "${quoteJql(params.epicIdOrKey)}" ORDER BY key`, maxResults: maxChildren }, signal),
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

export async function listJiraAttachments(client: AtlassianRestClient, params: ListJiraAttachmentsParams, signal?: AbortSignal) {
  const raw = await client.jira<Record<string, unknown>>(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    query: { fields: 'attachment' },
    signal,
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
  signal?: AbortSignal,
) {
  const target = workspacePath(workspaceRoot, params.outputPath);
  const response = await client.jiraBytes(`/rest/api/3/attachment/content/${encodePath(params.attachmentId)}`, signal);
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
  signal?: AbortSignal,
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
    signal,
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

export async function deleteJiraAttachment(client: AtlassianRestClient, params: DeleteJiraAttachmentParams, signal?: AbortSignal) {
  await client.jira(`/rest/api/3/attachment/${encodePath(params.attachmentId)}`, { method: 'DELETE', signal });
  return { ok: true, attachmentId: params.attachmentId, deleted: true };
}

export interface SearchConfluenceParams {
  cql: string;
  includeRaw?: boolean;
}

export async function searchConfluence(client: AtlassianRestClient, params: SearchConfluenceParams, signal?: AbortSignal) {
  const raw = await client.confluence<Record<string, unknown>>('/wiki/rest/api/content/search', {
    query: { cql: params.cql, expand: 'body.storage,version,space' },
    signal,
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

export async function addJiraComment(client: AtlassianRestClient, params: AddJiraCommentParams, signal?: AbortSignal) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/comment`,
    { method: 'POST', body: { body: markdownAdf(params.commentBody) }, signal },
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

export async function getTransitionsForJiraIssue(client: AtlassianRestClient, params: GetTransitionsForJiraIssueParams, signal?: AbortSignal) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/transitions`,
    { signal },
  );
  return { transitions: extractTransitions(asRecord(raw.data)) };
}

export interface TransitionJiraIssueParams {
  issueIdOrKey: string;
  transitionId: string;
}

export async function transitionJiraIssue(client: AtlassianRestClient, params: TransitionJiraIssueParams, signal?: AbortSignal) {
  const raw = await client.jira(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/transitions`, {
    method: 'POST',
    body: { transition: { id: params.transitionId } },
    signal,
  });
  return { ok: true, payload: successPayload(raw) };
}

export interface EditJiraIssueFieldsParams {
  issueIdOrKey: string;
  fields: Record<string, unknown>;
}

export async function editJiraIssueFields(client: AtlassianRestClient, params: EditJiraIssueFieldsParams, signal?: AbortSignal) {
  await client.jira(`/rest/api/3/issue/${encodePath(params.issueIdOrKey)}`, {
    method: 'PUT',
    body: { fields: params.fields },
    signal,
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
  /** Story points require the field id returned by Jira issue-type metadata. */
  storyPoints?: number;
  storyPointsFieldId?: string;
  /** Escape hatch for arbitrary Jira fields. Convenience fields above override matching keys. */
  fields?: Record<string, unknown>;
}

export async function editJiraIssue(client: AtlassianRestClient, params: EditJiraIssueParams, signal?: AbortSignal) {
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
    if (!params.storyPointsFieldId?.trim()) {
      throw new Error('atlassian.editJiraIssue requires a non-empty "storyPointsFieldId" when "storyPoints" is set.');
    }
    fields[params.storyPointsFieldId] = params.storyPoints;
  }
  if (Object.keys(fields).length === 0) {
    throw new Error('atlassian.editJiraIssue requires at least one editable field.');
  }
  return editJiraIssueFields(client, { issueIdOrKey: params.issueIdOrKey, fields }, signal);
}

export interface SearchJiraUsersParams {
  query: string;
  maxResults?: number;
}

/**
 * Resolves a display name or email to Jira account ids via the user-search
 * endpoint, so callers no longer need a raw call_adapter_operation passthrough
 * (or a separate hosted Atlassian MCP call) just to find an accountId for an
 * assignment (punokawan-t6y). Read-only.
 */
export async function searchJiraUsers(client: AtlassianRestClient, params: SearchJiraUsersParams, signal?: AbortSignal) {
  const maxResults = Math.min(50, Math.max(1, params.maxResults ?? 20));
  const raw = await client.jira<unknown[]>('/rest/api/3/user/search', {
    query: { query: params.query, maxResults },
    signal,
  });
  const all = Array.isArray(raw.data) ? raw.data : [];
  const users = all.map((entry) => {
    const user = asRecord(entry);
    return {
      accountId: asString(user.accountId),
      displayName: asString(user.displayName),
      emailAddress: asString(user.emailAddress),
      active: typeof user.active === 'boolean' ? user.active : undefined,
    };
  });
  return { users };
}

export interface JiraBoard {
  id: number;
  name?: string;
  type?: string;
}

export interface ListJiraBoardsParams {
  /** Restrict to one project's boards; omit to list every board visible to this token. */
  projectKeyOrId?: string;
  /** Jira board type filter, e.g. "scrum" (only scrum boards support the sprint endpoint). */
  type?: string;
  maxResults?: number;
}

/**
 * Lists Jira Agile boards, optionally scoped to a project. Board discovery is
 * the first half of resolving a sprint by project_key alone (punokawan-wij9):
 * a caller with only a project key has no other way to learn the numeric
 * board id listSprintsForBoard requires. Read-only.
 */
export async function listJiraBoards(client: AtlassianRestClient, params: ListJiraBoardsParams, signal?: AbortSignal) {
  const maxResults = Math.min(100, Math.max(1, params.maxResults ?? 50));
  const raw = await client.jira<Record<string, unknown>>('/rest/agile/1.0/board', {
    query: { projectKeyOrId: params.projectKeyOrId, type: params.type, maxResults },
    signal,
  });
  const payload = asRecord(raw.data);
  const values = Array.isArray(payload.values) ? payload.values : [];
  const boards: JiraBoard[] = values.flatMap((entry) => {
    const board = asRecord(entry);
    const id = typeof board.id === 'number' ? board.id : Number(board.id);
    if (!Number.isFinite(id)) return [];
    return [{ id, name: asString(board.name), type: asString(board.type) }];
  });
  return {
    boards,
    page: { returned: boards.length, isLast: typeof payload.isLast === 'boolean' ? payload.isLast : undefined },
  };
}

export interface JiraSprint {
  id: number;
  name?: string;
  state?: string;
  boardId?: number;
  startDate?: string;
  endDate?: string;
  goal?: string;
}

export interface ListJiraSprintsParams {
  boardId: number;
  /** Jira accepts a comma-separated combination of active,future,closed; omit for all. */
  state?: string;
  maxResults?: number;
}

/**
 * Lists sprints on one Agile board, optionally filtered by state. Direct
 * replacement for the raw JQL "board in (X) and sprint in openSprints()"
 * workaround (punokawan-wij9) a caller previously had to resort to just to
 * find a sprint id. Read-only; 400s from Jira if boardId names a kanban
 * board (kanban boards have no sprints).
 */
export async function listJiraSprints(client: AtlassianRestClient, params: ListJiraSprintsParams, signal?: AbortSignal) {
  const maxResults = Math.min(100, Math.max(1, params.maxResults ?? 50));
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/agile/1.0/board/${encodePath(String(params.boardId))}/sprint`,
    { query: { state: params.state, maxResults }, signal },
  );
  const payload = asRecord(raw.data);
  const values = Array.isArray(payload.values) ? payload.values : [];
  const sprints: JiraSprint[] = values.flatMap((entry) => {
    const sprint = asRecord(entry);
    const id = typeof sprint.id === 'number' ? sprint.id : Number(sprint.id);
    if (!Number.isFinite(id)) return [];
    const boardId = typeof sprint.originBoardId === 'number' ? sprint.originBoardId : params.boardId;
    return [{
      id,
      name: asString(sprint.name),
      state: asString(sprint.state),
      boardId,
      startDate: asString(sprint.startDate),
      endDate: asString(sprint.endDate),
      goal: asString(sprint.goal),
    }];
  });
  return {
    sprints,
    page: { returned: sprints.length, isLast: typeof payload.isLast === 'boolean' ? payload.isLast : undefined },
  };
}

export interface CreateIssueLinkParams {
  /** Issue link type NAME (e.g. "Blocks", "Relates"). */
  linkType: string;
  inwardIssueKey: string;
  outwardIssueKey: string;
}

/**
 * Creates an issue link between two issues. inwardIssueKey/outwardIssueKey map
 * directly onto Jira's inwardIssue/outwardIssue, so the semantic direction is
 * the link type's own (for "Blocks", outward blocks inward). A write.
 */
export async function createIssueLink(client: AtlassianRestClient, params: CreateIssueLinkParams, signal?: AbortSignal) {
  await client.jira('/rest/api/3/issueLink', {
    method: 'POST',
    body: {
      type: { name: params.linkType },
      inwardIssue: { key: params.inwardIssueKey },
      outwardIssue: { key: params.outwardIssueKey },
    },
    signal,
  });
  return {
    ok: true,
    linkType: params.linkType,
    inwardIssue: params.inwardIssueKey,
    outwardIssue: params.outwardIssueKey,
  };
}

export interface AddWorklogParams {
  issueIdOrKey: string;
  timeSpentSeconds: number;
  comment?: string;
}

export async function addWorklog(client: AtlassianRestClient, params: AddWorklogParams, signal?: AbortSignal) {
  const body: Record<string, unknown> = { timeSpentSeconds: params.timeSpentSeconds };
  if (params.comment !== undefined) body.comment = markdownAdf(params.comment);
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/worklog`,
    { method: 'POST', body, signal },
  );
  const payload = asRecord(raw.data);
  const id = payload.id;
  return {
    ok: true,
    worklogId: typeof id === 'string' || typeof id === 'number' ? String(id) : undefined,
    timeSpentSeconds: params.timeSpentSeconds,
  };
}

export interface ListJiraWorklogsParams {
  issueIdOrKey: string;
  maxResults?: number;
}

/**
 * Fetches every worklog entry on an issue, across all pages
 * (collectStartAtPages) - the read a jira.worklog provider-write intent's
 * reconciliation needs to positively determine whether an ambiguous
 * addWorklog attempt already applied, by searching for the comment marker
 * that attempt would have embedded.
 */
export async function listJiraWorklogs(client: AtlassianRestClient, params: ListJiraWorklogsParams, signal?: AbortSignal) {
  const pageSize = Math.min(100, Math.max(1, params.maxResults ?? 100));
  const result = await collectStartAtPages(async (startAt) => {
    const raw = await client.jira<Record<string, unknown>>(
      `/rest/api/3/issue/${encodePath(params.issueIdOrKey)}/worklog`,
      { query: { startAt, maxResults: pageSize }, signal },
    );
    const payload = asRecord(raw.data);
    const worklogs = Array.isArray(payload.worklogs) ? payload.worklogs : [];
    return { values: worklogs, total: typeof payload.total === 'number' ? payload.total : undefined };
  }, DEFAULT_HARD_LIMIT_ITEMS);

  return {
    worklogs: result.items.map((entry) => {
      const worklog = asRecord(entry);
      return {
        id: asString(worklog.id),
        comment: jiraText(worklog.comment),
        timeSpentSeconds: typeof worklog.timeSpentSeconds === 'number' ? worklog.timeSpentSeconds : undefined,
        started: asString(worklog.started),
      };
    }),
    page: { returned: result.items.length, complete: result.complete, pages: result.pages, ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}) },
  };
}

export interface GetIssueTypeFieldMetaParams {
  projectIdOrKey: string;
  issueTypeId: string;
}

export async function getIssueTypeFieldMeta(client: AtlassianRestClient, params: GetIssueTypeFieldMetaParams, signal?: AbortSignal) {
  const raw = await client.jira<Record<string, unknown>>(
    `/rest/api/3/issue/createmeta/${encodePath(params.projectIdOrKey)}/issuetypes/${encodePath(params.issueTypeId)}`,
    { signal },
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

export async function createJiraIssue(client: AtlassianRestClient, params: CreateJiraIssueParams, signal?: AbortSignal) {
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
    signal,
  });
  const created = asRecord(createResponse.data);
  const key = asString(created.key) ?? asString(created.id);
  if (!key) throw new Error('Jira create issue response is missing both "key" and "id".');

  // Jira's create endpoint returns identifiers, not the fields needed by the
  // stable normalized result, so read the newly created issue once.
  const fetched = await getJiraIssue(client, { issueIdOrKey: key }, signal);
  return { normalized: fetched.normalized };
}

/**
 * Normalizes a subtask summary for equality comparison exactly as the Go
 * outbox reconciler (internal/providerwrite.normalizeSummary) and the
 * fingerprint that identifies a create-subtask intent both do, so a
 * summary that already exists (however it was created) always compares
 * equal here.
 */
function normalizeSummaryForComparison(summary: string): string {
  return summary.trim().replace(/\s+/g, ' ').toLocaleLowerCase('en-US');
}

/**
 * Embeds a marker in the description as its own trailing paragraph, so a
 * reconciler can recognize the exact candidate this call created
 * independent of whether its summary text is later edited. This
 * deliberately is NOT an HTML comment (`<!-- ... -->`): markdownToAdf
 * (marklassian) treats a raw HTML comment as an HTML block and drops it
 * entirely, which would silently make the marker never reach Jira at all -
 * the same reason addJiraComment's own marker (see
 * internal/providerwrite/worker.go's jiraCommentMarker) uses plain
 * bracketed text instead of a real comment.
 */
function withIntentMarker(description: string | undefined, intentMarker: string | undefined): string | undefined {
  if (!intentMarker) return description;
  const marker = `[${intentMarker}]`;
  return description ? `${description}\n\n${marker}` : marker;
}

export interface CreateJiraSubtaskCandidate {
  summary: string;
  description?: string;
  additionalFields?: Record<string, unknown>;
  /** Recorded (invisibly) on the created or matched issue so reconciliation can recognize this exact write intent's own subtask. */
  intentMarker?: string;
}

export interface CreateJiraSubtaskParams {
  parentKey: string;
  projectKey: string;
  issueTypeName: string;
  candidates: CreateJiraSubtaskCandidate[];
}

export interface SubtaskResult {
  summary: string;
  issueKey: string;
  outcome: 'created' | 'existing';
}

export interface CreateJiraSubtaskResult {
  results: SubtaskResult[];
}

/**
 * Creates every candidate subtask that does not already exist under
 * parentKey, idempotently within and across calls. Existing children come
 * from the parent issue's own fields.subtasks - Jira returns that array in
 * full, unpaginated, regardless of how many subtasks a parent has - so no
 * default page-size cap can silently hide an existing child the way
 * capping a JQL search at its default maxResults used to (a parent with
 * more subtasks than one search page fit could get a second, duplicate
 * subtask created for a summary that already existed past page one). The
 * in-memory existing-summary map is updated immediately after each create,
 * so two duplicate candidates in the very same request collapse onto one
 * created issue instead of two.
 */
export async function createJiraSubtask(
  client: AtlassianRestClient,
  params: CreateJiraSubtaskParams,
  signal?: AbortSignal,
): Promise<CreateJiraSubtaskResult> {
  const parent = await getJiraIssue(client, { issueIdOrKey: params.parentKey }, signal);
  const existingBySummary = new Map<string, string>();
  for (const child of parent.normalized.subtasks ?? []) {
    if (child.key && child.summary) {
      existingBySummary.set(normalizeSummaryForComparison(child.summary), child.key);
    }
  }

  const results: SubtaskResult[] = [];
  for (const candidate of params.candidates) {
    const normalized = normalizeSummaryForComparison(candidate.summary);
    const existingKey = existingBySummary.get(normalized);
    if (existingKey) {
      results.push({ summary: candidate.summary, issueKey: existingKey, outcome: 'existing' });
      continue;
    }
    const created = await createJiraIssue(client, {
      projectKey: params.projectKey,
      issueTypeName: params.issueTypeName,
      summary: candidate.summary,
      description: withIntentMarker(candidate.description, candidate.intentMarker),
      parent: params.parentKey,
      additionalFields: candidate.additionalFields,
    }, signal);
    // Update the map before the next candidate is considered, so a second
    // occurrence of the same normalized summary later in this same
    // candidates array is treated as existing rather than created again.
    existingBySummary.set(normalized, created.normalized.key);
    results.push({ summary: candidate.summary, issueKey: created.normalized.key, outcome: 'created' });
  }
  return { results };
}
