import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';
import { createHandlers } from '../src/adapter.js';
import { AtlassianMcpClient, loadConfigFromEnv } from '../src/mcpClient.js';
import { getJiraIssue, getConfluencePage, addJiraComment } from '../src/operations.js';
import { manifest } from '../src/manifest.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import { FIXTURE_CONFLUENCE_PAGE, FIXTURE_JIRA_ISSUE, startFakeAtlassianServer } from './fakeAtlassianServer.js';

const TEST_ENV = { ATLASSIAN_MCP_TOKEN: 'fake-token', ATLASSIAN_CLOUD_ID: 'fake-cloud-id' };

/** Builds an AtlassianMcpClient wired to a fresh fake server instance. */
async function fakeClient(): Promise<AtlassianMcpClient> {
  const config = loadConfigFromEnv(TEST_ENV);
  let transport: Transport | undefined;
  const client = new AtlassianMcpClient(config, () => {
    if (!transport) throw new Error('transport requested before fake server started');
    return transport;
  });
  const { clientTransport } = await startFakeAtlassianServer();
  transport = clientTransport;
  return client;
}

describe('manifest', () => {
  test('validates against AdapterManifestSchema', () => {
    const parsed = AdapterManifestSchema.parse(manifest);
    assert.equal(parsed.id, 'atlassian');
    assert.equal(parsed.protocol, 'punakawan.adapter/v1');
    assert.equal(parsed.runtime, 'node');
    assert.deepEqual(parsed.provides, ['jira', 'confluence']);
    assert.deepEqual(parsed.permissions.network.hosts, ['mcp.atlassian.com']);
    assert.deepEqual(parsed.permissions.secrets, ['ATLASSIAN_MCP_TOKEN']);
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
});

describe('getJiraIssue', () => {
  test('normalizes provider, external_id, version, and preserves fields', async () => {
    const client = await fakeClient();
    const { normalized, raw } = await getJiraIssue(client, { issueIdOrKey: 'PROJ-123' });

    assert.equal(normalized.source.provider, 'jira');
    assert.equal(normalized.source.external_id, 'PROJ-123');
    assert.equal(normalized.source.version, FIXTURE_JIRA_ISSUE.fields.updated);
    assert.equal(normalized.source.uri, 'jira://fake-cloud-id/PROJ-123');
    assert.ok(normalized.source.retrieved_at.length > 0);
    assert.equal(normalized.summary, FIXTURE_JIRA_ISSUE.fields.summary);
    assert.equal(normalized.status, 'In Progress');
    assert.equal(normalized.description, FIXTURE_JIRA_ISSUE.fields.description);
    assert.ok(raw, 'raw tool result should be returned alongside normalization');

    await client.close();
  });

  test('rejects an unknown issue key with the fake server error', async () => {
    const client = await fakeClient();
    await assert.rejects(() => getJiraIssue(client, { issueIdOrKey: 'NOPE-1' }), /Unknown issue/);
    await client.close();
  });
});

describe('getConfluencePage', () => {
  test('normalizes provider, external_id, version, and preserves content', async () => {
    const client = await fakeClient();
    const { normalized } = await getConfluencePage(client, { pageId: '987654', contentFormat: 'markdown' });

    assert.equal(normalized.source.provider, 'confluence');
    assert.equal(normalized.source.external_id, '987654');
    assert.equal(normalized.source.version, FIXTURE_CONFLUENCE_PAGE.version.number);
    assert.equal(normalized.source.uri, 'confluence://fake-cloud-id/987654');
    assert.equal(normalized.title, FIXTURE_CONFLUENCE_PAGE.title);
    assert.equal(normalized.spaceKey, FIXTURE_CONFLUENCE_PAGE.spaceKey);
    assert.equal(normalized.content, FIXTURE_CONFLUENCE_PAGE.body.markdown.value);

    await client.close();
  });
});

describe('addJiraComment', () => {
  test('round-trips a comment through the fake server', async () => {
    const client = await fakeClient();
    const result = await addJiraComment(client, { issueIdOrKey: 'PROJ-123', commentBody: 'Can you clarify the repro steps?' });

    assert.ok(result.commentId, 'expected a commentId to come back');
    await client.close();
  });
});

describe('connection reuse', () => {
  test('reuses a single MCP connection across multiple calls', async () => {
    const client = await fakeClient();
    let connectCount = 0;
    const config = loadConfigFromEnv(TEST_ENV);
    const { clientTransport } = await startFakeAtlassianServer();
    const countingClient = new AtlassianMcpClient(config, () => {
      connectCount += 1;
      return clientTransport;
    });

    await getJiraIssue(countingClient, { issueIdOrKey: 'PROJ-123' });
    await getJiraIssue(countingClient, { issueIdOrKey: 'PROJ-123' });
    await getConfluencePage(countingClient, { pageId: '987654' });

    assert.equal(connectCount, 1, 'transport factory should be invoked exactly once across three calls');

    await client.close();
    await countingClient.close();
  });
});

describe('missing configuration fails fast', () => {
  test('missing ATLASSIAN_MCP_TOKEN throws before any network attempt', () => {
    assert.throws(() => loadConfigFromEnv({ ATLASSIAN_CLOUD_ID: 'x' }), /ATLASSIAN_MCP_TOKEN/);
  });

  test('missing ATLASSIAN_CLOUD_ID throws before any network attempt', () => {
    assert.throws(() => loadConfigFromEnv({ ATLASSIAN_MCP_TOKEN: 'x' }), /ATLASSIAN_CLOUD_ID/);
  });

  test('execute() surfaces the config error immediately instead of hanging on a real connection attempt', async () => {
    const handlers = createHandlers({ env: {} });
    await assert.rejects(
      () => handlers.execute({ op: 'atlassian.getJiraIssue', issueIdOrKey: 'PROJ-123' }, new AbortController().signal),
      /ATLASSIAN_MCP_TOKEN/,
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

describe('execute via handlers', () => {
  test('atlassian.getJiraIssue through the full handler dispatch', async () => {
    let transport: Transport | undefined;
    const handlers = createHandlers({
      env: TEST_ENV,
      transportFactory: () => {
        if (!transport) throw new Error('transport requested before fake server started');
        return transport;
      },
    });
    const { clientTransport } = await startFakeAtlassianServer();
    transport = clientTransport;

    const result = (await handlers.execute(
      { op: 'atlassian.getJiraIssue', issueIdOrKey: 'PROJ-123' },
      new AbortController().signal,
    )) as { normalized: { source: { provider: string } } };

    assert.equal(result.normalized.source.provider, 'jira');
    await handlers.shutdown(undefined, new AbortController().signal);
  });

  test('rejects unsupported ops', async () => {
    const handlers = createHandlers({ env: TEST_ENV });
    await assert.rejects(
      () => handlers.execute({ op: 'atlassian.doesNotExist' }, new AbortController().signal),
      /Unsupported op/,
    );
  });
});
