import { AdapterManifestSchema } from '@punakawan/schema-types';
import type { Handler } from '@punakawan/adapter-sdk';
import { GitHubRestClient, loadConfigFromEnv } from './restClient.js';
import { manifest } from './manifest.js';
import {
  addLabels,
  createPullRequest,
  getPullRequest,
  getPullRequestChecks,
  getPullRequestFiles,
  listPullRequestComments,
  replyToReviewComment,
  requestReviewers,
  resolveReviewThread,
} from './operations.js';

/**
 * Builds the `initialize`/`execute`/`shutdown` handler set for the GitHub
 * adapter, mirroring packages/adapter-atlassian/src/adapter.ts's pattern.
 * Split out from the top-level script (see src/index.ts) so tests can
 * exercise it with an injected fetch implementation instead of live APIs.
 */
export function createHandlers(options?: { fetchImpl?: typeof fetch; env?: NodeJS.ProcessEnv }): Record<string, Handler> {
  let client: GitHubRestClient | undefined;
  const env = options?.env ?? process.env;

  function getClient(): GitHubRestClient {
    if (!client) {
      const config = loadConfigFromEnv(env);
      client = new GitHubRestClient(config, options?.fetchImpl);
    }
    return client;
  }

  return {
    async initialize(params) {
      const parsed = AdapterManifestSchema.parse(manifest);
      void params;
      return { ok: true, id: parsed.id, version: parsed.version };
    },

    async capabilities() {
      return AdapterManifestSchema.parse(manifest);
    },

    async execute(params) {
      const { op, ...rest } = params as { op: string } & Record<string, unknown>;

      switch (op) {
        case 'github.getPullRequest': {
          const { repository, pullRequestNumber } = rest as { repository: string; pullRequestNumber: number };
          if (!repository || pullRequestNumber === undefined) {
            throw new Error('github.getPullRequest requires "repository" and "pullRequestNumber"');
          }
          return getPullRequest(getClient(), { repository, pullRequestNumber });
        }
        case 'github.getPullRequestFiles': {
          const { repository, pullRequestNumber } = rest as { repository: string; pullRequestNumber: number };
          if (!repository || pullRequestNumber === undefined) {
            throw new Error('github.getPullRequestFiles requires "repository" and "pullRequestNumber"');
          }
          return getPullRequestFiles(getClient(), { repository, pullRequestNumber });
        }
        case 'github.getPullRequestChecks': {
          const { repository, ref } = rest as { repository: string; ref: string };
          if (!repository || !ref) throw new Error('github.getPullRequestChecks requires "repository" and "ref"');
          return getPullRequestChecks(getClient(), { repository, ref });
        }
        case 'github.listPullRequestComments': {
          const { repository, pullRequestNumber } = rest as { repository: string; pullRequestNumber: number };
          if (!repository || pullRequestNumber === undefined) {
            throw new Error('github.listPullRequestComments requires "repository" and "pullRequestNumber"');
          }
          return listPullRequestComments(getClient(), { repository, pullRequestNumber });
        }
        case 'github.createPullRequest': {
          const { repository, baseBranch, headBranch, title, body, draft } = rest as {
            repository: string; baseBranch: string; headBranch: string; title: string; body: string; draft?: boolean;
          };
          if (!repository || !baseBranch || !headBranch || !title) {
            throw new Error('github.createPullRequest requires "repository", "baseBranch", "headBranch", and "title"');
          }
          return createPullRequest(getClient(), { repository, baseBranch, headBranch, title, body: body ?? '', draft });
        }
        case 'github.addLabels': {
          const { repository, pullRequestNumber, labels } = rest as { repository: string; pullRequestNumber: number; labels: string[] };
          if (!repository || pullRequestNumber === undefined || !labels) {
            throw new Error('github.addLabels requires "repository", "pullRequestNumber", and "labels"');
          }
          return addLabels(getClient(), { repository, pullRequestNumber, labels });
        }
        case 'github.requestReviewers': {
          const { repository, pullRequestNumber, reviewers } = rest as { repository: string; pullRequestNumber: number; reviewers: string[] };
          if (!repository || pullRequestNumber === undefined || !reviewers) {
            throw new Error('github.requestReviewers requires "repository", "pullRequestNumber", and "reviewers"');
          }
          return requestReviewers(getClient(), { repository, pullRequestNumber, reviewers });
        }
        case 'github.replyToReviewComment': {
          const { repository, pullRequestNumber, commentId, body } = rest as {
            repository: string; pullRequestNumber: number; commentId: string; body: string;
          };
          if (!repository || pullRequestNumber === undefined || !commentId || !body) {
            throw new Error('github.replyToReviewComment requires "repository", "pullRequestNumber", "commentId", and "body"');
          }
          return replyToReviewComment(getClient(), { repository, pullRequestNumber, commentId, body });
        }
        case 'github.resolveReviewThread': {
          const { threadId } = rest as { threadId: string };
          if (!threadId) throw new Error('github.resolveReviewThread requires "threadId"');
          return resolveReviewThread(getClient(), { threadId });
        }
        default:
          throw new Error(`Unsupported op: ${op}`);
      }
    },

    async shutdown() {
      if (client) await client.close();
      return { ok: true };
    },
  };
}
