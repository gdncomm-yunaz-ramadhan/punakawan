import { AdapterManifestSchema } from '@punakawan/schema-types';
import type { Handler } from '@punakawan/adapter-sdk';
import { AtlassianRestClient, loadConfigFromEnv } from './restClient.js';
import { manifest } from './manifest.js';
import {
  addJiraComment,
  addWorklog,
  createJiraSubtask,
  deleteJiraAttachment,
  downloadJiraAttachment,
  editJiraIssue,
  editJiraIssueFields,
  getConfluencePage,
  getIssueTypeFieldMeta,
  getJiraComments,
  getJiraEpic,
  getJiraIssue,
  getJiraRemoteLinks,
  getTransitionsForJiraIssue,
  listJiraAttachments,
  searchConfluence,
  searchJira,
  transitionJiraIssue,
  uploadJiraAttachment,
} from './operations.js';

/**
 * Builds the `initialize`/`execute`/`shutdown` handler set for the Atlassian
 * adapter, mirroring packages/adapter-sdk/src/prototypeAdapter.ts's pattern.
 * Split out from the top-level script (see src/index.ts) so tests can
 * exercise it with an injected fetch implementation instead of live APIs.
 */
export function createHandlers(options?: {
  fetchImpl?: typeof fetch;
  cloudIdResolver?: (host: string) => Promise<string>;
  env?: NodeJS.ProcessEnv;
}): Record<string, Handler> {
  let client: AtlassianRestClient | undefined;
  const env = options?.env ?? process.env;

  function getClient(): AtlassianRestClient {
    if (!client) {
      const config = loadConfigFromEnv(env);
      client = new AtlassianRestClient(config, options?.fetchImpl, options?.cloudIdResolver);
    }
    return client;
  }

  return {
    async initialize(params) {
      const parsed = AdapterManifestSchema.parse(manifest);
      // params is the manifest the host expects to see; validating our own
      // manifest against the shared schema is the contract check that
      // matters here (mirrors prototypeAdapter.ts's initialize).
      void params;
      return { ok: true, id: parsed.id, version: parsed.version };
    },

    async capabilities() {
      return AdapterManifestSchema.parse(manifest);
    },

    async execute(params) {
      const { op, ...rest } = params as { op: string } & Record<string, unknown>;

      switch (op) {
        case 'atlassian.getJiraIssue': {
          const { issueIdOrKey, fields, includeRaw } = rest as { issueIdOrKey: string; fields?: string[]; includeRaw?: boolean };
          if (!issueIdOrKey) throw new Error('atlassian.getJiraIssue requires "issueIdOrKey"');
          return getJiraIssue(getClient(), { issueIdOrKey, fields, includeRaw });
        }
        case 'atlassian.getJiraComments': {
          const { issueIdOrKey, startAt, maxResults } = rest as { issueIdOrKey: string; startAt?: number; maxResults?: number };
          if (!issueIdOrKey) throw new Error('atlassian.getJiraComments requires "issueIdOrKey"');
          return getJiraComments(getClient(), { issueIdOrKey, startAt, maxResults });
        }
        case 'atlassian.getJiraRemoteLinks': {
          const { issueIdOrKey, maxResults } = rest as { issueIdOrKey: string; maxResults?: number };
          if (!issueIdOrKey) throw new Error('atlassian.getJiraRemoteLinks requires "issueIdOrKey"');
          return getJiraRemoteLinks(getClient(), { issueIdOrKey, maxResults });
        }
        case 'atlassian.getJiraEpic': {
          const { epicIdOrKey, maxChildren } = rest as { epicIdOrKey: string; maxChildren?: number };
          if (!epicIdOrKey) throw new Error('atlassian.getJiraEpic requires "epicIdOrKey"');
          return getJiraEpic(getClient(), { epicIdOrKey, maxChildren });
        }
        case 'atlassian.listJiraAttachments': {
          const { issueIdOrKey, maxResults } = rest as { issueIdOrKey: string; maxResults?: number };
          if (!issueIdOrKey) throw new Error('atlassian.listJiraAttachments requires "issueIdOrKey"');
          return listJiraAttachments(getClient(), { issueIdOrKey, maxResults });
        }
        case 'atlassian.downloadJiraAttachment': {
          const { attachmentId, outputPath } = rest as { attachmentId: string; outputPath: string };
          if (!attachmentId || !outputPath) throw new Error('atlassian.downloadJiraAttachment requires "attachmentId" and "outputPath"');
          return downloadJiraAttachment(getClient(), { attachmentId, outputPath }, env.PUNAKAWAN_WORKSPACE_ROOT ?? '');
        }
        case 'atlassian.uploadJiraAttachment': {
          const { issueIdOrKey, filePath } = rest as { issueIdOrKey: string; filePath: string };
          if (!issueIdOrKey || !filePath) throw new Error('atlassian.uploadJiraAttachment requires "issueIdOrKey" and "filePath"');
          return uploadJiraAttachment(getClient(), { issueIdOrKey, filePath }, env.PUNAKAWAN_WORKSPACE_ROOT ?? '');
        }
        case 'atlassian.deleteJiraAttachment': {
          const { attachmentId } = rest as { attachmentId: string };
          if (!attachmentId) throw new Error('atlassian.deleteJiraAttachment requires "attachmentId"');
          return deleteJiraAttachment(getClient(), { attachmentId });
        }
        case 'atlassian.getConfluencePage': {
          const { pageId, contentFormat, includeRaw } = rest as { pageId: string; contentFormat?: string; includeRaw?: boolean };
          if (!pageId) throw new Error('atlassian.getConfluencePage requires "pageId"');
          return getConfluencePage(getClient(), { pageId, contentFormat, includeRaw });
        }
        case 'atlassian.searchJira': {
          const { jql, fields, maxResults, includeRaw } = rest as { jql: string; fields?: string[]; maxResults?: number; includeRaw?: boolean };
          if (!jql) throw new Error('atlassian.searchJira requires "jql"');
          return searchJira(getClient(), { jql, fields, maxResults, includeRaw });
        }
        case 'atlassian.searchConfluence': {
          const { cql, includeRaw } = rest as { cql: string; includeRaw?: boolean };
          if (!cql) throw new Error('atlassian.searchConfluence requires "cql"');
          return searchConfluence(getClient(), { cql, includeRaw });
        }
        case 'atlassian.addJiraComment': {
          const { issueIdOrKey, commentBody } = rest as { issueIdOrKey: string; commentBody: string };
          if (!issueIdOrKey || !commentBody) {
            throw new Error('atlassian.addJiraComment requires "issueIdOrKey" and "commentBody"');
          }
          return addJiraComment(getClient(), { issueIdOrKey, commentBody });
        }
        case 'atlassian.getTransitionsForJiraIssue': {
          const { issueIdOrKey } = rest as { issueIdOrKey: string };
          if (!issueIdOrKey) throw new Error('atlassian.getTransitionsForJiraIssue requires "issueIdOrKey"');
          return getTransitionsForJiraIssue(getClient(), { issueIdOrKey });
        }
        case 'atlassian.transitionJiraIssue': {
          const { issueIdOrKey, transitionId } = rest as { issueIdOrKey: string; transitionId: string };
          if (!issueIdOrKey || !transitionId) {
            throw new Error('atlassian.transitionJiraIssue requires "issueIdOrKey" and "transitionId"');
          }
          return transitionJiraIssue(getClient(), { issueIdOrKey, transitionId });
        }
        case 'atlassian.editJiraIssue': {
          const edit = rest as unknown as Parameters<typeof editJiraIssue>[1];
          if (!edit.issueIdOrKey) throw new Error('atlassian.editJiraIssue requires "issueIdOrKey"');
          return editJiraIssue(getClient(), edit);
        }
        case 'atlassian.editJiraIssueFields': {
          const { issueIdOrKey, fields } = rest as { issueIdOrKey: string; fields: Record<string, unknown> };
          if (!issueIdOrKey || !fields) throw new Error('atlassian.editJiraIssueFields requires "issueIdOrKey" and "fields"');
          return editJiraIssueFields(getClient(), { issueIdOrKey, fields });
        }
        case 'atlassian.addWorklog': {
          const { issueIdOrKey, timeSpentSeconds, comment } = rest as {
            issueIdOrKey: string;
            timeSpentSeconds: number;
            comment?: string;
          };
          if (!issueIdOrKey || timeSpentSeconds === undefined) {
            throw new Error('atlassian.addWorklog requires "issueIdOrKey" and "timeSpentSeconds"');
          }
          return addWorklog(getClient(), { issueIdOrKey, timeSpentSeconds, comment });
        }
        case 'atlassian.getIssueTypeFieldMeta': {
          const { projectIdOrKey, issueTypeId } = rest as { projectIdOrKey: string; issueTypeId: string };
          if (!projectIdOrKey || !issueTypeId) {
            throw new Error('atlassian.getIssueTypeFieldMeta requires "projectIdOrKey" and "issueTypeId"');
          }
          return getIssueTypeFieldMeta(getClient(), { projectIdOrKey, issueTypeId });
        }
        case 'atlassian.createJiraSubtask': {
          const { parentKey, projectKey, issueTypeName, candidates } = rest as {
            parentKey: string;
            projectKey: string;
            issueTypeName: string;
            candidates: { summary: string; description?: string; additionalFields?: Record<string, unknown> }[];
          };
          if (!parentKey || !projectKey || !issueTypeName || !candidates) {
            throw new Error(
              'atlassian.createJiraSubtask requires "parentKey", "projectKey", "issueTypeName", and "candidates"',
            );
          }
          return createJiraSubtask(getClient(), { parentKey, projectKey, issueTypeName, candidates });
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
