export const FIXTURE_JIRA_ISSUE = {
  id: '10123',
  key: 'PROJ-123',
  fields: {
    summary: 'Login button is unresponsive on Safari',
    description: 'Steps to reproduce: click the login button on Safari 17. Nothing happens.',
    status: { name: 'In Progress' },
    updated: '2026-07-15T09:30:00.000+0000',
    issuelinks: [{
      id: '9001',
      type: { name: 'Blocks', inward: 'is blocked by', outward: 'blocks' },
      outwardIssue: { key: 'PROJ-124', fields: { summary: 'Dependent rollout', status: { name: 'To Do' }, issuetype: { name: 'Task' } } },
    }],
    attachment: [{ id: '7001', filename: 'design.txt', mimeType: 'text/plain', size: 12, created: '2026-07-15T10:00:00Z', author: { displayName: 'Test User' } }],
  },
};

export const FIXTURE_COMMENTS = [
  { id: '8001', author: { displayName: 'Product Owner' }, body: { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Please cover Safari.' }] }] }, created: '2026-07-15T10:00:00Z', updated: '2026-07-15T10:00:00Z' },
];

export const FIXTURE_REMOTE_LINKS = [
  { id: 42, globalId: 'spec-42', relationship: 'specification', object: { title: 'Login design', summary: 'Auth flow', url: 'https://docs.example.test/login' } },
];

export const FIXTURE_CONFLUENCE_PAGE = {
  id: '987654',
  title: 'Authentication service design',
  space: { key: 'ENG' },
  version: { number: 4 },
  body: { storage: { value: '<h1>Authentication service design</h1><p>Overview of the auth flow.</p>' } },
};

export const FIXTURE_TRANSITIONS = [
  { id: '11', name: 'Start Progress', to: { id: '3', name: 'In Progress' } },
  { id: '21', name: 'Done', to: { id: '10001', name: 'Done' } },
];

export const FIXTURE_PARENT_KEY = 'PROJ-200';
export const FIXTURE_EXISTING_SUBTASKS = [
  { key: 'PROJ-201', fields: { summary: 'Write unit tests', status: { name: 'To Do' }, updated: '2026-07-10T00:00:00.000+0000' } },
  { key: 'PROJ-202', fields: { summary: 'Update docs', status: { name: 'To Do' }, updated: '2026-07-10T00:00:00.000+0000' } },
  { key: 'PROJ-203', fields: { summary: 'Fix flaky CI job', status: { name: 'Done' }, updated: '2026-07-10T00:00:00.000+0000' } },
];

export const FIXTURE_ISSUE_TYPE_FIELD_META = {
  fields: {
    summary: { required: true, name: 'Summary', key: 'summary' },
    customfield_10016: {
      required: false,
      name: 'Story point estimate',
      key: 'customfield_10016',
      schema: { type: 'number' },
    },
  },
};

export interface RecordedRestRequest {
  method: string;
  url: string;
  path: string;
  authorization: string | undefined;
  xAtlassianToken: string | undefined;
  body: Record<string, unknown>;
}

export interface FakeAtlassianRest {
  fetch: typeof fetch;
  requests: RecordedRestRequest[];
  addedComments: { issueIdOrKey: string; body: Record<string, unknown> }[];
  transitionedIssues: { issueIdOrKey: string; transitionId: string }[];
  editedFields: { issueIdOrKey: string; fields: Record<string, unknown> }[];
  addedWorklogs: { issueIdOrKey: string; body: Record<string, unknown> }[];
  createdIssues: { key: string; fields: Record<string, unknown> }[];
  uploadedAttachments: { issueIdOrKey: string; filename: string }[];
  deletedAttachments: string[];
}

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
}

function adfText(value: unknown): string | undefined {
  const body = value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
  const content = Array.isArray(body.content) ? body.content : [];
  const paragraph = content[0] && typeof content[0] === 'object' ? (content[0] as Record<string, unknown>) : {};
  const inline = Array.isArray(paragraph.content) ? paragraph.content : [];
  const text = inline[0] && typeof inline[0] === 'object' ? (inline[0] as Record<string, unknown>).text : undefined;
  return typeof text === 'string' ? text : undefined;
}

/** In-memory fetch implementation that exercises real REST request mapping without network access. */
export function createFakeAtlassianRest(): FakeAtlassianRest {
  const requests: RecordedRestRequest[] = [];
  const addedComments: FakeAtlassianRest['addedComments'] = [];
  const transitionedIssues: FakeAtlassianRest['transitionedIssues'] = [];
  const editedFields: FakeAtlassianRest['editedFields'] = [];
  const addedWorklogs: FakeAtlassianRest['addedWorklogs'] = [];
  const createdIssues: FakeAtlassianRest['createdIssues'] = [];
  const uploadedAttachments: FakeAtlassianRest['uploadedAttachments'] = [];
  const deletedAttachments: string[] = [];

  const fakeFetch = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
    const method = (init?.method ?? 'GET').toUpperCase();
    const parsedBody = typeof init?.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    const headers = new Headers(init?.headers);
    const path = url.pathname.replace(/^\/ex\/(?:jira|confluence)\/[^/]+/, '');
    requests.push({
      method,
      url: url.toString(),
      path,
      authorization: headers.get('Authorization') ?? undefined,
      xAtlassianToken: headers.get('X-Atlassian-Token') ?? undefined,
      body: parsedBody,
    });

    if (path === '/_edge/tenant_info' && method === 'GET') return json({ cloudId: 'fake-cloud-id' });

    if (path === '/rest/api/3/search/jql' && method === 'POST') {
      const jql = typeof parsedBody.jql === 'string' ? parsedBody.jql : '';
      return json({ issues: jql.includes(`parent = "${FIXTURE_PARENT_KEY}"`) ? FIXTURE_EXISTING_SUBTASKS : [FIXTURE_JIRA_ISSUE] });
    }

    const issueMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)$/);
    if (issueMatch && method === 'GET') {
      const key = decodeURIComponent(issueMatch[1] ?? '');
      if (key === FIXTURE_JIRA_ISSUE.key) return json(FIXTURE_JIRA_ISSUE);
      if (key === FIXTURE_PARENT_KEY) return json({
        id: '10200', key: FIXTURE_PARENT_KEY,
        fields: { summary: 'Authentication epic', status: { name: 'In Progress' }, issuetype: { name: 'Epic' }, updated: '2026-07-10T00:00:00.000+0000' },
      });
      const created = createdIssues.find((issue) => issue.key === key);
      if (created) {
        return json({
          id: String(10300 + createdIssues.indexOf(created)),
          key,
          fields: {
            summary: created.fields.summary,
            description: created.fields.description,
            status: { name: 'To Do' },
            updated: '2026-07-21T00:00:00.000+0000',
          },
        });
      }
      return json({ errorMessages: [`Unknown issue: ${key}`] }, 404);
    }

    const commentMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)\/comment$/);
    if (commentMatch && method === 'GET') {
      return json({ startAt: 0, maxResults: 20, total: FIXTURE_COMMENTS.length, comments: FIXTURE_COMMENTS });
    }
    if (commentMatch && method === 'POST') {
      addedComments.push({ issueIdOrKey: decodeURIComponent(commentMatch[1] ?? ''), body: parsedBody });
      return json({ id: String(5000 + addedComments.length), body: parsedBody.body }, 201);
    }

    const remoteLinkMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)\/remotelink$/);
    if (remoteLinkMatch && method === 'GET') return json(FIXTURE_REMOTE_LINKS);

    const attachmentContentMatch = path.match(/^\/rest\/api\/3\/attachment\/content\/([^/]+)$/);
    if (attachmentContentMatch && method === 'GET') {
      return new Response('fixture attachment', { status: 200, headers: { 'Content-Type': 'text/plain' } });
    }

    const attachmentDeleteMatch = path.match(/^\/rest\/api\/3\/attachment\/([^/]+)$/);
    if (attachmentDeleteMatch && method === 'DELETE') {
      deletedAttachments.push(decodeURIComponent(attachmentDeleteMatch[1] ?? ''));
      return new Response(null, { status: 204 });
    }

    const attachmentUploadMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)\/attachments$/);
    if (attachmentUploadMatch && method === 'POST') {
      const form = init?.body instanceof FormData ? init.body : undefined;
      const file = form?.get('file');
      const filename = file instanceof File ? file.name : 'unknown';
      const issueIdOrKey = decodeURIComponent(attachmentUploadMatch[1] ?? '');
      uploadedAttachments.push({ issueIdOrKey, filename });
      return json([{ id: '7002', filename, mimeType: file instanceof File ? file.type : undefined, size: file instanceof File ? file.size : undefined }], 200);
    }

    const transitionMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)\/transitions$/);
    if (transitionMatch && method === 'GET') {
      const key = decodeURIComponent(transitionMatch[1] ?? '');
      return key === FIXTURE_JIRA_ISSUE.key
        ? json({ transitions: FIXTURE_TRANSITIONS })
        : json({ errorMessages: [`Unknown issue: ${key}`] }, 404);
    }
    if (transitionMatch && method === 'POST') {
      const transition = parsedBody.transition && typeof parsedBody.transition === 'object'
        ? (parsedBody.transition as Record<string, unknown>)
        : {};
      const transitionId = typeof transition.id === 'string' ? transition.id : '';
      if (!FIXTURE_TRANSITIONS.some((candidate) => candidate.id === transitionId)) {
        return json({ errorMessages: [`Unknown transitionId: ${transitionId}`] }, 400);
      }
      transitionedIssues.push({ issueIdOrKey: decodeURIComponent(transitionMatch[1] ?? ''), transitionId });
      return new Response(null, { status: 204 });
    }

    if (issueMatch && method === 'PUT') {
      const fields = parsedBody.fields && typeof parsedBody.fields === 'object'
        ? (parsedBody.fields as Record<string, unknown>)
        : {};
      editedFields.push({ issueIdOrKey: decodeURIComponent(issueMatch[1] ?? ''), fields });
      return json({ ...FIXTURE_JIRA_ISSUE, fields: { ...FIXTURE_JIRA_ISSUE.fields, ...fields } });
    }

    const worklogMatch = path.match(/^\/rest\/api\/3\/issue\/([^/]+)\/worklog$/);
    if (worklogMatch && method === 'POST') {
      addedWorklogs.push({ issueIdOrKey: decodeURIComponent(worklogMatch[1] ?? ''), body: parsedBody });
      return json({ id: String(6000 + addedWorklogs.length), ...parsedBody }, 201);
    }

    if (path.match(/^\/rest\/api\/3\/issue\/createmeta\/[^/]+\/issuetypes\/[^/]+$/) && method === 'GET') {
      return json(FIXTURE_ISSUE_TYPE_FIELD_META);
    }

    if (path === '/rest/api/3/issue' && method === 'POST') {
      const fields = parsedBody.fields && typeof parsedBody.fields === 'object'
        ? (parsedBody.fields as Record<string, unknown>)
        : {};
      const project = fields.project && typeof fields.project === 'object' ? (fields.project as Record<string, unknown>) : {};
      const projectKey = typeof project.key === 'string' ? project.key : 'PROJ';
      const key = `${projectKey}-${300 + createdIssues.length + 1}`;
      createdIssues.push({ key, fields });
      return json({ id: String(10300 + createdIssues.length), key, self: `https://fake-team.atlassian.net/rest/api/3/issue/${key}` }, 201);
    }

    if (path === `/wiki/rest/api/content/${FIXTURE_CONFLUENCE_PAGE.id}` && method === 'GET') {
      return json(FIXTURE_CONFLUENCE_PAGE);
    }
    if (path === '/wiki/rest/api/content/search' && method === 'GET') {
      return json({ results: [FIXTURE_CONFLUENCE_PAGE] });
    }

    return json({ errorMessages: [`Unhandled fake REST route: ${method} ${path}`] }, 404);
  };

  return {
    fetch: fakeFetch as typeof fetch,
    requests,
    addedComments,
    transitionedIssues,
    editedFields,
    addedWorklogs,
    createdIssues,
    uploadedAttachments,
    deletedAttachments,
  };
}

export { adfText };
