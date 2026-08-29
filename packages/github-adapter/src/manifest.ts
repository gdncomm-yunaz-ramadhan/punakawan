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
    'github.getRepository': { side_effect: false },
    'github.getPullRequest': { side_effect: false },
    'github.getPullRequestFiles': { side_effect: false },
    'github.getPullRequestChecks': { side_effect: false },
    'github.listPullRequestComments': { side_effect: false },
    'github.listUnresolvedReviewThreads': { side_effect: false },
    'github.createPullRequest': { side_effect: true },
    'github.addLabels': { side_effect: true },
    'github.requestReviewers': { side_effect: true },
    'github.replyToReviewComment': { side_effect: true },
    'github.createPullRequestReview': { side_effect: true },
    'github.resolveReviewThread': { side_effect: true },
  },
};
