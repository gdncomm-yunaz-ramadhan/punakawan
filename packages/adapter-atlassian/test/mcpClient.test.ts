import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadConfigFromEnv, buildAuthorizationHeader, resolveCloudId, AtlassianMcpClient } from '../src/mcpClient.js';

describe('loadConfigFromEnv', () => {
  test('email is undefined when ATLASSIAN_EMAIL is not set (service-account key case)', () => {
    const config = loadConfigFromEnv({ ATLASSIAN_MCP_TOKEN: 't', ATLASSIAN_HOST: 'team.atlassian.net' });
    assert.equal(config.email, undefined);
  });

  test('email is read from ATLASSIAN_EMAIL when set (personal API token case)', () => {
    const config = loadConfigFromEnv({
      ATLASSIAN_MCP_TOKEN: 't',
      ATLASSIAN_HOST: 'team.atlassian.net',
      ATLASSIAN_EMAIL: 'person@example.com',
    });
    assert.equal(config.email, 'person@example.com');
  });

  test('host is read from ATLASSIAN_HOST', () => {
    const config = loadConfigFromEnv({ ATLASSIAN_MCP_TOKEN: 't', ATLASSIAN_HOST: 'team.atlassian.net' });
    assert.equal(config.host, 'team.atlassian.net');
  });
});

describe('buildAuthorizationHeader', () => {
  test('builds a Bearer header for a service-account key (no email)', () => {
    const header = buildAuthorizationHeader({ token: 'service-account-key' });
    assert.equal(header, 'Bearer service-account-key');
  });

  test('builds a Basic base64(email:token) header for a personal API token', () => {
    const header = buildAuthorizationHeader({ token: 'abc123', email: 'person@example.com' });
    const expected = `Basic ${Buffer.from('person@example.com:abc123', 'utf8').toString('base64')}`;
    assert.equal(header, expected);
    // Sanity-check the encoding round-trips to the documented "email:token" form.
    const decoded = Buffer.from(header.slice('Basic '.length), 'base64').toString('utf8');
    assert.equal(decoded, 'person@example.com:abc123');
  });
});

describe('resolveCloudId', () => {
  test('fetches the tenant-info endpoint for the given host and returns its cloudId', async () => {
    const calls: string[] = [];
    const fakeFetch = async (url: string | URL) => {
      calls.push(String(url));
      return new Response(JSON.stringify({ cloudId: 'abc-123' }), { status: 200 });
    };

    const cloudId = await resolveCloudId('team.atlassian.net', fakeFetch as typeof fetch);
    assert.equal(cloudId, 'abc-123');
    assert.deepEqual(calls, ['https://team.atlassian.net/_edge/tenant_info']);
  });

  test('throws when the endpoint responds with a non-2xx status', async () => {
    const fakeFetch = async () => new Response('not found', { status: 404 });
    await assert.rejects(() => resolveCloudId('team.atlassian.net', fakeFetch as typeof fetch), /404/);
  });

  test('throws when the response body has no cloudId', async () => {
    const fakeFetch = async () => new Response(JSON.stringify({}), { status: 200 });
    await assert.rejects(() => resolveCloudId('team.atlassian.net', fakeFetch as typeof fetch), /did not return a cloudId/);
  });
});

describe('AtlassianMcpClient.getCloudId', () => {
  test('memoizes the resolved cloudId across multiple calls', async () => {
    let resolveCount = 0;
    const client = new AtlassianMcpClient(
      { token: 't', host: 'team.atlassian.net' },
      () => {
        throw new Error('transport should not be requested by getCloudId');
      },
      async (host) => {
        resolveCount += 1;
        return `cloud-for-${host}`;
      },
    );

    assert.equal(await client.getCloudId(), 'cloud-for-team.atlassian.net');
    assert.equal(await client.getCloudId(), 'cloud-for-team.atlassian.net');
    assert.equal(resolveCount, 1, 'resolver should be invoked exactly once across two calls');
  });
});
