import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { GitHubRestClient, loadConfigFromEnv } from '../src/restClient.js';
import { createFakeGitHubRest } from './fakeGitHubRest.js';

describe('loadConfigFromEnv', () => {
  test('reads GITHUB_TOKEN and defaults the API base URL', () => {
    const config = loadConfigFromEnv({ GITHUB_TOKEN: 't' });
    assert.equal(config.token, 't');
    assert.equal(config.apiBaseUrl, 'https://api.github.com');
    assert.equal(config.graphqlUrl, 'https://api.github.com/graphql');
  });

  test('falls back to GH_TOKEN', () => {
    const config = loadConfigFromEnv({ GH_TOKEN: 'gh-cli-token' });
    assert.equal(config.token, 'gh-cli-token');
  });

  test('rejects a missing token', () => {
    assert.throws(() => loadConfigFromEnv({}), /GITHUB_TOKEN/);
  });

  test('honors a custom GHES API base URL', () => {
    const config = loadConfigFromEnv({ GITHUB_TOKEN: 't', GITHUB_API_URL: 'https://ghes.example.com/api/v3' });
    assert.equal(config.apiBaseUrl, 'https://ghes.example.com/api/v3');
    assert.equal(config.graphqlUrl, 'https://ghes.example.com/api/graphql');
  });
});

describe('GitHubRestClient', () => {
  test('sends a Bearer authorization header', async () => {
    const rest = createFakeGitHubRest();
    const client = new GitHubRestClient({ token: 'my-token', apiBaseUrl: 'https://api.github.com', graphqlUrl: 'https://api.github.com/graphql' }, rest.fetch);
    await client.request('/repos/acme/widgets/pulls/42');
    assert.equal(rest.requests[0]?.authorization, 'Bearer my-token');
  });

  test('surfaces a clear error on HTTP failure', async () => {
    const rest = createFakeGitHubRest();
    const client = new GitHubRestClient({ token: 't', apiBaseUrl: 'https://api.github.com', graphqlUrl: 'https://api.github.com/graphql' }, rest.fetch);
    await assert.rejects(() => client.request('/repos/acme/widgets/does-not-exist'), /failed with HTTP 404/);
  });

  test('graphql posts to the graphql endpoint and returns data', async () => {
    const rest = createFakeGitHubRest();
    const client = new GitHubRestClient({ token: 't', apiBaseUrl: 'https://api.github.com', graphqlUrl: 'https://api.github.com/graphql' }, rest.fetch);
    const data = await client.graphql<{ resolveReviewThread: { thread: { isResolved: boolean } } }>(
      'mutation { resolveReviewThread(input: {threadId: $threadId}) { thread { id isResolved } } }',
      { threadId: 'thread-1' },
    );
    assert.equal(data.resolveReviewThread.thread.isResolved, true);
    assert.equal(rest.resolvedThreadIds[0], 'thread-1');
  });
});
