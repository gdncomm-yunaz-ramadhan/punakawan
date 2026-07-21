import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  AtlassianRestClient,
  buildAuthorizationHeader,
  loadConfigFromEnv,
  resolveCloudId,
} from '../src/restClient.js';

describe('loadConfigFromEnv', () => {
  test('personal tokens default to unscoped direct-site mode', () => {
    const config = loadConfigFromEnv({
      ATLASSIAN_API_TOKEN: 't',
      ATLASSIAN_HOST: 'team.atlassian.net',
      ATLASSIAN_EMAIL: 'person@example.com',
    });
    assert.equal(config.email, 'person@example.com');
    assert.equal(config.scoped, false);
  });

  test('service-account tokens default to scoped gateway mode', () => {
    const config = loadConfigFromEnv({ ATLASSIAN_API_TOKEN: 't', ATLASSIAN_HOST: 'team.atlassian.net' });
    assert.equal(config.email, undefined);
    assert.equal(config.scoped, true);
  });

  test('explicit scoped mode supports scoped personal tokens', () => {
    const config = loadConfigFromEnv({
      ATLASSIAN_API_TOKEN: 't',
      ATLASSIAN_HOST: 'https://team.atlassian.net',
      ATLASSIAN_EMAIL: 'person@example.com',
      ATLASSIAN_API_TOKEN_SCOPED: 'yes',
    });
    assert.equal(config.host, 'team.atlassian.net');
    assert.equal(config.scoped, true);
  });

  test('accepts the former token variable during migration', () => {
    const config = loadConfigFromEnv({
      ATLASSIAN_MCP_TOKEN: 'legacy',
      ATLASSIAN_HOST: 'team.atlassian.net',
      ATLASSIAN_EMAIL: 'person@example.com',
    });
    assert.equal(config.token, 'legacy');
  });

  test('rejects invalid scoped mode', () => {
    assert.throws(
      () => loadConfigFromEnv({ ATLASSIAN_API_TOKEN: 't', ATLASSIAN_HOST: 'team.atlassian.net', ATLASSIAN_API_TOKEN_SCOPED: 'maybe' }),
      /must be true\/false/,
    );
  });
});

describe('buildAuthorizationHeader', () => {
  test('builds Bearer auth for service-account tokens', () => {
    assert.equal(buildAuthorizationHeader({ token: 'service-token' }), 'Bearer service-token');
  });

  test('builds Basic email:token auth for personal tokens', () => {
    const header = buildAuthorizationHeader({ token: 'abc123', email: 'person@example.com' });
    assert.equal(Buffer.from(header.slice('Basic '.length), 'base64').toString('utf8'), 'person@example.com:abc123');
  });
});

describe('resolveCloudId', () => {
  test('uses the tenant-info endpoint', async () => {
    const calls: string[] = [];
    const fakeFetch = async (url: string | URL) => {
      calls.push(String(url));
      return new Response(JSON.stringify({ cloudId: 'abc-123' }), { status: 200 });
    };
    assert.equal(await resolveCloudId('team.atlassian.net', fakeFetch as typeof fetch), 'abc-123');
    assert.deepEqual(calls, ['https://team.atlassian.net/_edge/tenant_info']);
  });

  test('rejects an invalid tenant response', async () => {
    const fakeFetch = async () => new Response('{}', { status: 200 });
    await assert.rejects(() => resolveCloudId('team.atlassian.net', fakeFetch as typeof fetch), /did not return a cloudId/);
  });
});

describe('AtlassianRestClient', () => {
  test('uses the site URL and Basic auth for unscoped personal tokens', async () => {
    let request: { url: string; authorization: string | null } | undefined;
    const fakeFetch = async (input: string | URL | Request, init?: RequestInit) => {
      request = {
        url: String(input),
        authorization: new Headers(init?.headers).get('Authorization'),
      };
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    };
    const client = new AtlassianRestClient(
      { token: 't', host: 'team.atlassian.net', email: 'person@example.com', scoped: false },
      fakeFetch as typeof fetch,
      async () => 'cloud-id',
    );

    await client.jira('/rest/api/3/issue/PROJ-1');
    assert.equal(request?.url, 'https://team.atlassian.net/rest/api/3/issue/PROJ-1');
    assert.match(request?.authorization ?? '', /^Basic /);
  });

  test('uses the API gateway and Bearer auth for scoped service-account tokens', async () => {
    let request: { url: string; authorization: string | null } | undefined;
    const fakeFetch = async (input: string | URL | Request, init?: RequestInit) => {
      request = { url: String(input), authorization: new Headers(init?.headers).get('Authorization') };
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    };
    const client = new AtlassianRestClient(
      { token: 'service-token', host: 'team.atlassian.net', scoped: true },
      fakeFetch as typeof fetch,
      async () => 'cloud-id',
    );

    await client.jira('/rest/api/3/issue/PROJ-1');
    assert.equal(request?.url, 'https://api.atlassian.com/ex/jira/cloud-id/rest/api/3/issue/PROJ-1');
    assert.equal(request?.authorization, 'Bearer service-token');
  });

  test('surfaces HTTP errors with direct-auth guidance', async () => {
    const fakeFetch = async () => new Response(JSON.stringify({ errorMessages: ['Forbidden'] }), { status: 403 });
    const client = new AtlassianRestClient(
      { token: 'bad', host: 'team.atlassian.net', email: 'person@example.com', scoped: false },
      fakeFetch as typeof fetch,
      async () => 'cloud-id',
    );
    await assert.rejects(() => client.jira('/rest/api/3/issue/PROJ-1'), /API token.*Jira\/Confluence permissions/);
  });

  test('memoizes cloud ID resolution', async () => {
    let count = 0;
    const client = new AtlassianRestClient(
      { token: 't', host: 'team.atlassian.net', scoped: true },
      fetch,
      async () => {
        count += 1;
        return 'cloud-id';
      },
    );
    assert.equal(await client.getCloudId(), 'cloud-id');
    assert.equal(await client.getCloudId(), 'cloud-id');
    assert.equal(count, 1);
  });
});
