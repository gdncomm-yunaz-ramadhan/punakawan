import type { AdapterManifest } from '@punakawan/schema-types';

/**
 * Manifest for the GitHub adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2/§16
 * and punakawan-architecture-enhancement-plan.md §8 (create_pr/review_pr/
 * fix_pr_review).
 *
 * Read operations (PR metadata, diff files, CI checks, comments) are
 * side-effect free. Writes (creating a PR, labeling, requesting reviewers,
 * replying to a review comment, resolving a thread) are declared with
 * `approval: "required"`, enforced the same way Atlassian writes are.
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
    'github.getPullRequest': { side_effect: false },
    'github.getPullRequestFiles': { side_effect: false },
    'github.getPullRequestChecks': { side_effect: false },
    'github.listPullRequestComments': { side_effect: false },
    'github.createPullRequest': { side_effect: true, approval: 'required' },
    'github.addLabels': { side_effect: true, approval: 'required' },
    'github.requestReviewers': { side_effect: true, approval: 'required' },
    'github.replyToReviewComment': { side_effect: true, approval: 'required' },
    'github.resolveReviewThread': { side_effect: true, approval: 'required' },
  },
};
