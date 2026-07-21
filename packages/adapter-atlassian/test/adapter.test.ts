import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { createHandlers } from '../src/adapter.js';
import { AtlassianRestClient, loadConfigFromEnv } from '../src/restClient.js';
import {
  getJiraIssue,
  getConfluencePage,
  searchJira,
  searchConfluence,
  addJiraComment,
  getTransitionsForJiraIssue,
  transitionJiraIssue,
  editJiraIssueFields,
  addWorklog,
  getIssueTypeFieldMeta,
  createJiraIssue,
  createJiraSubtask,
  deleteJiraAttachment,
  downloadJiraAttachment,
  editJiraIssue,
  getJiraComments,
  getJiraEpic,
  getJiraRemoteLinks,
  listJiraAttachments,
  uploadJiraAttachment,
} from '../src/operations.js';
import { manifest } from '../src/manifest.js';
import { normalizeJiraIssue } from '../src/normalize.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import {
  FIXTURE_CONFLUENCE_PAGE,
  FIXTURE_JIRA_ISSUE,
  FIXTURE_TRANSITIONS,
  FIXTURE_PARENT_KEY,
  FIXTURE_EXISTING_SUBTASKS,
  FIXTURE_ISSUE_TYPE_FIELD_META,
  FIXTURE_COMMENTS,
  FIXTURE_REMOTE_LINKS,
  adfText,
  createFakeAtlassianRest,
  type FakeAtlassianRest,
} from './fakeAtlassianRest.js';

const TEST_ENV = {
  ATLASSIAN_API_TOKEN: 'fake-token',
  ATLASSIAN_HOST: 'fake-team.atlassian.net',
  ATLASSIAN_EMAIL: 'tester@example.com',
};
/** Stands in for resolveCloudId in tests so no real network request is made. */
const TEST_CLOUD_ID_RESOLVER = async () => 'fake-cloud-id';

function fakeClientWithRest(): { client: AtlassianRestClient; rest: FakeAtlassianRest } {
  const config = loadConfigFromEnv(TEST_ENV);
  const rest = createFakeAtlassianRest();
  return { client: new AtlassianRestClient(config, rest.fetch, TEST_CLOUD_ID_RESOLVER), rest };
}

async function fakeClient(): Promise<AtlassianRestClient> {
  return fakeClientWithRest().client;
}

function fakeHandlers() {
  const rest = createFakeAtlassianRest();
  const handlers = createHandlers({ env: TEST_ENV, fetchImpl: rest.fetch, cloudIdResolver: TEST_CLOUD_ID_RESOLVER });
  return { handlers, rest };
}

describe('manifest', () => {
  test('validates against AdapterManifestSchema', () => {
    const parsed = AdapterManifestSchema.parse(manifest);
    assert.equal(parsed.id, 'atlassian');
    assert.equal(parsed.protocol, 'punakawan.adapter/v1');
    assert.equal(parsed.runtime, 'node');
    assert.deepEqual(parsed.provides, ['jira', 'confluence']);
    assert.deepEqual(parsed.permissions.network.hosts, ['api.atlassian.com', '*.atlassian.net']);
    assert.deepEqual(parsed.permissions.secrets, ['ATLASSIAN_API_TOKEN', 'ATLASSIAN_EMAIL']);
  });

  test('declares the write operation as side-effecting and requiring approval', () => {
    assert.deepEqual(manifest.operations['atlassian.addJiraComment'], { side_effect: true, approval: 'required' });
  });

  test('declares read operations as side-effect free', () => {
    for (const op of ['atlassian.searchJira', 'atlassian.searchConfluence', 'atlassian.getJiraIssue', 'atlassian.getConfluencePage']) {
      assert.equal(manifest.operations[op]?.side_effect, false, `${op} should be side_effect: false`);
      assert.equal(manifest.operations[op]?.approval, undefined, `${op} should not require approval`);
    }
  });

  test('declares the new write operations as side-effecting and requiring approval', () => {
    for (const op of [
      'atlassian.transitionJiraIssue',
      'atlassian.editJiraIssueFields',
      'atlassian.addWorklog',
      'atlassian.createJiraSubtask',
      'atlassian.editJiraIssue',
      'atlassian.downloadJiraAttachment',
      'atlassian.uploadJiraAttachment',
      'atlassian.deleteJiraAttachment',
    ]) {
      assert.deepEqual(manifest.operations[op], { side_effect: true, approval: 'required' }, `${op} should require approval`);
    }
  });

  test('declares the new read operations as side-effect free', () => {
    for (const op of [
      'atlassian.getTransitionsForJiraIssue',
      'atlassian.getIssueTypeFieldMeta',
      'atlassian.getJiraComments',
      'atlassian.getJiraRemoteLinks',
      'atlassian.getJiraEpic',
      'atlassian.listJiraAttachments',
    ]) {
      assert.equal(manifest.operations[op]?.side_effect, false, `${op} should be side_effect: false`);
      assert.equal(manifest.operations[op]?.approval, undefined, `${op} should not require approval`);
    }
  });
});

