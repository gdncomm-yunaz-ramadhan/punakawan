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
};

export const FIXTURE_FILES = [
  { filename: 'src/refund.ts', status: 'modified', additions: 3, deletions: 1, changes: 4, patch: '@@ -1,1 +1,3 @@\n-old\n+new' },
];

export const FIXTURE_CHECK_RUNS = [
  { name: 'build', status: 'completed', conclusion: 'success', html_url: 'https://github.com/acme/widgets/runs/1' },
  { name: 'lint', status: 'completed', conclusion: 'failure', html_url: 'https://github.com/acme/widgets/runs/2' },
];

export const FIXTURE_REVIEW_COMMENTS = [
  { id: 501, user: { login: 'reviewer1' }, body: 'This rounds down, should round to nearest cent.', path: 'src/refund.ts', line: 12, created_at: '2026-07-20T01:00:00Z', updated_at: '2026-07-20T01:00:00Z' },
];

export const FIXTURE_ISSUE_COMMENTS = [
  { id: 601, user: { login: 'reviewer2' }, body: 'LGTM once the rounding is fixed.', created_at: '2026-07-20T02:00:00Z', updated_at: '2026-07-20T02:00:00Z' },
];

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
  addedLabels: { pullRequestNumber: number; labels: string[] }[];
  requestedReviewers: { pullRequestNumber: number; reviewers: string[] }[];
  repliedComments: { commentId: string; body: string }[];
  resolvedThreadIds: string[];
}

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
}

/** In-memory fetch implementation that exercises real REST/GraphQL request mapping without network access. */
export function createFakeGitHubRest(): FakeGitHubRest {
  const requests: RecordedRestRequest[] = [];
  const createdPullRequests: FakeGitHubRest['createdPullRequests'] = [];
  const addedLabels: FakeGitHubRest['addedLabels'] = [];
  const requestedReviewers: FakeGitHubRest['requestedReviewers'] = [];
  const repliedComments: FakeGitHubRest['repliedComments'] = [];
  const resolvedThreadIds: string[] = [];

  const fakeFetch = async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url);
    const method = (init?.method ?? 'GET').toUpperCase();
    const parsedBody = typeof init?.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : {};
    const headers = new Headers(init?.headers);
    requests.push({ method, path: url.pathname, authorization: headers.get('Authorization') ?? undefined, body: parsedBody });

    if (url.pathname === '/graphql' && method === 'POST') {
      const variables = (parsedBody.variables ?? {}) as Record<string, unknown>;
      const threadId = typeof variables.threadId === 'string' ? variables.threadId : '';
      resolvedThreadIds.push(threadId);
      return json({ data: { resolveReviewThread: { thread: { id: threadId, isResolved: true } } } });
    }

    const prMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)$/);
    if (prMatch && method === 'GET') return json(FIXTURE_PULL_REQUEST);

    const filesMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/files$/);
    if (filesMatch && method === 'GET') return json(FIXTURE_FILES);

    const checksMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/commits\/([^/]+)\/check-runs$/);
    if (checksMatch && method === 'GET') return json({ check_runs: FIXTURE_CHECK_RUNS });

    const reviewCommentsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls\/(\d+)\/comments$/);
    if (reviewCommentsMatch && method === 'GET') return json(FIXTURE_REVIEW_COMMENTS);

    const issueCommentsMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/issues\/(\d+)\/comments$/);
    if (issueCommentsMatch && method === 'GET') return json(FIXTURE_ISSUE_COMMENTS);

    const createPrMatch = url.pathname.match(/^\/repos\/([^/]+)\/([^/]+)\/pulls$/);
    if (createPrMatch && method === 'POST') {
      createdPullRequests.push(parsedBody);
      return json({ ...FIXTURE_PULL_REQUEST, title: parsedBody.title, body: parsedBody.body, number: 43 }, 201);
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

  return {
    fetch: fakeFetch as typeof fetch,
    requests,
    createdPullRequests,
    addedLabels,
    requestedReviewers,
    repliedComments,
    resolvedThreadIds,
  };
}
