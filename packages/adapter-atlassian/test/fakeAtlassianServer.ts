import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { InMemoryTransport } from '@modelcontextprotocol/sdk/inMemory.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import { z } from 'zod';

/**
 * A fake local MCP server responding to the real Atlassian tool names
 * (getJiraIssue/getConfluencePage/searchJiraIssuesUsingJql/
 * searchConfluenceUsingCql/addCommentToJiraIssue, confirmed via Context7
 * `/atlassian/atlassian-mcp-server`) with realistic fixture data shaped like
 * the real tools' inputs/outputs. Used in place of the real
 * mcp.atlassian.com endpoint so tests exercise real MCP client/server wire
 * behavior without network access.
 */

export const FIXTURE_JIRA_ISSUE = {
  key: 'PROJ-123',
  fields: {
    summary: 'Login button is unresponsive on Safari',
    description: 'Steps to reproduce: click the login button on Safari 17. Nothing happens.',
    status: { name: 'In Progress' },
    updated: '2026-07-15T09:30:00.000+0000',
  },
};

export const FIXTURE_CONFLUENCE_PAGE = {
  id: '987654',
  title: 'Authentication service design',
  spaceKey: 'ENG',
  version: { number: 4 },
  contentFormat: 'markdown',
  body: { markdown: { value: '# Authentication service design\n\nOverview of the auth flow.' } },
};

export interface FakeServerHandle {
  serverTransport: Transport;
  addedComments: { issueIdOrKey: string; commentBody: string }[];
}

/**
 * Builds an McpServer with the Atlassian tool set registered, and returns
 * one end of an in-memory linked transport pair (the caller connects the
 * other end to a real Client).
 */
export function createFakeAtlassianServer(): { server: McpServer; clientTransport: Transport; handle: FakeServerHandle } {
  const server = new McpServer({ name: 'fake-atlassian-mcp', version: '0.0.1' });
  const handle: FakeServerHandle = { serverTransport: undefined as unknown as Transport, addedComments: [] };

  server.registerTool(
    'getJiraIssue',
    {
      description: 'Fetch full details for a specific Jira issue.',
      inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string() }),
    },
    async ({ cloudId, issueIdOrKey }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      if (issueIdOrKey !== FIXTURE_JIRA_ISSUE.key) {
        return { isError: true, content: [{ type: 'text', text: `Unknown issue: ${issueIdOrKey}` }] };
      }
      return {
        content: [{ type: 'text', text: JSON.stringify(FIXTURE_JIRA_ISSUE) }],
        structuredContent: FIXTURE_JIRA_ISSUE,
      };
    },
  );

  server.registerTool(
    'getConfluencePage',
    {
      description: 'Fetch a Confluence page by id.',
      inputSchema: z.object({ cloudId: z.string(), pageId: z.string(), contentFormat: z.string().optional() }),
    },
    async ({ cloudId, pageId }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      if (pageId !== FIXTURE_CONFLUENCE_PAGE.id) {
        return { isError: true, content: [{ type: 'text', text: `Unknown page: ${pageId}` }] };
      }
      return {
        content: [{ type: 'text', text: JSON.stringify(FIXTURE_CONFLUENCE_PAGE) }],
        structuredContent: FIXTURE_CONFLUENCE_PAGE,
      };
    },
  );

  server.registerTool(
    'searchJiraIssuesUsingJql',
    {
      description: 'Search Jira issues using JQL.',
      inputSchema: z.object({
        cloudId: z.string(),
        jql: z.string(),
        fields: z.array(z.string()).optional(),
        maxResults: z.number().optional(),
      }),
    },
    async ({ cloudId }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      const payload = { issues: [FIXTURE_JIRA_ISSUE] };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'searchConfluenceUsingCql',
    {
      description: 'Search Confluence content using CQL.',
      inputSchema: z.object({ cloudId: z.string(), cql: z.string() }),
    },
    async ({ cloudId }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      const payload = { results: [FIXTURE_CONFLUENCE_PAGE] };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'addCommentToJiraIssue',
    {
      description: 'Add a comment to a Jira issue.',
      inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string(), commentBody: z.string() }),
    },
    async ({ cloudId, issueIdOrKey, commentBody }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      handle.addedComments.push({ issueIdOrKey, commentBody });
      const payload = { id: `comment-${handle.addedComments.length}`, body: commentBody };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  handle.serverTransport = serverTransport;

  return { server, clientTransport, handle };
}

/** Connects the fake server's server-side transport; call before connecting the client. */
export async function startFakeAtlassianServer(): Promise<{ clientTransport: Transport; handle: FakeServerHandle }> {
  const { server, clientTransport, handle } = createFakeAtlassianServer();
  await server.connect(handle.serverTransport);
  return { clientTransport, handle };
}
