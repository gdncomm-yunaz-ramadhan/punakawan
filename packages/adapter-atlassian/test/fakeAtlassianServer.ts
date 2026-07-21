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

/** Fixture workflow transitions for FIXTURE_JIRA_ISSUE, shaped per the ASSUMPTION documented on getTransitionsForJiraIssue in src/operations.ts. */
export const FIXTURE_TRANSITIONS = [
  { id: '11', name: 'Start Progress', to: { id: '3', name: 'In Progress' } },
  { id: '21', name: 'Done', to: { id: '10001', name: 'Done' } },
];

/** Fixture parent issue with existing subtasks, used to test createJiraSubtask's dedup logic. */
export const FIXTURE_PARENT_KEY = 'PROJ-200';
export const FIXTURE_EXISTING_SUBTASKS = [
  { key: 'PROJ-201', fields: { summary: 'Write unit tests', status: { name: 'To Do' }, updated: '2026-07-10T00:00:00.000+0000' } },
  { key: 'PROJ-202', fields: { summary: 'Update docs', status: { name: 'To Do' }, updated: '2026-07-10T00:00:00.000+0000' } },
  { key: 'PROJ-203', fields: { summary: 'Fix flaky CI job', status: { name: 'Done' }, updated: '2026-07-10T00:00:00.000+0000' } },
];

/** Fixture issue-type field metadata, keyed loosely as (projectIdOrKey, issueTypeId). */
export const FIXTURE_ISSUE_TYPE_FIELD_META = {
  fields: {
    summary: { required: true, name: 'Summary', key: 'summary' },
    customfield_10016: { required: false, name: 'Story point estimate', key: 'customfield_10016', schema: { type: 'number' } },
  },
};

export interface FakeServerHandle {
  serverTransport: Transport;
  addedComments: { issueIdOrKey: string; commentBody: string }[];
  transitionedIssues: { issueIdOrKey: string; transitionId: string }[];
  editedFields: { issueIdOrKey: string; fields: Record<string, unknown> }[];
  addedWorklogs: { issueIdOrKey: string; timeSpentSeconds: number; comment?: string }[];
  createdIssues: { projectKey: string; issueTypeName: string; summary: string; description?: string; parent?: string }[];
}

export interface FakeAtlassianServerOptions {
  omitGetJiraIssue?: boolean;
  getJiraIssueError?: string;
  includeTeamworkGraphObject?: boolean;
}

/**
 * Builds an McpServer with the Atlassian tool set registered, and returns
 * one end of an in-memory linked transport pair (the caller connects the
 * other end to a real Client).
 */
