export const FIXTURE_REPO = 'acme/widgets';
export const FIXTURE_PR_NUMBER = 42;

export const FIXTURE_PULL_REQUEST = {
  number: FIXTURE_PR_NUMBER,
  title: 'Fix refund rounding',
  body: 'Fixes the off-by-one-cent rounding bug.',
  state: 'open',
  draft: false,
  merged: false,
  mergeable: true,
  base: { ref: 'main' },
  head: { ref: 'punakawan/fix-refund-rounding', sha: 'abc123' },
  user: { login: 'petruk-bot' },
  html_url: 'https://github.com/acme/widgets/pull/42',
  created_at: '2026-07-20T00:00:00Z',
  updated_at: '2026-07-21T00:00:00Z',
  labels: [{ name: 'needs-review' }],
  requested_reviewers: [{ login: 'reviewer1' }],
};

/** The PR's head SHA changes to this value once "advanceHeadSha" is used, simulating a new push landing mid-workflow. */
export const FIXTURE_STALE_HEAD_SHA = 'abc123';
export const FIXTURE_FRESH_HEAD_SHA = 'def456';

export const FIXTURE_FILES = [
  { filename: 'src/refund.ts', status: 'modified', additions: 3, deletions: 1, changes: 4, patch: '@@ -1,1 +1,3 @@\n-old\n+new' },
];

/** 250 files - exercises Link-header pagination past a single 100-per-page response. */
export const FIXTURE_MANY_FILES = Array.from({ length: 250 }, (_, i) => ({
  filename: `src/file-${i}.ts`, status: 'modified', additions: 1, deletions: 0, changes: 1, patch: `@@ file ${i} @@`,
}));

export const FIXTURE_CHECK_RUNS = [
  { name: 'build', status: 'completed', conclusion: 'success', html_url: 'https://github.com/acme/widgets/runs/1' },
  { name: 'lint', status: 'completed', conclusion: 'failure', html_url: 'https://github.com/acme/widgets/runs/2' },
];

/** 180 check runs - exercises Link-header pagination past a single 100-per-page response. */
export const FIXTURE_MANY_CHECK_RUNS = Array.from({ length: 180 }, (_, i) => ({
  name: `check-${i}`, status: 'completed', conclusion: 'success', html_url: `https://github.com/acme/widgets/runs/${i}`,
}));

export const FIXTURE_COMBINED_STATUS = {
  state: 'success',
  statuses: [
    { context: 'ci/legacy-build', state: 'success', description: 'Build passed', target_url: 'https://ci.example.test/1' },
  ],
};

export const FIXTURE_REVIEW_COMMENTS = [
  { id: 501, user: { login: 'reviewer1' }, body: 'This rounds down, should round to nearest cent.', path: 'src/refund.ts', line: 12, created_at: '2026-07-20T01:00:00Z', updated_at: '2026-07-20T01:00:00Z' },
];

export const FIXTURE_ISSUE_COMMENTS = [
  { id: 601, user: { login: 'reviewer2' }, body: 'LGTM once the rounding is fixed.', created_at: '2026-07-20T02:00:00Z', updated_at: '2026-07-20T02:00:00Z' },
];

/** 70 review comments + 60 issue comments = 130 total - exercises Link-header pagination on both endpoints at once. */
export const FIXTURE_MANY_REVIEW_COMMENTS = Array.from({ length: 70 }, (_, i) => ({
  id: 10000 + i, user: { login: 'reviewer1' }, body: `Review comment ${i}`, path: 'src/refund.ts', line: 1, created_at: '2026-07-20T01:00:00Z', updated_at: '2026-07-20T01:00:00Z',
}));
export const FIXTURE_MANY_ISSUE_COMMENTS = Array.from({ length: 60 }, (_, i) => ({
  id: 20000 + i, user: { login: 'reviewer2' }, body: `Issue comment ${i}`, created_at: '2026-07-20T02:00:00Z', updated_at: '2026-07-20T02:00:00Z',
}));

