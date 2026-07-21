import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { loadConfigFromEnv, buildAuthorizationHeader } from '../src/mcpClient.js';

describe('loadConfigFromEnv', () => {
  test('email is undefined when ATLASSIAN_EMAIL is not set (service-account key case)', () => {
    const config = loadConfigFromEnv({ ATLASSIAN_MCP_TOKEN: 't', ATLASSIAN_CLOUD_ID: 'c' });
    assert.equal(config.email, undefined);
  });

  test('email is read from ATLASSIAN_EMAIL when set (personal API token case)', () => {
    const config = loadConfigFromEnv({
      ATLASSIAN_MCP_TOKEN: 't',
      ATLASSIAN_CLOUD_ID: 'c',
      ATLASSIAN_EMAIL: 'person@example.com',
    });
    assert.equal(config.email, 'person@example.com');
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