describe('getJiraIssue', () => {
  test('normalizes the planning fields without returning the raw REST envelope by default', async () => {
    const client = await fakeClient();
    const result = await getJiraIssue(client, { issueIdOrKey: 'PROJ-123' });
    const { normalized } = result;

    assert.equal(normalized.source.provider, 'jira');
    assert.equal(normalized.source.external_id, 'PROJ-123');
    assert.equal(normalized.source.version, FIXTURE_JIRA_ISSUE.fields.updated);
    assert.equal(normalized.source.uri, 'jira://fake-cloud-id/PROJ-123');
    assert.ok(normalized.source.retrieved_at.length > 0);
    assert.equal(normalized.summary, FIXTURE_JIRA_ISSUE.fields.summary);
    assert.equal(normalized.status, 'In Progress');
    assert.equal(normalized.description, FIXTURE_JIRA_ISSUE.fields.description);
    assert.deepEqual(normalized.links, [{
      id: '9001',
      direction: 'outward',
      relationship: 'blocks',
      issue: { key: 'PROJ-124', summary: 'Dependent rollout', status: 'To Do', issueType: 'Task' },
    }]);
    assert.equal('raw' in result, false);

    await client.close();
  });

  test('returns the raw envelope only when explicitly requested', async () => {
    const client = await fakeClient();
    const compact = await getJiraIssue(client, { issueIdOrKey: 'PROJ-123' });
    const diagnostic = await getJiraIssue(client, { issueIdOrKey: 'PROJ-123', includeRaw: true });

    assert.ok(diagnostic.raw);
    assert.ok(JSON.stringify(compact).length < JSON.stringify(diagnostic).length);
    await client.close();
  });

  test('flattens multiline ADF to compact plain text instead of serializing its JSON tree', () => {
    const normalized = normalizeJiraIssue({
      key: 'PROJ-9',
      fields: {
        summary: 'ADF issue',
        description: {
          type: 'doc',
          content: [
            { type: 'paragraph', content: [{ type: 'text', text: 'First line' }] },
            { type: 'paragraph', content: [{ type: 'text', text: 'second line' }] },
          ],
        },
      },
    }, 'cloud');

    assert.equal(normalized.description, 'First line second line');
    assert.equal(normalized.description?.includes('"type"'), false);
  });

  test('rejects an unknown issue key with the fake server error', async () => {
    const client = await fakeClient();
    await assert.rejects(() => getJiraIssue(client, { issueIdOrKey: 'NOPE-1' }), /Unknown issue/);
    await client.close();
  });

});