export const FIXTURE_REPOSITORY = {
  private: true,
  default_branch: 'main',
  permissions: { admin: false, maintain: false, push: true, pull: true, triage: false },
};

/** A repository slug the fake REST server 404s for, simulating a private repo the configured credential cannot see. */
export const INACCESSIBLE_REPO = 'acme/no-access';

export const FIXTURE_REVIEW_THREADS = [
  {
    id: 'thread-unresolved-1',
    isResolved: false,
    comments: { nodes: [{ id: '501', body: 'This rounds down, should round to nearest cent.', path: 'src/refund.ts', line: 12, createdAt: '2026-07-20T01:00:00Z', author: { login: 'reviewer1' } }] },
  },
  {
    id: 'thread-resolved-1',
    isResolved: true,
    comments: { nodes: [{ id: '502', body: 'Nevermind, this is fine.', path: 'src/refund.ts', line: 20, createdAt: '2026-07-20T01:30:00Z', author: { login: 'reviewer1' } }] },
  },
];

/** 140 review threads, one of them unresolved past the first GraphQL page (100) - exercises cursor pagination. */
export const FIXTURE_MANY_REVIEW_THREADS = Array.from({ length: 140 }, (_, i) => ({
  id: `thread-many-${i}`,
  isResolved: i !== 120,
  comments: { nodes: [{ id: String(30000 + i), body: `Thread ${i}`, path: 'src/refund.ts', line: 1, createdAt: '2026-07-20T01:00:00Z', author: { login: 'reviewer1' } }] },
}));

export const FIXTURE_REVIEWS = [
  { id: 701, user: { login: 'reviewer1' }, state: 'APPROVED', body: 'Looks good.', commit_id: FIXTURE_STALE_HEAD_SHA, submitted_at: '2026-07-20T03:00:00Z' },
];

export const FIXTURE_REVIEW_THREAD_NODE = { id: 'thread-unresolved-1', isResolved: false };

export interface RecordedRestRequest {
  method: string;
  path: string;
  authorization: string | undefined;
  body: Record<string, unknown>;
}

export interface FakeGitHubRest {
  fetch: typeof fetch;
  requests: RecordedRestRequest[];
  createdPullRequests: Record<string, unknown>[];
  createdPullRequestReviews: Record<string, unknown>[];
  addedLabels: { pullRequestNumber: number; labels: string[] }[];
  requestedReviewers: { pullRequestNumber: number; reviewers: string[] }[];
  repliedComments: { commentId: string; body: string }[];
  resolvedThreadIds: string[];
  /** When true, github.getPullRequest reports FIXTURE_FRESH_HEAD_SHA instead of FIXTURE_STALE_HEAD_SHA - simulates a new push landing between hydration and a review submission. */
  headAdvanced: boolean;
}

