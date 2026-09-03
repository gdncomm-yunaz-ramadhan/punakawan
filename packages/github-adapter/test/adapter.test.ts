import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { createHandlers } from '../src/adapter.js';
import { manifest } from '../src/manifest.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import {
  createFakeGitHubRest,
  FIXTURE_FRESH_HEAD_SHA,
  FIXTURE_MANY_CHECK_RUNS,
  FIXTURE_MANY_FILES,
  FIXTURE_MANY_ISSUE_COMMENTS,
  FIXTURE_MANY_REVIEW_COMMENTS,
  FIXTURE_MANY_REVIEW_THREADS,
  FIXTURE_PR_NUMBER,
  FIXTURE_PULL_REQUEST,
  FIXTURE_REPO,
  FIXTURE_STALE_HEAD_SHA,
  INACCESSIBLE_REPO,
  type FakeGitHubRest,
} from './fakeGitHubRest.js';

function fakeHandlers(): { handlers: ReturnType<typeof createHandlers>; rest: FakeGitHubRest } {
  const rest = createFakeGitHubRest();
  const handlers = createHandlers({ fetchImpl: rest.fetch, env: { GITHUB_TOKEN: 'fake-token' } });
  return { handlers, rest };
}

describe('manifest', () => {
  test('validates against the shared AdapterManifest schema', () => {
    assert.doesNotThrow(() => AdapterManifestSchema.parse(manifest));
  });

  test('declares every write operation as side-effecting with no approval member', () => {
    const writeOps = ['github.createPullRequest', 'github.addLabels', 'github.requestReviewers', 'github.createPullRequestReview', 'github.replyToReviewComment', 'github.resolveReviewThread'];
    for (const op of writeOps) {
      assert.equal(manifest.operations[op]?.side_effect, true, `${op} should be side_effect: true`);
      assert.equal('approval' in (manifest.operations[op] ?? {}), false, `${op} should not declare an approval member`);
    }
  });

  test('declares every read operation as side-effect free', () => {
    const readOps = [
      'github.getRepository', 'github.getPullRequest', 'github.getPullRequestFiles', 'github.getPullRequestChecks',
      'github.getCommitStatus', 'github.listPullRequestComments', 'github.listUnresolvedReviewThreads',
      'github.listPullRequestReviews', 'github.findPullRequest', 'github.getReviewThread',
      'github.searchRepositories',
    ];
    for (const op of readOps) {
      assert.equal(manifest.operations[op]?.side_effect, false, `${op} should be side_effect: false`);
    }
  });

  test('every operation declares a non-empty description and an object input_schema', () => {
    for (const [op, metadata] of Object.entries(manifest.operations)) {
      assert.ok(typeof metadata.description === 'string' && metadata.description.length > 0, `${op} should declare a description`);
      assert.equal(metadata.input_schema.type, 'object', `${op} should declare an object input_schema`);
    }
  });
});

describe('github.searchRepositories', () => {
  test('scopes the query to the owners it was given and reports what it did not show', async () => {
    const { handlers, rest } = fakeHandlers();

    const result = (await handlers.execute!(
      { op: 'github.searchRepositories', name: 'widgets', owners: ['acme'] },
      new AbortController().signal,
    )) as { normalized: { repositories: Array<{ repository: string; defaultBranch: string | null }>; total: number; complete: boolean } };

    assert.deepEqual(result.normalized.repositories.map((r) => r.repository), ['acme/widgets']);
    assert.equal(result.normalized.repositories[0]?.defaultBranch, 'main');
    const search = rest.requests.find((r) => r.path === '/search/repositories');
    assert.ok(search, 'expected the search endpoint to be called');
  });

  test('returns every owner a name matches, which is the ambiguity a caller has to resolve', async () => {
    const { handlers } = fakeHandlers();

    const result = (await handlers.execute!(
      { op: 'github.searchRepositories', name: 'widgets' },
      new AbortController().signal,
    )) as { normalized: { repositories: Array<{ repository: string }>; total: number; complete: boolean } };

    assert.deepEqual(result.normalized.repositories.map((r) => r.repository), ['acme/widgets', 'personal-account/widgets']);
    assert.equal(result.normalized.total, 2);
    assert.equal(result.normalized.complete, true);
  });

  test('refuses a blank name rather than searching for everything', async () => {
    const { handlers } = fakeHandlers();
    await assert.rejects(
      handlers.execute!({ op: 'github.searchRepositories', name: '  ' }, new AbortController().signal),
      /non-empty "name"/,
    );
  });
});

