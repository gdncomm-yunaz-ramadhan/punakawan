import { GitHubRestError, type GitHubRestClient } from './restClient.js';
import {
  INACCESSIBLE_REPOSITORY,
  normalizeCheckRun,
  normalizeCombinedStatus,
  normalizeGraphQLReviewComment,
  normalizeIssueComment,
  normalizePullRequest,
  normalizePullRequestFile,
  normalizeRepositoryAccess,
  normalizeRepositoryMatch,
  normalizeReview,
  normalizeReviewComment,
  type NormalizedReviewThread,
} from './normalize.js';
import { collectCursorPages, collectLinkPages, DEFAULT_HARD_LIMIT_ITEMS } from './pagination.js';

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

export interface RepoRef {
  /** "owner/repo", e.g. "acme/widgets". */
  repository: string;
}

function splitRepo(repository: string): { owner: string; repo: string } {
  const [owner, repo] = repository.split('/');
  if (!owner || !repo) {
    throw new Error(`repository must be in "owner/repo" form, got ${JSON.stringify(repository)}`);
  }
  return { owner, repo };
}

export interface SearchRepositoriesParams {
  /** The bare repository name to look for, e.g. "widgets". */
  name: string;
  /** Owners to scope the search to. Omitted searches everything the credential can see. */
  owners?: string[];
  limit?: number;
}

const REPOSITORY_SEARCH_DEFAULT_LIMIT = 10;
const REPOSITORY_SEARCH_MAX_LIMIT = 50;

/**
 * Finds repositories by name, optionally scoped to owners.
 *
 * This exists so a repository named without an owner can be resolved to
 * exactly one, instead of the caller guessing an owner and getting a 404
 * that says nothing about where the repository actually is. GitHub's
 * "user:" qualifier matches repositories owned by a personal account or
 * an organisation alike, which is required here: the owner a credential
 * speaks for is routinely a personal account.
 *
 * Results are deliberately not paged past the limit. A name matching two
 * hundred repositories is an ambiguity to put to a human, not a list to
 * walk - total says how much was matched so the caller can say so.
 */
export async function searchRepositories(client: GitHubRestClient, params: SearchRepositoriesParams, signal?: AbortSignal) {
  const name = params.name.trim();
  if (!name) {
    throw new Error('searchRepositories requires a non-empty "name"');
  }
  const limit = Math.min(REPOSITORY_SEARCH_MAX_LIMIT, Math.max(1, params.limit ?? REPOSITORY_SEARCH_DEFAULT_LIMIT));
  const owners = (params.owners ?? []).map((owner) => owner.trim()).filter(Boolean);
  const q = [`${name} in:name`, ...owners.map((owner) => `user:${owner}`)].join(' ');

  const raw = await client.request<Record<string, unknown>>('/search/repositories', {
    query: { q, per_page: limit },
    signal,
  });
  const items = Array.isArray(raw.data.items) ? raw.data.items : [];
  const total = typeof raw.data.total_count === 'number' ? raw.data.total_count : items.length;
  const repositories = items.map((item) => normalizeRepositoryMatch(asRecord(item)));
  return { normalized: { repositories, total, complete: repositories.length >= total } };
}

export interface GetPullRequestParams extends RepoRef {
  pullRequestNumber: number;
}

export async function getPullRequest(client: GitHubRestClient, params: GetPullRequestParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}`, { signal });
  return { normalized: normalizePullRequest(raw.data) };
}

export interface GetPullRequestFilesParams extends RepoRef {
  pullRequestNumber: number;
}

/**
 * Fetches every file changed by a pull request, walking GitHub's Link-header
 * pagination (collectLinkPages) instead of returning only the first page -
 * a hydration that silently truncated a large PR's file list at 100 files
 * would leave a reviewer or reconciler working from incomplete diff
 * context.
 */
export async function getPullRequestFiles(client: GitHubRestClient, params: GetPullRequestFilesParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const firstURL = `/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/files?per_page=100`;
  const result = await collectLinkPages<Record<string, unknown>>(
    firstURL,
    async (url) => {
      const raw = await client.request<unknown[]>(url, { signal });
      return { items: asArray(raw.data).map(asRecord), linkHeader: raw.linkHeader };
    },
    DEFAULT_HARD_LIMIT_ITEMS,
  );
  return {
    normalized: result.items.map((entry) => normalizePullRequestFile(entry)),
    page: { returned: result.items.length, complete: result.complete, pages: result.pages, ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}) },
  };
}

export type GetRepositoryParams = RepoRef;

/**
 * Checks whether the configured credential can even see repository, and if
 * so what access it has - used by the delivery preflight's
 * private-repository-identity and pr-permissions checks. A 404 is
 * normalized to an inaccessible result rather than thrown: a private repo
 * the caller has no access to also 404s (GitHub never distinguishes "does
 * not exist" from "exists but you can't see it" for a repo lookup), so
 * this is diagnostic information for the caller to report, not a failed
 * call.
 */
export async function getRepository(client: GitHubRestClient, params: GetRepositoryParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  try {
    const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}`, { signal });
    return { normalized: normalizeRepositoryAccess(raw.data) };
  } catch (error) {
    if (error instanceof GitHubRestError && error.status === 404) {
      return { normalized: INACCESSIBLE_REPOSITORY };
    }
    throw error;
  }
}