describe('Jira context expansion', () => {
  test('returns compact paginated comments', async () => {
    const client = await fakeClient();
    const result = await getJiraComments(client, { issueIdOrKey: 'PROJ-123' });
    assert.equal(result.comments[0]?.id, FIXTURE_COMMENTS[0]?.id);
    assert.equal(result.comments[0]?.body, 'Please cover Safari.');
    assert.equal(result.page.total, 1);
    await client.close();
  });

  test('returns bounded remote links', async () => {
    const client = await fakeClient();
    const result = await getJiraRemoteLinks(client, { issueIdOrKey: 'PROJ-123' });
    assert.equal(result.links[0]?.url, FIXTURE_REMOTE_LINKS[0]?.object.url);
    assert.equal(result.links[0]?.relationship, 'specification');
    await client.close();
  });

  test('loads an epic and compact child issues', async () => {
    const client = await fakeClient();
    const result = await getJiraEpic(client, { epicIdOrKey: FIXTURE_PARENT_KEY, maxChildren: 10 });
    assert.equal(result.epic.key, FIXTURE_PARENT_KEY);
    assert.equal(result.children.length, FIXTURE_EXISTING_SUBTASKS.length);
    assert.ok(result.children.every((child) => !('source' in child)));
    await client.close();
  });
});

describe('Jira attachments', () => {
  test('lists compact attachment metadata', async () => {
    const client = await fakeClient();
    const result = await listJiraAttachments(client, { issueIdOrKey: 'PROJ-123' });
    assert.deepEqual(result.attachments[0], {
      id: '7001', filename: 'design.txt', mediaType: 'text/plain', size: 12,
      created: '2026-07-15T10:00:00Z', author: 'Test User',
    });
    await client.close();
  });

  test('downloads only inside the workspace', async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), 'punakawan-attachment-'));
    try {
      const client = await fakeClient();
      const result = await downloadJiraAttachment(client, { attachmentId: '7001', outputPath: 'artifacts/design.txt' }, root);
      assert.equal(await readFile(path.join(root, 'artifacts/design.txt'), 'utf8'), 'fixture attachment');
      assert.equal(result.path, path.join('artifacts', 'design.txt'));
      await assert.rejects(
        () => downloadJiraAttachment(client, { attachmentId: '7001', outputPath: '../escape.txt' }, root),
        /inside the Punakawan workspace/,
      );
      await client.close();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test('uploads a workspace file as multipart and deletes by attachment id', async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), 'punakawan-attachment-'));
    try {
      await writeFile(path.join(root, 'evidence.txt'), 'evidence');
      const { client, rest } = fakeClientWithRest();
      const uploaded = await uploadJiraAttachment(client, { issueIdOrKey: 'PROJ-123', filePath: 'evidence.txt' }, root);
      assert.equal(uploaded.uploaded[0]?.filename, 'evidence.txt');
      assert.deepEqual(rest.uploadedAttachments, [{ issueIdOrKey: 'PROJ-123', filename: 'evidence.txt' }]);
      assert.equal(rest.requests.find((request) => request.path.endsWith('/attachments'))?.xAtlassianToken, 'no-check');
      const deleted = await deleteJiraAttachment(client, { attachmentId: '7002' });
      assert.equal(deleted.deleted, true);
      assert.deepEqual(rest.deletedAttachments, ['7002']);
      await client.close();
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });

  test('rejects attachment paths that escape through symlinks', async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), 'punakawan-attachment-root-'));
    const outside = await mkdtemp(path.join(os.tmpdir(), 'punakawan-attachment-outside-'));
    try {
      await writeFile(path.join(outside, 'secret.txt'), 'secret');
      await symlink(outside, path.join(root, 'escape'));
      const client = await fakeClient();
      await assert.rejects(
        () => uploadJiraAttachment(client, { issueIdOrKey: 'PROJ-123', filePath: 'escape/secret.txt' }, root),
        /escapes the Punakawan workspace through a symlink/,
      );
      await assert.rejects(
        () => downloadJiraAttachment(client, { attachmentId: '7001', outputPath: 'escape/written.txt' }, root),
        /escapes the Punakawan workspace through a symlink/,
      );
      await client.close();
    } finally {
      await rm(root, { recursive: true, force: true });
      await rm(outside, { recursive: true, force: true });
    }
  });
});

