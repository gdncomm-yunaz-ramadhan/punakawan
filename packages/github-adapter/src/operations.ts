import { GitHubRestError, type GitHubRestClient } from './restClient.js';
import {
  INACCESSIBLE_REPOSITORY,
  normalizeCheckRun,
  normalizeGraphQLReviewComment,
  normalizeIssueComment,
  normalizePullRequest,
  normalizePullRequestFile,
  normalizeRepositoryAccess,
  normalizeReviewComment,
  type NormalizedReviewThread,
} from './normalize.js';

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

export interface GetPullRequestParams extends RepoRef {
  pullRequestNumber: number;
}

export async function getPullRequest(client: GitHubRestClient, params: GetPullRequestParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}`);
  return { normalized: normalizePullRequest(raw.data) };
}

export interface GetPullRequestFilesParams extends RepoRef {
  pullRequestNumber: number;
}

export async function getPullRequestFiles(client: GitHubRestClient, params: GetPullRequestFilesParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<unknown[]>(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/files`, {
    query: { per_page: 100 },
  });
  return { normalized: asArray(raw.data).map((entry) => normalizePullRequestFile(asRecord(entry))) };
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
export async function getRepository(client: GitHubRestClient, params: GetRepositoryParams) {
  const { owner, repo } = splitRepo(params.repository);
  try {
    const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}`);
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

export async function getPullRequestChecks(client: GitHubRestClient, params: GetPullRequestChecksParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/commits/${params.ref}/check-runs`, {
    query: { per_page: 100 },
  });
  const runs = asArray(raw.data.check_runs).map((entry) => normalizeCheckRun(asRecord(entry)));
  return { normalized: runs };
}

export interface ListPullRequestCommentsParams extends RepoRef {
  pullRequestNumber: number;
}

/** Merges diff-line review comments and general issue-level comments into one normalized, chronologically-tagged list. */
export async function listPullRequestComments(client: GitHubRestClient, params: ListPullRequestCommentsParams) {
  const { owner, repo } = splitRepo(params.repository);
  const [reviewRaw, issueRaw] = await Promise.all([
    client.request<unknown[]>(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/comments`, { query: { per_page: 100 } }),
    client.request<unknown[]>(`/repos/${owner}/${repo}/issues/${params.pullRequestNumber}/comments`, { query: { per_page: 100 } }),
  ]);
  const reviewComments = asArray(reviewRaw.data).map((entry) => normalizeReviewComment(asRecord(entry)));
  const issueComments = asArray(issueRaw.data).map((entry) => normalizeIssueComment(asRecord(entry)));
  return { normalized: [...reviewComments, ...issueComments] };
}

export interface CreatePullRequestParams extends RepoRef {
  baseBranch: string;
  headBranch: string;
  title: string;
  body: string;
  draft?: boolean;
}

export async function createPullRequest(client: GitHubRestClient, params: CreatePullRequestParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/pulls`, {
    method: 'POST',
    body: { title: params.title, body: params.body, base: params.baseBranch, head: params.headBranch, draft: params.draft ?? false },
  });
  return { normalized: normalizePullRequest(raw.data) };
}

export interface AddLabelsParams extends RepoRef {
  pullRequestNumber: number;
  labels: string[];
}

export async function addLabels(client: GitHubRestClient, params: AddLabelsParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(`/repos/${owner}/${repo}/issues/${params.pullRequestNumber}/labels`, {
    method: 'POST',
    body: { labels: params.labels },
  });
  return { ok: true, labels: asArray(raw.data).map((entry) => asRecord(entry).name).filter((name): name is string => typeof name === 'string') };
}

export interface RequestReviewersParams extends RepoRef {
  pullRequestNumber: number;
  reviewers: string[];
}

export async function requestReviewers(client: GitHubRestClient, params: RequestReviewersParams) {
  const { owner, repo } = splitRepo(params.repository);
  await client.request(`/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/requested_reviewers`, {
    method: 'POST',
    body: { reviewers: params.reviewers },
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

export async function createPullRequestReview(client: GitHubRestClient, params: CreatePullRequestReviewParams) {
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
  });
  return { ok: true, reviewId: typeof raw.data.id === 'number' || typeof raw.data.id === 'string' ? String(raw.data.id) : undefined };
}

export interface ReplyToReviewCommentParams extends RepoRef {
  pullRequestNumber: number;
  commentId: string;
  body: string;
}

export async function replyToReviewComment(client: GitHubRestClient, params: ReplyToReviewCommentParams) {
  const { owner, repo } = splitRepo(params.repository);
  const raw = await client.request<Record<string, unknown>>(
    `/repos/${owner}/${repo}/pulls/${params.pullRequestNumber}/comments/${params.commentId}/replies`,
    { method: 'POST', body: { body: params.body } },
  );
  return { normalized: normalizeReviewComment(raw.data) };
}

const UNRESOLVED_REVIEW_THREADS_QUERY = `
  query UnresolvedReviewThreads($owner: String!, $name: String!, $number: Int!) {
    repository(owner: $owner, name: $name) {
      pullRequest(number: $number) {
        reviewThreads(first: 100) {
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

interface GraphQLReviewThreadsResponse {
  repository: {
    pullRequest: {
      reviewThreads: {
        nodes: { id: string; isResolved: boolean; comments: { nodes: Record<string, unknown>[] } }[];
      };
    };
  };
}

/**
 * Fetches a PR's still-open review threads via GraphQL - REST's pulls
 * comments endpoint has no per-comment resolution state at all, so
 * filtering to "unresolved" is only possible through GraphQL's
 * reviewThreads.isResolved field (docs.github.com/en/graphql/reference/objects#pullrequestreviewthread).
 */
export async function listUnresolvedReviewThreads(client: GitHubRestClient, params: ListUnresolvedReviewThreadsParams) {
  const { owner, repo } = splitRepo(params.repository);
  const data = await client.graphql<GraphQLReviewThreadsResponse>(UNRESOLVED_REVIEW_THREADS_QUERY, {
    owner, name: repo, number: params.pullRequestNumber,
  });
  const threads = data.repository.pullRequest.reviewThreads.nodes
    .filter((thread) => !thread.isResolved)
    .map((thread): NormalizedReviewThread => ({
      id: thread.id,
      comments: asArray(thread.comments.nodes).map((entry) => normalizeGraphQLReviewComment(asRecord(entry))),
    }));
  return { normalized: threads };
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

export async function resolveReviewThread(client: GitHubRestClient, params: ResolveReviewThreadParams) {
  const data = await client.graphql<{ resolveReviewThread: { thread: { id: string; isResolved: boolean } } }>(
    RESOLVE_REVIEW_THREAD_MUTATION,
    { threadId: params.threadId },
  );
  return { ok: true, resolved: data.resolveReviewThread.thread.isResolved };
}