export interface GetPullRequestChecksParams extends RepoRef {
  /** The commit SHA to fetch check runs for - typically the PR's current head SHA. */
  ref: string;
}

/**
 * Fetches every check run reported for a commit, walking Link-header
 * pagination - a large PR can accumulate well over the 100-per-page
 * default (many status checks, matrix builds), and silently reporting
 * only the first page would misrepresent whether CI actually passed.
 */
export async function getPullRequestChecks(client: GitHubRestClient, params: GetPullRequestChecksParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const firstURL = `/repos/${owner}/${repo}/commits/${params.ref}/check-runs?per_page=100`;
  const result = await collectLinkPages<Record<string, unknown>>(
    firstURL,
    async (url) => {
      const raw = await client.request<Record<string, unknown>>(url, { signal });
      return { items: asArray(raw.data.check_runs).map(asRecord), linkHeader: raw.linkHeader };
    },
    DEFAULT_HARD_LIMIT_ITEMS,
  );
  return {
    normalized: result.items.map((entry) => normalizeCheckRun(entry)),
    page: { returned: result.items.length, complete: result.complete, pages: result.pages, ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}) },
  };
}

export interface GetCommitStatusParams extends RepoRef {
  ref: string;
}

/**
 * Fetches a commit's combined legacy Status API state
 * (docs.github.com/en/rest/commits/statuses) - separate from, and not
 * returned by, getPullRequestChecks' newer Check Runs API. A pull
 * request's true CI state can depend on either or both depending on which
 * integration posted it, so a complete hydration needs both.
 */
export async function getCommitStatus(client: GitHubRestClient, params: GetCommitStatusParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/commits/${params.ref}/status`, { signal });
  return { normalized: normalizeCombinedStatus(raw.data) };
}

export interface ListPullRequestCommentsParams extends RepoRef {
  pullRequestNumber: number;
}

/**
 * Merges every diff-line review comment and every general issue-level
 * comment into one normalized, chronologically-tagged list, walking each
 * endpoint's own Link-header pagination in full.
 */
export async function listPullRequestComments(client: GitHubRestClient, params: ListPullRequestCommentsParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const paginate = (firstURL: string) =>
    collectLinkPages<Record<string, unknown>>(
      firstURL,
      async (url) => {
        const raw = await client.request<unknown[]>(url, { signal });
        return { items: asArray(raw.data).map(asRecord), linkHeader: raw.linkHeader };
      },
      DEFAULT_HARD_LIMIT_ITEMS,
    );
  const [review, issue] = await Promise.all([
    paginate(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/comments?per_page=100`),
    paginate(`/repos/${owner}/${repo}/issues/${params.pullRequestNumber}/comments?per_page=100`),
  ]);
  const reviewComments = review.items.map((entry) => normalizeReviewComment(entry));
  const issueComments = issue.items.map((entry) => normalizeIssueComment(entry));
  const complete = review.complete && issue.complete;
  return {
    normalized: [...reviewComments, ...issueComments],
    page: {
      returned: reviewComments.length + issueComments.length,
      complete,
      pages: review.pages + issue.pages,
      ...(complete ? {} : { truncated_reason: review.truncated_reason ?? issue.truncated_reason }),
    },
  };
}

export interface CreatePullRequestParams extends RepoRef {
  baseBranch: string;
  headBranch: string;
  title: string;
  body: string;
  draft?: boolean;
}