function json(data: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

/** Slices a REST list response per GitHub's page/per_page convention and reports a Link header naming the next page, if any. */
function paginate<T>(all: T[], url: URL, defaultPerPage = 100): { page: T[]; linkHeader?: string } {
  const perPage = Number(url.searchParams.get('per_page') ?? String(defaultPerPage));
  const page = Number(url.searchParams.get('page') ?? '1');
  const start = (page - 1) * perPage;
  const slice = all.slice(start, start + perPage);
  if (start + perPage >= all.length) return { page: slice };
  const next = new URL(url.toString());
  next.searchParams.set('page', String(page + 1));
  next.searchParams.set('per_page', String(perPage));
  return { page: slice, linkHeader: `<${next.toString()}>; rel="next"` };
}

/** Slices a GraphQL connection per an integer-string cursor. */
function paginateNodes<T>(all: T[], after: string | undefined, first: number): { nodes: T[]; hasNextPage: boolean; endCursor?: string } {
  const start = after ? Number(after) : 0;
  const nodes = all.slice(start, start + first);
  const hasNextPage = start + first < all.length;
  return { nodes, hasNextPage, endCursor: hasNextPage ? String(start + first) : undefined };
}

/** In-memory fetch implementation that exercises real REST/GraphQL request mapping without network access. */
export function createFakeGitHubRest(): FakeGitHubRest {
  const requests: RecordedRestRequest[] = [];
  const createdPullRequests: FakeGitHubRest['createdPullRequests'] = [];
  const createdPullRequestReviews: FakeGitHubRest['createdPullRequestReviews'] = [];
  const addedLabels: FakeGitHubRest['addedLabels'] = [];
  const requestedReviewers: FakeGitHubRest['requestedReviewers'] = [];
  const repliedComments: FakeGitHubRest['repliedComments'] = [];
  const resolvedThreadIds: string[] = [];
  const state: FakeGitHubRest = {
    fetch: (() => { throw new Error('not yet assigned'); }) as unknown as typeof fetch,
    requests, createdPullRequests, createdPullRequestReviews, addedLabels, requestedReviewers, repliedComments, resolvedThreadIds,
    headAdvanced: false,
  };

  const fakeFetch = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    // Real fetch would observe init.signal itself; this fake replaces fetch
    // entirely, so it must check the signal itself to exercise
    // cancellation - a request already aborted before this call started
    // must never let a POST below reach its "recorded effect" array.
    if (init?.signal?.aborted) {
      throw new DOMException('The operation was aborted.', 'AbortError');
    }
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
    const method = (init?.method ?? 'GET').toUpperCase();
    const parsedBody = typeof init?.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    const headers = new Headers(init?.headers);
    requests.push({ method, path: url.pathname, authorization: headers.get('Authorization') ?? undefined, body: parsedBody });

    if (url.pathname === '/graphql' && method === 'POST') {
      const variables = (parsedBody.variables ?? {}) as Record<string, unknown>;
      const query = typeof parsedBody.query === 'string' ? parsedBody.query : '';
      if (query.includes('ReviewThreads')) {
        const after = typeof variables.after === 'string' ? variables.after : undefined;
        const many = variables.number === FIXTURE_PR_NUMBER + 2;
        const { nodes, hasNextPage, endCursor } = paginateNodes(many ? FIXTURE_MANY_REVIEW_THREADS : FIXTURE_REVIEW_THREADS, after, 100);
        return json({ data: { repository: { pullRequest: { reviewThreads: { pageInfo: { hasNextPage, endCursor: endCursor ?? null }, nodes } } } } });
      }
      if (query.includes('GetReviewThread')) {
        const threadId = typeof variables.threadId === 'string' ? variables.threadId : '';
        if (threadId === 'does-not-exist') return json({ data: { node: null } });
        return json({ data: { node: { id: threadId, isResolved: resolvedThreadIds.includes(threadId) } } });
      }
      const threadId = typeof variables.threadId === 'string' ? variables.threadId : '';
      resolvedThreadIds.push(threadId);
      return json({ data: { resolveReviewThread: { thread: { id: threadId, isResolved: true } } } });
    }

    const repoMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)$/);
    if (repoMatch && method === 'GET') {
      const repo = `${repoMatch[1]}/${repoMatch[2]}`;
      if (repo === INACCESSIBLE_REPO) return json({ message: 'Not Found' }, 404);
      return json(FIXTURE_REPOSITORY);
    }

    const prMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)$/);
    if (prMatch && method === 'GET') {
      return json({ ...FIXTURE_PULL_REQUEST, head: { ...FIXTURE_PULL_REQUEST.head, sha: state.headAdvanced ? FIXTURE_FRESH_HEAD_SHA : FIXTURE_STALE_HEAD_SHA } });
    }

    const pullsListMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls$/);
    if (pullsListMatch && method === 'GET') {
      const head = url.searchParams.get('head');
      const base = url.searchParams.get('base');
      const requestedState = url.searchParams.get('state');
      const matches = head === `${pullsListMatch[1]}:${FIXTURE_PULL_REQUEST.head.ref}` && base === FIXTURE_PULL_REQUEST.base.ref && requestedState === 'open';
      return json(matches ? [FIXTURE_PULL_REQUEST] : []);
    }

    const filesMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/files$/);
    if (filesMatch && method === 'GET') {
      const many = Number(filesMatch[3]) === FIXTURE_PR_NUMBER + 1;
      const { page, linkHeader } = paginate(many ? FIXTURE_MANY_FILES : FIXTURE_FILES, url);
      return json(page, 200, linkHeader ? { Link: linkHeader } : {});
    }

    const checksMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/commits\/([^/]+)\/check-runs$/);
    if (checksMatch && method === 'GET') {
      const many = checksMatch[3] === 'many-checks-sha';
      const { page, linkHeader } = paginate(many ? FIXTURE_MANY_CHECK_RUNS : FIXTURE_CHECK_RUNS, url);
      return json({ check_runs: page }, 200, linkHeader ? { Link: linkHeader } : {});
    }

    const combinedStatusMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/commits\/([^/]+)\/status$/);
    if (combinedStatusMatch && method === 'GET') {
      return json(FIXTURE_COMBINED_STATUS);
    }

    const reviewCommentsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/comments$/);
    if (reviewCommentsMatch && method === 'GET') {
      const many = Number(reviewCommentsMatch[3]) === FIXTURE_PR_NUMBER + 1;
      const { page, linkHeader } = paginate(many ? FIXTURE_MANY_REVIEW_COMMENTS : FIXTURE_REVIEW_COMMENTS, url);
      return json(page, 200, linkHeader ? { Link: linkHeader } : {});
    }

    const issueCommentsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/issues\/(\d+)\/comments$/);
    if (issueCommentsMatch && method === 'GET') {
      const many = Number(issueCommentsMatch[3]) === FIXTURE_PR_NUMBER + 1;
      const { page, linkHeader } = paginate(many ? FIXTURE_MANY_ISSUE_COMMENTS : FIXTURE_ISSUE_COMMENTS, url);
      return json(page, 200, linkHeader ? { Link: linkHeader } : {});
    }

    const reviewsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/reviews$/);
    if (reviewsMatch && method === 'GET') {
      const { page, linkHeader } = paginate([...FIXTURE_REVIEWS, ...createdPullRequestReviews.map((r, i) => ({ id: 800 + i, user: { login: 'punakawan-bot' }, state: r.event, body: r.body, commit_id: r.commit_id, submitted_at: '2026-07-20T04:00:00Z' }))], url);
      return json(page, 200, linkHeader ? { Link: linkHeader } : {});
    }

    const createPrMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls$/);
    if (createPrMatch && method === 'POST') {
      createdPullRequests.push(parsedBody);
      return json({ ...FIXTURE_PULL_REQUEST, title: parsedBody.title, body: parsedBody.body, number: 43 }, 201);
    }

    const createReviewMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/reviews$/);
    if (createReviewMatch && method === 'POST') {
      createdPullRequestReviews.push(parsedBody);
      return json({ id: 701 }, 201);
    }

    const labelsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/issues\/(\d+)\/labels$/);
    if (labelsMatch && method === 'POST') {
      const labels = Array.isArray(parsedBody.labels) ? (parsedBody.labels as string[]) : [];
      addedLabels.push({ pullRequestNumber: Number(labelsMatch[3]), labels });
      return json(labels.map((name) => ({ name })), 200);
    }

    const reviewersMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/requested_reviewers$/);
    if (reviewersMatch && method === 'POST') {
      const reviewers = Array.isArray(parsedBody.reviewers) ? (parsedBody.reviewers as string[]) : [];
      requestedReviewers.push({ pullRequestNumber: Number(reviewersMatch[3]), reviewers });
      return json({ ...FIXTURE_PULL_REQUEST }, 201);
    }

    const replyMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/comments\/([^/]+)\/replies$/);
    if (replyMatch && method === 'POST') {
      const commentId = replyMatch[4] ?? '';
      const body = typeof parsedBody.body === 'string' ? parsedBody.body : '';
      repliedComments.push({ commentId, body });
      return json({ ...FIXTURE_REVIEW_COMMENTS[0], id: 502, body }, 201);
    }

    return json({ message: `Unhandled fake REST route: ${method} ${url.pathname}` }, 404);
  };

  state.fetch = fakeFetch as typeof fetch;
  return state;
}
