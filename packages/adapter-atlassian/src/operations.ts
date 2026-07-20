import type { AtlassianMcpClient } from './mcpClient.js';
import { normalizeConfluencePage, normalizeJiraIssue } from './normalize.js';

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
  const result = await client.callTool('getJiraIssue', { cloudId: client.cloudId, issueIdOrKey: params.issueIdOrKey });
  const payload = extractPayload(result);
  return { raw: result, normalized: normalizeJiraIssue(payload, client.cloudId) };
}

export interface GetConfluencePageParams {
  pageId: string;
  contentFormat?: string;
}

export async function getConfluencePage(client: AtlassianMcpClient, params: GetConfluencePageParams) {
  const args: Record<string, unknown> = { cloudId: client.cloudId, pageId: params.pageId };
  if (params.contentFormat) args.contentFormat = params.contentFormat;

  const result = await client.callTool('getConfluencePage', args);
  const payload = extractPayload(result);
  return { raw: result, normalized: normalizeConfluencePage(payload, client.cloudId) };
}

export interface SearchJiraParams {
  jql: string;
  fields?: string[];
  maxResults?: number;
}

export async function searchJira(client: AtlassianMcpClient, params: SearchJiraParams) {
  const args: Record<string, unknown> = { cloudId: client.cloudId, jql: params.jql };
  if (params.fields) args.fields = params.fields;
  if (params.maxResults !== undefined) args.maxResults = params.maxResults;

  const result = await client.callTool('searchJiraIssuesUsingJql', args);
  const payload = extractPayload(result);
  const issues = Array.isArray(payload.issues) ? payload.issues : [];
  return {
    raw: result,
    normalized: issues.map((issue) => normalizeJiraIssue(asRecord(issue), client.cloudId)),
  };
}

export interface SearchConfluenceParams {
  cql: string;
}

export async function searchConfluence(client: AtlassianMcpClient, params: SearchConfluenceParams) {
  const result = await client.callTool('searchConfluenceUsingCql', { cloudId: client.cloudId, cql: params.cql });
  const payload = extractPayload(result);
  const pages = Array.isArray(payload.results) ? payload.results : [];
  return {
    raw: result,
    normalized: pages.map((page) => normalizeConfluencePage(asRecord(page), client.cloudId)),
  };
}

export interface AddJiraCommentParams {
  issueIdOrKey: string;
  commentBody: string;
}

export async function addJiraComment(client: AtlassianMcpClient, params: AddJiraCommentParams) {
  const result = await client.callTool('addCommentToJiraIssue', {
    cloudId: client.cloudId,
    issueIdOrKey: params.issueIdOrKey,
    commentBody: params.commentBody,
  });
  const payload = extractPayload(result);
  return { raw: result, commentId: typeof payload.id === 'string' ? payload.id : undefined };
}