export function createFakeAtlassianServer(
  options: FakeAtlassianServerOptions = {},
): { server: McpServer; clientTransport: Transport; handle: FakeServerHandle } {
  const server = new McpServer({ name: 'fake-atlassian-mcp', version: '0.0.1' });
  const handle: FakeServerHandle = {
    serverTransport: undefined as unknown as Transport,
    addedComments: [],
    transitionedIssues: [],
    editedFields: [],
    addedWorklogs: [],
    createdIssues: [],
  };

  if (!options.omitGetJiraIssue) {
    server.registerTool(
      'getJiraIssue',
      {
        description: 'Fetch full details for a specific Jira issue.',
        inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string() }),
      },
      async ({ cloudId, issueIdOrKey }) => {
        if (options.getJiraIssueError) {
          return { isError: true, content: [{ type: 'text' as const, text: options.getJiraIssueError }] };
        }
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
  }

  if (options.includeTeamworkGraphObject) {
    server.registerTool(
      'getTeamworkGraphObject',
      {
        description: 'Fetch Teamwork Graph objects.',
        inputSchema: z.object({ cloudId: z.string(), objects: z.array(z.string()) }),
      },
      async () => ({ content: [{ type: 'text', text: JSON.stringify({ objects: [] }) }] }),
    );
  }

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
    async ({ cloudId, jql }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      if (jql.includes(`parent = "${FIXTURE_PARENT_KEY}"`)) {
        const payload = { issues: FIXTURE_EXISTING_SUBTASKS };
        return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
      }
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

  server.registerTool(
    'getTransitionsForJiraIssue',
    {
      description: 'List available workflow transitions for an issue.',
      inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string() }),
    },
    async ({ cloudId, issueIdOrKey }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      if (issueIdOrKey !== FIXTURE_JIRA_ISSUE.key) {
        return { isError: true, content: [{ type: 'text', text: `Unknown issue: ${issueIdOrKey}` }] };
      }
      const payload = { transitions: FIXTURE_TRANSITIONS };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'transitionJiraIssue',
    {
      description: 'Perform a workflow transition on a Jira issue.',
      inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string(), transitionId: z.string() }),
    },
    async ({ cloudId, issueIdOrKey, transitionId }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      const known = FIXTURE_TRANSITIONS.some((t) => t.id === transitionId);
      if (!known) {
        return { isError: true, content: [{ type: 'text', text: `Unknown transitionId: ${transitionId}` }] };
      }
      handle.transitionedIssues.push({ issueIdOrKey, transitionId });
      const payload = { ok: true };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'editJiraIssue',
    {
      description: 'Update fields on an existing Jira issue.',
      inputSchema: z.object({ cloudId: z.string(), issueIdOrKey: z.string(), fields: z.record(z.string(), z.unknown()) }),
    },
    async ({ cloudId, issueIdOrKey, fields }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      handle.editedFields.push({ issueIdOrKey, fields });
      const payload = { ok: true };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'addWorklogToJiraIssue',
    {
      description: 'Adds a time-tracking worklog to a Jira issue.',
      inputSchema: z.object({
        cloudId: z.string(),
        issueIdOrKey: z.string(),
        timeSpentSeconds: z.number(),
        comment: z.string().optional(),
      }),
    },
    async ({ cloudId, issueIdOrKey, timeSpentSeconds, comment }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      handle.addedWorklogs.push({ issueIdOrKey, timeSpentSeconds, comment });
      const payload = { id: `worklog-${handle.addedWorklogs.length}`, timeSpentSeconds, comment };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'getJiraIssueTypeMetaWithFields',
    {
      description: 'Get create-field metadata for a project and issue type.',
      inputSchema: z.object({ cloudId: z.string(), projectIdOrKey: z.string(), issueTypeId: z.string() }),
    },
    async ({ cloudId }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      const payload = FIXTURE_ISSUE_TYPE_FIELD_META;
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  server.registerTool(
    'createJiraIssue',
    {
      description: 'Create a new Jira issue.',
      inputSchema: z.object({
        cloudId: z.string(),
        projectKey: z.string(),
        issueTypeName: z.string(),
        summary: z.string(),
        description: z.string().optional(),
        parent: z.string().optional(),
        additional_fields: z.record(z.string(), z.unknown()).optional(),
      }),
    },
    async ({ cloudId, projectKey, issueTypeName, summary, description, parent }) => {
      if (!cloudId) return { isError: true, content: [{ type: 'text', text: 'cloudId is required' }] };
      handle.createdIssues.push({ projectKey, issueTypeName, summary, description, parent });
      const key = `${projectKey}-${300 + handle.createdIssues.length}`;
      const payload = {
        key,
        fields: {
          summary,
          description,
          status: { name: 'To Do' },
          updated: '2026-07-21T00:00:00.000+0000',
        },
      };
      return { content: [{ type: 'text', text: JSON.stringify(payload) }], structuredContent: payload };
    },
  );

  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  handle.serverTransport = serverTransport;

  return { server, clientTransport, handle };
}

/** Connects the fake server's server-side transport; call before connecting the client. */
export async function startFakeAtlassianServer(
  options: FakeAtlassianServerOptions = {},
): Promise<{ clientTransport: Transport; handle: FakeServerHandle }> {
  const { server, clientTransport, handle } = createFakeAtlassianServer(options);
  await server.connect(handle.serverTransport);
  return { clientTransport, handle };
}