export async function createPullRequest(client: GitHubRestClient, params: CreatePullRequestParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/pulls`, {
    method: 'POST',
    body: { title: params.title, body: params.body, base: params.baseBranch, head: params.headBranch, draft: params.draft ?? false },
    signal,
  });
  return { normalized: normalizePullRequest(raw.data) };
}

export interface FindPullRequestParams extends RepoRef {
  headBranch: string;
  baseBranch: string;
}

/**
 * Searches both open and closed pull requests for an exact head/base match
 * - the read github.create-pr reconciliation needs to positively determine
 * whether an ambiguous createPullRequest attempt already opened a pull
 * request, without creating a second one. GitHub's list endpoint scopes
 * head to "owner:branch" and only accepts one state filter per call, so
 * open and closed are queried separately.
 */
export async function findPullRequest(client: GitHubRestClient, params: FindPullRequestParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const search = async (state: 'open' | 'closed') => {
    const raw = await client.request<unknown[]>(`/repos/${owner}/${repo}/pulls`, {
      query: { head: `${owner}:${params.headBranch}`, base: params.baseBranch, state, per_page: 10 },
      signal,
    });
    return asArray(raw.data).map(asRecord);
  };
  const [open, closed] = await Promise.all([search('open'), search('closed')]);
  const match = open[0] ?? closed[0];
  return { normalized: match ? normalizePullRequest(match) : undefined };
}

export interface AddLabelsParams extends RepoRef {
  pullRequestNumber: number;
  labels: string[];
}

export async function addLabels(client: GitHubRestClient, params: AddLabelsParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/issues/${params.pullRequestNumber}/labels`, {
    method: 'POST',
    body: { labels: params.labels },
    signal,
  });
  return { ok: true, labels: asArray(raw.data).map((entry) => asRecord(entry).name).filter((name): name is string => typeof name === 'string') };
}

export interface RequestReviewersParams extends RepoRef {
  pullRequestNumber: number;
  reviewers: string[];
}

export async function requestReviewers(client: GitHubRestClient, params: RequestReviewersParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  await client.request(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/requested_reviewers`, {
    method: 'POST',
    body: { reviewers: params.reviewers },
    signal,
  });
  return { ok: true, reviewers: params.reviewers };
}

export type PullRequestReviewEvent = 'APPROVE' | 'REQUEST_CHANGES' | 'COMMENT';

export interface PullRequestReviewComment {
  path: string;
  line: number;
  side: 'LEFT' | 'RIGHT';
  body: string;
  startLine?: number;
  startSide?: 'LEFT' | 'RIGHT';
}

export interface CreatePullRequestReviewParams extends RepoRef {
  pullRequestNumber: number;
  body: string;
  event: PullRequestReviewEvent;
  commitId?: string;
  comments?: PullRequestReviewComment[];
}

export async function createPullRequestReview(client: GitHubRestClient, params: CreatePullRequestReviewParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/reviews`, {
    method: 'POST',
    body: {
      body: params.body,
      event: params.event,
      ...(params.commitId ? { commit_id: params.commitId } : {}),
      ...(params.comments?.length ? {
        comments: params.comments.map((comment) => ({
          path: comment.path,
          line: comment.line,
          side: comment.side,
          body: comment.body,
          ...(comment.startLine ? { start_line: comment.startLine } : {}),
          ...(comment.startSide ? { start_side: comment.startSide } : {}),
        })),
      } : {}),
    },
    signal,
  });
  return { ok: true, reviewId: typeof raw.data.id === 'number' || typeof raw.data.id === 'string' ? String(raw.data.id) : undefined };
}

export interface ListPullRequestReviewsParams extends RepoRef {
  pullRequestNumber: number;
}

/**
 * Lists every review submitted on a pull request, walking Link-header
 * pagination - the read github.review reconciliation needs to positively
 * determine whether an ambiguous createPullRequestReview attempt already
 * landed, by matching the intent's own marker and target commit SHA
 * against a review already on the PR instead of submitting a second one.
 */
export async function listPullRequestReviews(client: GitHubRestClient, params: ListPullRequestReviewsParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const firstURL = `/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/reviews?per_page=100`;
  const result = await collectLinkPages<Record<string, unknown>>(
    firstURL,
    async (url) => {
      const raw = await client.request<unknown[]>(url, { signal });
      return { items: asArray(raw.data).map(asRecord), linkHeader: raw.linkHeader };
    },
    DEFAULT_HARD_LIMIT_ITEMS,
  );
  return {
    normalized: result.items.map((entry) => normalizeReview(entry)),
    page: { returned: result.items.length, complete: result.complete, pages: result.pages, ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}) },
  };
}

export interface ReplyToReviewCommentParams extends RepoRef {
  pullRequestNumber: number;
  commentId: string;
  body: string;
}

export async function replyToReviewComment(client: GitHubRestClient, params: ReplyToReviewCommentParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(
    `/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/comments/${params.commentId}/replies`,
    { method: 'POST', body: { body: params.body }, signal },
  );
  return { normalized: normalizeReviewComment(raw.data) };
}

