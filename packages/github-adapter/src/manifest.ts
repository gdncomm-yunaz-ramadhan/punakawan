import type { AdapterManifest } from '@punakawan/schema-types';

type InputSchema = AdapterManifest['operations'][string]['input_schema'];
type Operation = AdapterManifest['operations'][string];

const string = { type: 'string' };
const number = { type: 'number' };
const boolean = { type: 'boolean' };
const stringArray = { type: 'array', items: string };

function inputSchema(required: string[], properties: Record<string, unknown>): InputSchema {
  return { type: 'object', properties, ...(required.length > 0 ? { required } : {}) };
}

function read(description: string, required: string[], properties: Record<string, unknown>): Operation {
  return { side_effect: false, description, input_schema: inputSchema(required, properties) };
}

function write(description: string, required: string[], properties: Record<string, unknown>): Operation {
  return { side_effect: true, approval: 'required', description, input_schema: inputSchema(required, properties) };
}

/**
 * Manifest for the GitHub adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2/§16
 * and punakawan-architecture-enhancement-plan.md §8 (create_pr/review_pr/
 * fix_pr_review).
 *
 * Read operations (PR metadata, diff files, CI checks, comments) are
 * side-effect free. Writes (creating a PR, labeling, requesting reviewers,
 * replying to a review comment, submitting a review, resolving a thread)
 * are declared with `approval: "required"`, enforced the same way Atlassian
 * writes are.
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
    'github.getRepository': read('Get a GitHub repository.', ['repository'], { repository: string }),
    'github.getPullRequest': read('Get a GitHub pull request.', ['repository', 'pullRequestNumber'], { repository: string, pullRequestNumber: number }),
    'github.getPullRequestFiles': read('List files changed by a pull request.', ['repository', 'pullRequestNumber'], { repository: string, pullRequestNumber: number }),
    'github.getPullRequestChecks': read('List checks for a Git ref.', ['repository', 'ref'], { repository: string, ref: string }),
    'github.listPullRequestComments': read('List issue and review comments on a pull request.', ['repository', 'pullRequestNumber'], { repository: string, pullRequestNumber: number }),
    'github.listUnresolvedReviewThreads': read('List unresolved pull-request review threads.', ['repository', 'pullRequestNumber'], { repository: string, pullRequestNumber: number }),
    'github.createPullRequest': write('Create a pull request.', ['repository', 'baseBranch', 'headBranch', 'title'], { repository: string, baseBranch: string, headBranch: string, title: string, body: string, draft: boolean }),
    'github.addLabels': write('Add labels to a pull request.', ['repository', 'pullRequestNumber', 'labels'], { repository: string, pullRequestNumber: number, labels: stringArray }),
    'github.requestReviewers': write('Request pull-request reviewers.', ['repository', 'pullRequestNumber', 'reviewers'], { repository: string, pullRequestNumber: number, reviewers: stringArray }),
    'github.replyToReviewComment': write('Reply to a pull-request review comment.', ['repository', 'pullRequestNumber', 'commentId', 'body'], { repository: string, pullRequestNumber: number, commentId: string, body: string }),
    'github.createPullRequestReview': write('Submit a pull-request review.', ['repository', 'pullRequestNumber', 'body', 'event'], { repository: string, pullRequestNumber: number, body: string, event: { type: 'string', enum: ['APPROVE', 'REQUEST_CHANGES', 'COMMENT'] }, commitId: string, comments: { type: 'array', items: { type: 'object', required: ['path', 'line', 'side', 'body'], properties: { path: string, line: number, side: { type: 'string', enum: ['LEFT', 'RIGHT'] }, body: string, startLine: number, startSide: { type: 'string', enum: ['LEFT', 'RIGHT'] } } } } }),
    'github.resolveReviewThread': write('Resolve a pull-request review thread.', ['threadId'], { threadId: string }),
  },
};
