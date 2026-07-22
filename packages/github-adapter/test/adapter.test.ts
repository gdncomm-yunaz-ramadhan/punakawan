import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { createHandlers } from '../src/adapter.js';
import { manifest } from '../src/manifest.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import { createFakeGitHubRest, FIXTURE_PR_NUMBER, FIXTURE_REPO, type FakeGitHubRest } from './fakeGitHubRest.js';

function fakeHandlers(): { handlers: ReturnType<typeof createHandlers>; rest: FakeGitHubRest } {
  const rest = createFakeGitHubRest();
  const handlers = createHandlers({ fetchImpl: rest.fetch, env: { GITHUB_TOKEN: 'fake-token' } });
  return { handlers, rest };
}

describe('manifest', () => {
  test('validates against the shared AdapterManifest schema', () => {
    assert.doesNotThrow(() => AdapterManifestSchema.parse(manifest));
  });

  test('declares every write operation as approval-required', () => {
    const writeOps = ['github.createPullRequest', 'github.addLabels', 'github.requestReviewers', 'github.replyToReviewComment', 'github.resolveReviewThread'];
    for (const op of writeOps) {
      assert.equal(manifest.operations[op]?.side_effect, true, `${op} should be side_effect: true`);
      assert.equal(manifest.operations[op]?.approval, 'required', `${op} should require approval`);
    }
  });

  test('declares every read operation as side-effect free', () => {
    const readOps = ['github.getPullRequest', 'github.getPullRequestFiles', 'github.getPullRequestChecks', 'github.listPullRequestComments'];
    for (const op of readOps) {
      assert.equal(manifest.operations[op]?.side_effect, false, `${op} should be side_effect: false`);
    }
  });
});

describe('createHandlers().execute', () => {
  test('github.getPullRequest normalizes the PR payload', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getPullRequest', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER }, new AbortController().signal)) as {
      normalized: { number: number; title: string; headSha: string };
    };
    assert.equal(result.normalized.number, FIXTURE_PR_NUMBER);
    assert.equal(result.normalized.headSha, 'abc123');
  });

  test('github.getPullRequestFiles normalizes the diff files', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getPullRequestFiles', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER }, new AbortController().signal)) as {
      normalized: { path: string }[];
    };
    assert.equal(result.normalized[0]?.path, 'src/refund.ts');
  });

  test('github.getPullRequestChecks normalizes check runs', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getPullRequestChecks', repository: FIXTURE_REPO, ref: 'abc123' }, new AbortController().signal)) as {
      normalized: { name: string; conclusion: string | undefined }[];
    };
    assert.equal(result.normalized.length, 2);
    assert.equal(result.normalized[1]?.conclusion, 'failure');
  });

  test('github.listPullRequestComments merges review and issue comments', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.listPullRequestComments', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER }, new AbortController().signal)) as {
      normalized: { kind: string }[];
    };
    assert.equal(result.normalized.length, 2);
    assert.equal(result.normalized[0]?.kind, 'review');
    assert.equal(result.normalized[1]?.kind, 'issue');
  });

  test('github.createPullRequest posts to the pulls endpoint', async () => {
    const { handlers, rest } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.createPullRequest', repository: FIXTURE_REPO, baseBranch: 'main', headBranch: 'punakawan/fix', title: 'Fix it', body: 'Body text' },
      new AbortController().signal,
    )) as { normalized: { number: number; title: string } };
    assert.equal(result.normalized.title, 'Fix it');
    assert.equal(rest.createdPullRequests[0]?.head, 'punakawan/fix');
  });

  test('github.addLabels posts the label list', async () => {
    const { handlers, rest } = fakeHandlers();
    await handlers.execute!({ op: 'github.addLabels', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER, labels: ['needs-review'] }, new AbortController().signal);
    assert.deepEqual(rest.addedLabels[0]?.labels, ['needs-review']);
  });

  test('github.requestReviewers posts the reviewer list', async () => {
    const { handlers, rest } = fakeHandlers();
    await handlers.execute!({ op: 'github.requestReviewers', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER, reviewers: ['alice'] }, new AbortController().signal);
    assert.deepEqual(rest.requestedReviewers[0]?.reviewers, ['alice']);
  });

  test('github.replyToReviewComment posts a reply', async () => {
    const { handlers, rest } = fakeHandlers();
    await handlers.execute!({ op: 'github.replyToReviewComment', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER, commentId: '501', body: 'Fixed in a1b2c3' }, new AbortController().signal);
    assert.equal(rest.repliedComments[0]?.body, 'Fixed in a1b2c3');
  });

  test('github.resolveReviewThread issues the GraphQL mutation', async () => {
    const { handlers, rest } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.resolveReviewThread', threadId: 'thread-1' }, new AbortController().signal)) as { resolved: boolean };
    assert.equal(result.resolved, true);
    assert.equal(rest.resolvedThreadIds[0], 'thread-1');
  });

  test('rejects an unsupported op', async () => {
    const { handlers } = fakeHandlers();
    await assert.rejects(() => handlers.execute!({ op: 'github.doesNotExist' }, new AbortController().signal), /Unsupported op/);
  });
});

describe('createHandlers().initialize/capabilities/shutdown', () => {
  test('capabilities returns the parsed manifest', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.capabilities!(undefined, new AbortController().signal)) as { id: string };
    assert.equal(result.id, 'github');
  });

  test('initialize validates the manifest and returns ok', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.initialize!(manifest, new AbortController().signal)) as { ok: boolean; id: string };
    assert.equal(result.ok, true);
    assert.equal(result.id, 'github');
  });

  test('shutdown closes cleanly even if no operation was ever called', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.shutdown!(undefined, new AbortController().signal)) as { ok: boolean };
    assert.equal(result.ok, true);
  });
});
