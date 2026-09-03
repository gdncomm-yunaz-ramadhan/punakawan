import type { AdapterManifest } from '@punakawan/schema-types';

/**
 * Manifest for the GitHub adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2 and
 * punakawan-architecture-enhancement-plan.md §8 (create_pr/review_pr/
 * fix_pr_review).
 *
 * Read operations (PR metadata, diff files, CI checks, comments) are
 * side-effect free. Writes (creating a PR, labeling, requesting reviewers,
 * replying to a review comment, submitting a review, resolving a thread)
 * declare `side_effect: true`, which routes them through
 * validation/outbox/audit on the Go core side — it never means the write is
 * gated on user confirmation; execution proceeds once authorized by policy.
 *
 * Every operation also declares a description and an input_schema: the Go
 * core validates a payload against input_schema before it is ever enqueued
 * or executed, so a malformed call fails fast with a precise diagnostic
 * instead of reaching this adapter process at all.
 */
export const manifest: AdapterManifest = {
  id: 'github',
  name: 'GitHub adapter',
  version: '0.1.0',
  protocol: 'punakawan.adapter/v1',
  runtime: 'node',
  provides: ['github', 'pull-request'],
  permissions: {
    network: { hosts: ['api.github.com'] },
    filesystem: { read: [], write: [] },
    secrets: ['GITHUB_TOKEN'],
  },
  operations: {
    'github.getRepository': {
      side_effect: false,
      description: 'Check whether the configured credential can see a repository, and what access it has.',
      input_schema: {
        type: 'object',
        required: ['repository'],
        properties: { repository: { type: 'string' } },
      },
    },
    'github.searchRepositories': {
      side_effect: false,
      description: 'Find repositories by name, optionally scoped to the owners this credential speaks for, so a repository named without an owner resolves to exactly one.',
      input_schema: {
        type: 'object',
        required: ['name'],
        properties: {
          name: { type: 'string' },
          owners: { type: 'array', items: { type: 'string' } },
          limit: { type: 'number' },
        },
      },
    },
    'github.getPullRequest': {
      side_effect: false,
      description: 'Fetch one pull request\'s current metadata, including its labels and requested reviewers.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber'],
        properties: { repository: { type: 'string' }, pullRequestNumber: { type: 'number' } },
      },
    },
    'github.getPullRequestFiles': {
      side_effect: false,
      description: 'Fetch every file changed by a pull request, across all pages.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber'],
        properties: { repository: { type: 'string' }, pullRequestNumber: { type: 'number' } },
      },
    },
    'github.getPullRequestChecks': {
      side_effect: false,
      description: 'Fetch every check run reported for a commit, across all pages.',
      input_schema: {
        type: 'object',
        required: ['repository', 'ref'],
        properties: { repository: { type: 'string' }, ref: { type: 'string' } },
      },
    },
    'github.getCommitStatus': {
      side_effect: false,
      description: "Fetch a commit's combined legacy Status API state, separate from its Check Runs.",
      input_schema: {
        type: 'object',
        required: ['repository', 'ref'],
        properties: { repository: { type: 'string' }, ref: { type: 'string' } },
      },
    },
    'github.listPullRequestComments': {
      side_effect: false,
      description: 'Fetch every diff-line review comment and general issue comment on a pull request, across all pages.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber'],
        properties: { repository: { type: 'string' }, pullRequestNumber: { type: 'number' } },
      },
    },
    'github.listUnresolvedReviewThreads': {
      side_effect: false,
      description: 'Fetch a pull request\'s still-unresolved review threads, across all pages.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber'],
        properties: { repository: { type: 'string' }, pullRequestNumber: { type: 'number' } },
      },
    },
    'github.listPullRequestReviews': {
      side_effect: false,
      description: 'List every review submitted on a pull request, across all pages.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber'],
        properties: { repository: { type: 'string' }, pullRequestNumber: { type: 'number' } },
      },
    },
    'github.findPullRequest': {
      side_effect: false,
      description: 'Search open and closed pull requests for an exact head/base branch match.',
      input_schema: {
        type: 'object',
        required: ['repository', 'headBranch', 'baseBranch'],
        properties: { repository: { type: 'string' }, headBranch: { type: 'string' }, baseBranch: { type: 'string' } },
      },
    },
    'github.getReviewThread': {
      side_effect: false,
      description: 'Fetch one review thread\'s current resolution state by its GraphQL node id.',
      input_schema: {
        type: 'object',
        required: ['threadId'],
        properties: { threadId: { type: 'string' } },
      },
    },
    'github.createPullRequest': {
      side_effect: true,
      description: 'Open a new pull request.',
      input_schema: {
        type: 'object',
        required: ['repository', 'baseBranch', 'headBranch', 'title'],
        properties: {
          repository: { type: 'string' },
          baseBranch: { type: 'string' },
          headBranch: { type: 'string' },
          title: { type: 'string' },
          body: { type: 'string' },
          draft: { type: 'boolean' },
        },
      },
    },
    'github.addLabels': {
      side_effect: true,
      description: 'Add labels to a pull request.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber', 'labels'],
        properties: {
          repository: { type: 'string' },
          pullRequestNumber: { type: 'number' },
          labels: { type: 'array', items: { type: 'string' } },
        },
      },
    },
    'github.requestReviewers': {
      side_effect: true,
      description: 'Request reviewers on a pull request.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber', 'reviewers'],
        properties: {
          repository: { type: 'string' },
          pullRequestNumber: { type: 'number' },
          reviewers: { type: 'array', items: { type: 'string' } },
        },
      },
    },
    'github.replyToReviewComment': {
      side_effect: true,
      description: 'Reply to an existing review comment.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber', 'commentId', 'body'],
        properties: {
          repository: { type: 'string' },
          pullRequestNumber: { type: 'number' },
          commentId: { type: 'string' },
          body: { type: 'string' },
        },
      },
    },
    'github.createPullRequestReview': {
      side_effect: true,
      description: 'Submit a review (approve, request changes, or comment) on a pull request.',
      input_schema: {
        type: 'object',
        required: ['repository', 'pullRequestNumber', 'body', 'event'],
        properties: {
          repository: { type: 'string' },
          pullRequestNumber: { type: 'number' },
          body: { type: 'string' },
          event: { type: 'string', enum: ['APPROVE', 'REQUEST_CHANGES', 'COMMENT'] },
          commitId: { type: 'string' },
          comments: {
            type: 'array',
            items: {
              type: 'object',
              required: ['path', 'line', 'side', 'body'],
              properties: {
                path: { type: 'string' },
                line: { type: 'number' },
                side: { type: 'string', enum: ['LEFT', 'RIGHT'] },
                body: { type: 'string' },
                startLine: { type: 'number' },
                startSide: { type: 'string', enum: ['LEFT', 'RIGHT'] },
              },
            },
          },
        },
      },
    },
    'github.resolveReviewThread': {
      side_effect: true,
      description: 'Mark a review thread resolved.',
      input_schema: {
        type: 'object',
        required: ['threadId'],
        properties: { threadId: { type: 'string' } },
      },
    },
  },
};