describe('getConfluencePage', () => {
  test('normalizes provider, external_id, version, and preserves content', async () => {
    const client = await fakeClient();
    const { normalized } = await getConfluencePage(client, { pageId: '987654', contentFormat: 'storage' });

    assert.equal(normalized.source.provider, 'confluence');
    assert.equal(normalized.source.external_id, '987654');
    assert.equal(normalized.source.version, FIXTURE_CONFLUENCE_PAGE.version.number);
    assert.equal(normalized.source.uri, 'confluence://fake-cloud-id/987654');
    assert.equal(normalized.title, FIXTURE_CONFLUENCE_PAGE.title);
    assert.equal(normalized.spaceKey, FIXTURE_CONFLUENCE_PAGE.space.key);
    assert.equal(normalized.content, FIXTURE_CONFLUENCE_PAGE.body.storage.value);

    await client.close();
  });
});

describe('search operations', () => {
  test('posts JQL, fields, and limit to enhanced Jira search', async () => {
    const { client, rest } = fakeClientWithRest();
    const result = await searchJira(client, { jql: 'project = PROJ', fields: ['summary', 'status'], maxResults: 25 });

    assert.equal(result.normalized.length, 1);
    assert.equal('source' in (result.normalized[0] ?? {}), false, 'search rows should not repeat provenance metadata');
    assert.equal(rest.requests[0]?.path, '/rest/api/3/search/jql');
    assert.deepEqual(rest.requests[0]?.body, {
      jql: 'project = PROJ',
      fields: ['summary', 'status'],
      maxResults: 25,
    });
    await client.close();
  });

  test('defaults to a bounded compact planning search', async () => {
    const { client, rest } = fakeClientWithRest();
    await searchJira(client, { jql: 'project = PROJ' });

    assert.equal(rest.requests[0]?.body.maxResults, 20);
    assert.deepEqual(rest.requests[0]?.body.fields, [
      'summary', 'status', 'issuetype', 'priority', 'assignee', 'parent', 'updated',
    ]);
    await client.close();
  });

  test('uses direct Confluence CQL search', async () => {
    const { client, rest } = fakeClientWithRest();
    const result = await searchConfluence(client, { cql: 'space = ENG' });

    assert.equal(result.normalized[0]?.title, FIXTURE_CONFLUENCE_PAGE.title);
    assert.equal(rest.requests[0]?.path, '/wiki/rest/api/content/search');
    assert.match(rest.requests[0]?.url ?? '', /cql=space(?:\+|%20)%3D(?:\+|%20)ENG/);
    await client.close();
  });
});

describe('addJiraComment', () => {
  test('posts an ADF comment to the direct Jira REST endpoint', async () => {
    const { client, rest } = fakeClientWithRest();
    const result = await addJiraComment(client, { issueIdOrKey: 'PROJ-123', commentBody: 'Can you clarify the repro steps?' });

    assert.ok(result.commentId, 'expected a commentId to come back');
    assert.equal(rest.requests.at(-1)?.path, '/rest/api/3/issue/PROJ-123/comment');
    assert.equal(adfText(rest.addedComments[0]?.body.body), 'Can you clarify the repro steps?');
    await client.close();
  });
});

describe('getTransitionsForJiraIssue', () => {
  test('returns available transitions with id, name, and toStatus', async () => {
    const client = await fakeClient();
    const { transitions } = await getTransitionsForJiraIssue(client, { issueIdOrKey: 'PROJ-123' });

    assert.equal(transitions.length, FIXTURE_TRANSITIONS.length);
    assert.equal(transitions[0]?.id, FIXTURE_TRANSITIONS[0]?.id);
    assert.equal(transitions[0]?.name, FIXTURE_TRANSITIONS[0]?.name);
    assert.equal(transitions[0]?.toStatus.id, FIXTURE_TRANSITIONS[0]?.to.id);
    assert.equal(transitions[0]?.toStatus.name, FIXTURE_TRANSITIONS[0]?.to.name);

    await client.close();
  });

  test('rejects an unknown issue key with the fake server error', async () => {
    const client = await fakeClient();
    await assert.rejects(() => getTransitionsForJiraIssue(client, { issueIdOrKey: 'NOPE-1' }), /Unknown issue/);
    await client.close();
  });
});

