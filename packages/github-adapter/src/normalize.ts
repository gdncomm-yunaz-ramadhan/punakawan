function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' ? value : undefined;
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined;
}

export interface NormalizedPullRequest {
  number: number;
  title: string;
  body: string;
  state: string;
  draft: boolean;
  merged: boolean;
  mergeable: boolean | undefined;
  baseRef: string;
  headRef: string;
  headSha: string;
  author: string | undefined;
  url: string | undefined;
  createdAt: string | undefined;
  updatedAt: string | undefined;
}

export function normalizePullRequest(payload: Record<string, unknown>): NormalizedPullRequest {
  const base = asRecord(payload.base);
  const head = asRecord(payload.head);
  const user = asRecord(payload.user);
  return {
    number: asNumber(payload.number) ?? 0,
    title: asString(payload.title) ?? '',
    body: asString(payload.body) ?? '',
    state: asString(payload.state) ?? 'unknown',
    draft: asBoolean(payload.draft) ?? false,
    merged: asBoolean(payload.merged) ?? false,
    mergeable: asBoolean(payload.mergeable),
    baseRef: asString(base.ref) ?? '',
    headRef: asString(head.ref) ?? '',
    headSha: asString(head.sha) ?? '',
    author: asString(user.login),
    url: asString(payload.html_url),
    createdAt: asString(payload.created_at),
    updatedAt: asString(payload.updated_at),
  };
}

export interface NormalizedPullRequestFile {
  path: string;
  status: string;
  additions: number;
  deletions: number;
  changes: number;
  patch: string | undefined;
}

export function normalizePullRequestFile(payload: Record<string, unknown>): NormalizedPullRequestFile {
  return {
    path: asString(payload.filename) ?? '',
    status: asString(payload.status) ?? 'unknown',
    additions: asNumber(payload.additions) ?? 0,
    deletions: asNumber(payload.deletions) ?? 0,
    changes: asNumber(payload.changes) ?? 0,
    patch: asString(payload.patch),
  };
}

export interface NormalizedCheckRun {
  name: string;
  status: string;
  conclusion: string | undefined;
  url: string | undefined;
}

export function normalizeCheckRun(payload: Record<string, unknown>): NormalizedCheckRun {
  return {
    name: asString(payload.name) ?? 'unknown',
    status: asString(payload.status) ?? 'unknown',
    conclusion: asString(payload.conclusion),
    url: asString(payload.html_url),
  };
}

export interface NormalizedComment {
  id: string;
  /** "review" for a diff-line comment (pulls/comments); "issue" for a general PR-level comment (issues/comments). */
  kind: 'review' | 'issue';
  author: string | undefined;
  body: string;
  path: string | undefined;
  line: number | undefined;
  /** The review-thread comments API's own id for this comment's thread, if any (used to reply/resolve). */
  inReplyToId: string | undefined;
  createdAt: string | undefined;
  updatedAt: string | undefined;
}

export function normalizeReviewComment(payload: Record<string, unknown>): NormalizedComment {
  const user = asRecord(payload.user);
  return {
    id: String(payload.id ?? ''),
    kind: 'review',
    author: asString(user.login),
    body: asString(payload.body) ?? '',
    path: asString(payload.path),
    line: asNumber(payload.line) ?? asNumber(payload.original_line),
    inReplyToId: payload.in_reply_to_id !== undefined ? String(payload.in_reply_to_id) : undefined,
    createdAt: asString(payload.created_at),
    updatedAt: asString(payload.updated_at),
  };
}

export interface NormalizedReviewThread {
  /** GraphQL node id - pass this to github.resolveReviewThread's threadId, not any REST comment id. */
  id: string;
  comments: NormalizedComment[];
}

export function normalizeGraphQLReviewComment(payload: Record<string, unknown>): NormalizedComment {
  const author = asRecord(payload.author);
  return {
    id: String(payload.id ?? ''),
    kind: 'review',
    author: asString(author.login),
    body: asString(payload.body) ?? '',
    path: asString(payload.path),
    line: asNumber(payload.line),
    inReplyToId: undefined,
    createdAt: asString(payload.createdAt),
    updatedAt: asString(payload.updatedAt),
  };
}

export function normalizeIssueComment(payload: Record<string, unknown>): NormalizedComment {
  const user = asRecord(payload.user);
  return {
    id: String(payload.id ?? ''),
    kind: 'issue',
    author: asString(user.login),
    body: asString(payload.body) ?? '',
    path: undefined,
    line: undefined,
    inReplyToId: undefined,
    createdAt: asString(payload.created_at),
    updatedAt: asString(payload.updated_at),
  };
}