describe('createHandlers().execute', () => {
  test('github.getRepository normalizes access for a visible repository', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getRepository', repository: FIXTURE_REPO }, new AbortController().signal)) as {
      normalized: { accessible: boolean; private: boolean | null; permissions: { push: boolean } | null; defaultBranch: string | null };
    };
    assert.equal(result.normalized.accessible, true);
    assert.equal(result.normalized.private, true);
    assert.equal(result.normalized.permissions?.push, true);
    assert.equal(result.normalized.defaultBranch, 'main');
  });

  test('github.getRepository normalizes a 404 to an inaccessible result instead of throwing', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getRepository', repository: INACCESSIBLE_REPO }, new AbortController().signal)) as {
      normalized: { accessible: boolean; private: boolean | null; permissions: unknown; defaultBranch: string | null };
    };
    assert.equal(result.normalized.accessible, false);
    assert.equal(result.normalized.private, null);
    assert.equal(result.normalized.permissions, null);
    assert.equal(result.normalized.defaultBranch, null);
  });

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

  test('github.getPullRequestFiles returns all 250 files across pages, with complete=true', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.getPullRequestFiles', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER + 1 },
      new AbortController().signal,
    )) as { normalized: { path: string }[]; page: { returned: number; complete: boolean; pages: number } };
    assert.equal(result.normalized.length, FIXTURE_MANY_FILES.length);
    assert.equal(result.page.complete, true);
    assert.ok(result.page.pages >= 3, `expected at least 3 pages, got ${result.page.pages}`);
  });

  test('github.getPullRequestChecks normalizes check runs', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getPullRequestChecks', repository: FIXTURE_REPO, ref: 'abc123' }, new AbortController().signal)) as {
      normalized: { name: string; conclusion: string | undefined }[];
    };
    assert.equal(result.normalized.length, 2);
    assert.equal(result.normalized[1]?.conclusion, 'failure');
  });

  test('github.getPullRequestChecks returns all 180 check runs across pages, with complete=true', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.getPullRequestChecks', repository: FIXTURE_REPO, ref: 'many-checks-sha' },
      new AbortController().signal,
    )) as { normalized: { name: string }[]; page: { returned: number; complete: boolean; pages: number } };
    assert.equal(result.normalized.length, FIXTURE_MANY_CHECK_RUNS.length);
    assert.equal(result.page.complete, true);
    assert.ok(result.page.pages >= 2, `expected at least 2 pages, got ${result.page.pages}`);
  });

  test('github.getCommitStatus normalizes the combined legacy status', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getCommitStatus', repository: FIXTURE_REPO, ref: 'abc123' }, new AbortController().signal)) as {
      normalized: { state: string; statuses: { context: string; state: string }[] };
    };
    assert.equal(result.normalized.state, 'success');
    assert.equal(result.normalized.statuses.length, 1);
    assert.equal(result.normalized.statuses[0]?.context, 'ci/legacy-build');
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

  test('github.listPullRequestComments returns all 130 comments across pages, with complete=true', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.listPullRequestComments', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER + 1 },
      new AbortController().signal,
    )) as { normalized: { kind: string }[]; page: { returned: number; complete: boolean } };
    assert.equal(result.normalized.length, FIXTURE_MANY_REVIEW_COMMENTS.length + FIXTURE_MANY_ISSUE_COMMENTS.length);
    assert.equal(result.page.complete, true);
  });

  test('github.listUnresolvedReviewThreads filters out resolved threads', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.listUnresolvedReviewThreads', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER }, new AbortController().signal)) as {
      normalized: { id: string; comments: { body: string }[] }[];
    };
    assert.equal(result.normalized.length, 1);
    assert.equal(result.normalized[0]?.id, 'thread-unresolved-1');
    assert.equal(result.normalized[0]?.comments[0]?.body, 'This rounds down, should round to nearest cent.');
  });

  test('github.listUnresolvedReviewThreads walks all 140 threads across pages and reports complete=true', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.listUnresolvedReviewThreads', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER + 2 },
      new AbortController().signal,
    )) as { normalized: { id: string }[]; page: { complete: boolean; pages: number } };
    const expectedUnresolved = FIXTURE_MANY_REVIEW_THREADS.filter((t) => !t.isResolved).length;
    assert.equal(result.normalized.length, expectedUnresolved);
    assert.equal(result.page.complete, true);
    assert.ok(result.page.pages >= 2, `expected at least 2 GraphQL pages, got ${result.page.pages}`);
  });

  test('github.listPullRequestReviews lists submitted reviews', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!(
      { op: 'github.listPullRequestReviews', repository: FIXTURE_REPO, pullRequestNumber: FIXTURE_PR_NUMBER },
      new AbortController().signal,
    )) as { normalized: { commitId: string | undefined; state: string }[] };
    assert.ok(result.normalized.length >= 1);
    assert.equal(result.normalized[0]?.commitId, FIXTURE_STALE_HEAD_SHA);
  });

  test('github.findPullRequest finds an exact head/base match among open pull requests', async () => {
    const { handlers } = fakeHandlers();
    const found = (await handlers.execute!(
      { op: 'github.findPullRequest', repository: FIXTURE_REPO, headBranch: FIXTURE_PULL_REQUEST.head.ref, baseBranch: FIXTURE_PULL_REQUEST.base.ref },
      new AbortController().signal,
    )) as { normalized: { number: number } | undefined };
    assert.equal(found.normalized?.number, FIXTURE_PR_NUMBER);

    const notFound = (await handlers.execute!(
      { op: 'github.findPullRequest', repository: FIXTURE_REPO, headBranch: 'no-such-branch', baseBranch: 'main' },
      new AbortController().signal,
    )) as { normalized: { number: number } | undefined };
    assert.equal(notFound.normalized, undefined);
  });

  test('github.getReviewThread reports current resolution state by node id', async () => {
    const { handlers } = fakeHandlers();
    const result = (await handlers.execute!({ op: 'github.getReviewThread', threadId: 'thread-unresolved-1' }, new AbortController().signal)) as {
      normalized: { id: string; isResolved: boolean } | undefined;
    };
    assert.equal(result.normalized?.id, 'thread-unresolved-1');
    assert.equal(result.normalized?.isResolved, false);
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

  test('an already-aborted signal rejects github.createPullRequest before the fake remote effect completes', async () => {
    const { handlers, rest } = fakeHandlers();
    const controller = new AbortController();
    controller.abort();

    await assert.rejects(
      () => handlers.execute!(
        { op: 'github.createPullRequest', repository: FIXTURE_REPO, baseBranch: 'main', headBranch: 'punakawan/fix', title: 'Fix it', body: 'Body text' },
        controller.signal,
      ),
      (err: unknown) => err instanceof Error && err.name === 'AbortError',
    );
    assert.equal(rest.createdPullRequests.length, 0);
  });

  test('github.createPullRequestReview posts its verdict and inline comments', async () => {
    const { handlers, rest } = fakeHandlers();
    const result = (await handlers.execute!(
      {
        op: 'github.createPullRequestReview',
        repository: FIXTURE_REPO,
        pullRequestNumber: FIXTURE_PR_NUMBER,
        body: 'Please fix the rounding behavior.',
        event: 'REQUEST_CHANGES',
        commitId: 'abc123',
        comments: [{ path: 'src/refund.ts', line: 12, side: 'RIGHT', body: 'This rounds down.' }],
      },
      new AbortController().signal,
    )) as { ok: boolean; reviewId: string };
    assert.equal(result.ok, true);
    assert.equal(result.reviewId, '701');
    assert.deepEqual(rest.createdPullRequestReviews[0], {
      body: 'Please fix the rounding behavior.',
      event: 'REQUEST_CHANGES',
      commit_id: 'abc123',
      comments: [{ path: 'src/refund.ts', line: 12, side: 'RIGHT', body: 'This rounds down.' }],
    });
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