describe('transitionJiraIssue', () => {
  test('performs a transition using a discovered transitionId', async () => {
    const { client, rest } = fakeClientWithRest();
    const { payload } = await transitionJiraIssue(client, { issueIdOrKey: 'PROJ-123', transitionId: '11' });

    assert.equal(payload.ok, true);
    assert.deepEqual(rest.transitionedIssues, [{ issueIdOrKey: 'PROJ-123', transitionId: '11' }]);
    await client.close();
  });

  test('rejects an unknown transitionId with the fake server error', async () => {
    const client = await fakeClient();
    await assert.rejects(
      () => transitionJiraIssue(client, { issueIdOrKey: 'PROJ-123', transitionId: 'does-not-exist' }),
      /Unknown transitionId/,
    );
    await client.close();
  });
});

describe('editJiraIssueFields', () => {
  test('passes an arbitrary fields map through to the fake server', async () => {
    const { client, rest } = fakeClientWithRest();
    const fields = { customfield_10016: 5, timetracking: { originalEstimate: '8h' } };
    const result = await editJiraIssueFields(client, {
      issueIdOrKey: 'PROJ-123',
      fields,
    });

    assert.deepEqual(rest.editedFields, [{ issueIdOrKey: 'PROJ-123', fields }]);
    assert.deepEqual(result, { ok: true, issueIdOrKey: 'PROJ-123', updatedFields: ['customfield_10016', 'timetracking'] });
    await client.close();
  });
});

describe('editJiraIssue', () => {
  test('updates title, plain-text description, estimates, story points, and arbitrary fields', async () => {
    const { client, rest } = fakeClientWithRest();
    const result = await editJiraIssue(client, {
      issueIdOrKey: 'PROJ-123',
      title: 'Updated title',
      description: 'Updated description',
      originalEstimate: '8h',
      remainingEstimate: '2h',
      storyPoints: 5,
      storyPointsFieldId: 'customfield_10016',
      fields: { priority: { name: 'High' } },
    });

    const fields = rest.editedFields[0]?.fields ?? {};
    assert.equal(fields.summary, 'Updated title');
    assert.equal(adfText(fields.description), 'Updated description');
    assert.deepEqual(fields.timetracking, { originalEstimate: '8h', remainingEstimate: '2h' });
    assert.equal(fields.customfield_10016, 5);
    assert.deepEqual(fields.priority, { name: 'High' });
    assert.equal(result.ok, true);
    await client.close();
  });

  test('requires the story points custom-field id', async () => {
    const client = await fakeClient();
    await assert.rejects(
      () => editJiraIssue(client, { issueIdOrKey: 'PROJ-123', storyPoints: 3 }),
      /storyPointsFieldId/,
    );
    await client.close();
  });
});

describe('addWorklog', () => {
  test('adds a worklog with a comment and returns an id', async () => {
    const { client, rest } = fakeClientWithRest();
    const result = await addWorklog(client, {
      issueIdOrKey: 'PROJ-123',
      timeSpentSeconds: 3600,
      comment: 'Investigated root cause',
    });

    assert.ok(typeof result.worklogId === 'string' && result.worklogId.length > 0);
    assert.equal(result.timeSpentSeconds, 3600);
    assert.equal(adfText(rest.addedWorklogs[0]?.body.comment), 'Investigated root cause');
    await client.close();
  });

  test('adds a worklog without a comment', async () => {
    const client = await fakeClient();
    const result = await addWorklog(client, { issueIdOrKey: 'PROJ-123', timeSpentSeconds: 1800 });

    assert.equal(result.timeSpentSeconds, 1800);
    await client.close();
  });
});

describe('getIssueTypeFieldMeta', () => {
  test('returns create-field metadata for a project and issue type', async () => {
    const client = await fakeClient();
    const { payload } = await getIssueTypeFieldMeta(client, { projectIdOrKey: 'PROJ', issueTypeId: '10001' });

    assert.deepEqual(payload, FIXTURE_ISSUE_TYPE_FIELD_META);
    await client.close();
  });
});

describe('createJiraIssue', () => {
  test('creates an issue and returns a normalized result', async () => {
    const { client, rest } = fakeClientWithRest();
    const { normalized } = await createJiraIssue(client, {
      projectKey: 'PROJ',
      issueTypeName: 'Sub-task',
      summary: 'A brand new subtask',
      description: 'Some description',
      parent: 'PROJ-200',
    });

    assert.equal(normalized.summary, 'A brand new subtask');
    assert.equal(normalized.source.provider, 'jira');
    assert.deepEqual(rest.createdIssues[0]?.fields.parent, { key: 'PROJ-200' });
    assert.equal(adfText(rest.createdIssues[0]?.fields.description), 'Some description');
    await client.close();
  });
});