const REVIEW_THREADS_QUERY = `
  query ReviewThreads($owner: String!, $name: String!, $number: Int!, $after: String) {
    repository(owner: $owner, name: $name) {
      pullRequest(number: $number) {
        reviewThreads(first: 100, after: $after) {
          pageInfo { hasNextPage endCursor }
          nodes {
            id
            isResolved
            comments(first: 50) {
              nodes { id body path line createdAt author { login } }
            }
          }
        }
      }
    }
  }
`;

export interface ListUnresolvedReviewThreadsParams extends RepoRef {
  pullRequestNumber: number;
}

interface GraphQLReviewThreadNode {
  id: string;
  isResolved: boolean;
  comments: { nodes: Record<string, unknown>[] };
}

interface GraphQLReviewThreadsResponse {
  repository: {
    pullRequest: {
      reviewThreads: {
        pageInfo: { hasNextPage: boolean; endCursor: string | null };
        nodes: GraphQLReviewThreadNode[];
      };
    };
  };
}

/**
 * Fetches every one of a PR's review threads via GraphQL cursor pagination
 * (collectCursorPages) - REST's pulls comments endpoint has no per-comment
 * resolution state at all, so filtering to "unresolved" is only possible
 * through GraphQL's reviewThreads.isResolved field
 * (docs.github.com/en/graphql/reference/objects#pullrequestreviewthread).
 * A large PR can accumulate well over one page (100) of threads; walking
 * every page is what keeps "unresolved" complete rather than only
 * reflecting the first page's threads.
 */
export async function listUnresolvedReviewThreads(client: GitHubRestClient, params: ListUnresolvedReviewThreadsParams, signal?: AbortSignal) {
  const { owner, repo } = splitRepo(params.repository);
  const result = await collectCursorPages<GraphQLReviewThreadNode>(async (after) => {
    const data = await client.graphql<GraphQLReviewThreadsResponse>(REVIEW_THREADS_QUERY, {
      owner, name: repo, number: params.pullRequestNumber, after,
    }, signal);
    const connection = data.repository.pullRequest.reviewThreads;
    return { nodes: connection.nodes, endCursor: connection.pageInfo.endCursor ?? undefined, hasNextPage: connection.pageInfo.hasNextPage };
  }, DEFAULT_HARD_LIMIT_ITEMS);

  const threads = result.items
    .filter((thread) => !thread.isResolved)
    .map((thread): NormalizedReviewThread => ({
      id: thread.id,
      comments: asArray(thread.comments.nodes).map((entry) => normalizeGraphQLReviewComment(asRecord(entry))),
    }));
  return {
    normalized: threads,
    page: { returned: threads.length, complete: result.complete, pages: result.pages, ...(result.truncated_reason ? { truncated_reason: result.truncated_reason } : {}) },
  };
}

const GET_REVIEW_THREAD_QUERY = `
  query GetReviewThread($threadId: ID!) {
    node(id: $threadId) {
      ... on PullRequestReviewThread {
        id
        isResolved
      }
    }
  }
`;

export interface GetReviewThreadParams {
  /** GitHub's GraphQL node id for the review thread (not a REST comment id). */
  threadId: string;
}

/**
 * Fetches one review thread's current resolution state directly by its
 * GraphQL node id - the read github.resolve-thread reconciliation needs to
 * positively determine whether an ambiguous resolveReviewThread attempt
 * already applied, without needing the thread's owning repository/PR.
 */
export async function getReviewThread(client: GitHubRestClient, params: GetReviewThreadParams, signal?: AbortSignal) {
  const data = await client.graphql<{ node: { id: string; isResolved: boolean } | null }>(
    GET_REVIEW_THREAD_QUERY,
    { threadId: params.threadId },
    signal,
  );
  if (!data.node) return { normalized: undefined };
  return { normalized: { id: data.node.id, isResolved: data.node.isResolved } };
}

const RESOLVE_REVIEW_THREAD_MUTATION = `
  mutation ResolveReviewThread($threadId: ID!) {
    resolveReviewThread(input: { threadId: $threadId }) {
      thread { id isResolved }
    }
  }
`;

export interface ResolveReviewThreadParams {
  /** GitHub's GraphQL node id for the review thread (not a REST comment id - see docs.github.com/en/graphql/reference/mutations#resolvereviewthread). */
  threadId: string;
}

export async function resolveReviewThread(client: GitHubRestClient, params: ResolveReviewThreadParams, signal?: AbortSignal) {
  const data = await client.graphql<{ resolveReviewThread: { thread: { id: string; isResolved: boolean } } }>(
    RESOLVE_REVIEW_THREAD_MUTATION,
    { threadId: params.threadId },
    signal,
  );
  return { ok: true, resolved: data.resolveReviewThread.thread.isResolved };
}
