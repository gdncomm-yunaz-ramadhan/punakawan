import type { GitHubRestClient } from './restClient.js';
import {
  normalizeCheckRun,
  normalizeIssueComment,
  normalizePullRequest,
  normalizePullRequestFile,
  normalizeReviewComment,
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