describe('createJiraSubtask', () => {
  test('skips an exact-duplicate candidate and creates a genuinely-new one', async () => {
    const client = await fakeClient();
    const result = await createJiraSubtask(client, {
      parentKey: FIXTURE_PARENT_KEY,
      projectKey: 'PROJ',
      issueTypeName: 'Sub-task',
      candidates: [
        // Exact duplicate of an existing subtask, modulo case and whitespace.
        { summary: '  write   UNIT tests  ' },
        // Genuinely new.
        { summary: 'Add integration test for login flow' },
      ],
    });

    assert.equal(result.created.length, 1);
    assert.equal(result.created[0]?.summary, 'Add integration test for login flow');

    assert.equal(result.skipped.length, 1);
    assert.equal(result.skipped[0]?.summary, '  write   UNIT tests  ');
    assert.equal(result.skipped[0]?.existingKey, FIXTURE_EXISTING_SUBTASKS[0]?.key);

    await client.close();
  });

  test('creates all candidates when none match existing subtasks', async () => {
    const client = await fakeClient();
    const result = await createJiraSubtask(client, {
      parentKey: FIXTURE_PARENT_KEY,
      projectKey: 'PROJ',
      issueTypeName: 'Sub-task',
      candidates: [{ summary: 'Totally new subtask A' }, { summary: 'Totally new subtask B' }],
    });

    assert.equal(result.created.length, 2);
    assert.equal(result.skipped.length, 0);

    await client.close();
  });
});

describe('direct REST transport', () => {
  test('never calls the hosted Rovo MCP endpoint', async () => {
    const { client, rest } = fakeClientWithRest();
    await getJiraIssue(client, { issueIdOrKey: 'PROJ-123' });
    await getConfluencePage(client, { pageId: '987654' });

    assert.ok(rest.requests.every((request) => !request.url.includes('mcp.atlassian.com')));
    assert.deepEqual(
      rest.requests.map((request) => request.path),
      ['/rest/api/3/issue/PROJ-123', '/wiki/rest/api/content/987654'],
    );
    await client.close();
  });
});

describe('missing configuration fails fast', () => {
  test('missing ATLASSIAN_API_TOKEN throws before any network attempt', () => {
    assert.throws(() => loadConfigFromEnv({ ATLASSIAN_HOST: 'x.atlassian.net' }), /ATLASSIAN_API_TOKEN/);
  });

  test('missing ATLASSIAN_HOST throws before any network attempt', () => {
    assert.throws(() => loadConfigFromEnv({ ATLASSIAN_API_TOKEN: 'x' }), /ATLASSIAN_HOST/);
  });

  test('execute() surfaces the config error immediately instead of hanging on a real connection attempt', async () => {
    const handlers = createHandlers({ env: {} });
    await assert.rejects(
      () => handlers.execute({ op: 'atlassian.getJiraIssue', issueIdOrKey: 'PROJ-123' }, new AbortController().signal),
      /ATLASSIAN_API_TOKEN/,
    );
  });
});

describe('initialize', () => {
  test('validates the manifest and returns id/version', async () => {
    const handlers = createHandlers({ env: TEST_ENV });
    const result = await handlers.initialize(manifest, new AbortController().signal);
    assert.deepEqual(result, { ok: true, id: 'atlassian', version: manifest.version });
  });
});

describe('capabilities', () => {
  test("returns this adapter's own manifest, independent of initialize params", async () => {
    const handlers = createHandlers({ env: TEST_ENV });
    const result = await handlers.capabilities(undefined, new AbortController().signal);
    assert.deepEqual(result, manifest);
  });
});

describe('execute via handlers', () => {
  test('atlassian.getJiraIssue through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.getJiraIssue', issueIdOrKey: 'PROJ-123' },
      new AbortController().signal,
    )) as { normalized: { source: { provider: string } } };

    assert.equal(result.normalized.source.provider, 'jira');
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('context and attachment read operations dispatch by their public names', async () => {
    const { handlers } = fakeHandlers();
    for (const [op, params] of [
      ['atlassian.getJiraComments', { issueIdOrKey: 'PROJ-123' }],
      ['atlassian.getJiraRemoteLinks', { issueIdOrKey: 'PROJ-123' }],
      ['atlassian.getJiraEpic', { epicIdOrKey: FIXTURE_PARENT_KEY }],
      ['atlassian.listJiraAttachments', { issueIdOrKey: 'PROJ-123' }],
    ] as const) {
      const result = await handlers.execute({ op, ...params }, new AbortController().signal);
      assert.ok(result && typeof result === 'object', `${op} should return an object`);
    }
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('rejects unsupported ops', async () => {
    const handlers = createHandlers({ env: TEST_ENV });
    await assert.rejects(
      () => handlers.execute({ op: 'atlassian.doesNotExist' }, new AbortController().signal),
      /Unsupported op/,
    );
  });

  test('atlassian.getTransitionsForJiraIssue through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.getTransitionsForJiraIssue', issueIdOrKey: 'PROJ-123' },
      new AbortController().signal,
    )) as { transitions: { id: string }[] };

    assert.equal(result.transitions.length, FIXTURE_TRANSITIONS.length);
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.transitionJiraIssue through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.transitionJiraIssue', issueIdOrKey: 'PROJ-123', transitionId: '11' },
      new AbortController().signal,
    )) as { payload: { ok: boolean } };

    assert.equal(result.payload.ok, true);
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.editJiraIssueFields through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.editJiraIssueFields', issueIdOrKey: 'PROJ-123', fields: { customfield_10016: 8 } },
      new AbortController().signal,
    )) as { ok: boolean; updatedFields: string[] };

    assert.equal(result.ok, true);
    assert.deepEqual(result.updatedFields, ['customfield_10016']);
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.editJiraIssue supports the canonical high-level edit name', async () => {
    const { handlers, rest } = fakeHandlers();
    const result = (await handlers.execute(
      { op: 'atlassian.editJiraIssue', issueIdOrKey: 'PROJ-123', title: 'Canonical edit', originalEstimate: '8h' },
      new AbortController().signal,
    )) as { ok: boolean };

    assert.equal(result.ok, true);
    assert.equal(rest.editedFields[0]?.fields.summary, 'Canonical edit');
    assert.deepEqual(rest.editedFields[0]?.fields.timetracking, { originalEstimate: '8h' });
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.addWorklog through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.addWorklog', issueIdOrKey: 'PROJ-123', timeSpentSeconds: 900, comment: 'Quick fix' },
      new AbortController().signal,
    )) as { timeSpentSeconds: number };

    assert.equal(result.timeSpentSeconds, 900);
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.getIssueTypeFieldMeta through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      { op: 'atlassian.getIssueTypeFieldMeta', projectIdOrKey: 'PROJ', issueTypeId: '10001' },
      new AbortController().signal,
    )) as { payload: Record<string, unknown> };

    assert.deepEqual(result.payload, FIXTURE_ISSUE_TYPE_FIELD_META);
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('atlassian.createJiraSubtask through the full handler dispatch', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute(
      {
        op: 'atlassian.createJiraSubtask',
        parentKey: FIXTURE_PARENT_KEY,
        projectKey: 'PROJ',
        issueTypeName: 'Sub-task',
        candidates: [{ summary: 'Update docs' }, { summary: 'Handle edge case for logout' }],
      },
      new AbortController().signal,
    )) as { created: { summary: string }[]; skipped: { summary: string; existingKey: string }[] };

    assert.equal(result.created.length, 1);
    assert.equal(result.created[0]?.summary, 'Handle edge case for logout');
    assert.equal(result.skipped.length, 1);
    assert.equal(result.skipped[0]?.existingKey, FIXTURE_EXISTING_SUBTASKS[1]?.key);

    await handlers.shutdown(undefined, new AbortController().signal);
  });
});
