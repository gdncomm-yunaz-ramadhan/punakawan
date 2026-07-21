import { AdapterManifestSchema } from '@punakawan/schema-types';
import type { Handler } from '@punakawan/adapter-sdk';
import { AtlassianMcpClient, loadConfigFromEnv, type TransportFactory } from './mcpClient.js';
import { manifest } from './manifest.js';
import {
  addJiraComment,
  addWorklog,
  createJiraSubtask,
  editJiraIssueFields,
  getConfluencePage,
  getIssueTypeFieldMeta,
  getJiraIssue,
  getTransitionsForJiraIssue,
  searchConfluence,
  searchJira,
  transitionJiraIssue,
} from './operations.js';

/**
 * Builds the `initialize`/`execute`/`shutdown` handler set for the Atlassian
 * adapter, mirroring packages/adapter-sdk/src/prototypeAdapter.ts's pattern.
 * Split out from the top-level script (see src/index.ts) so tests can
 * exercise it with an injected fake-server transport instead of the real
 * mcp.atlassian.com endpoint.
 */
export function createHandlers(options?: {
  transportFactory?: TransportFactory;
  cloudIdResolver?: (host: string) => Promise<string>;
  env?: NodeJS.ProcessEnv;
}): Record<string, Handler> {
  let client: AtlassianMcpClient | undefined;

  function getClient(): AtlassianMcpClient {
    if (!client) {
      const config = loadConfigFromEnv(options?.env ?? process.env);
      client = new AtlassianMcpClient(config, options?.transportFactory, options?.cloudIdResolver);
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
          const { issueIdOrKey } = rest as { issueIdOrKey: string };
          if (!issueIdOrKey) throw new Error('atlassian.getJiraIssue requires "issueIdOrKey"');
          return getJiraIssue(getClient(), { issueIdOrKey });
        }
        case 'atlassian.getConfluencePage': {
          const { pageId, contentFormat } = rest as { pageId: string; contentFormat?: string };
          if (!pageId) throw new Error('atlassian.getConfluencePage requires "pageId"');
          return getConfluencePage(getClient(), { pageId, contentFormat });
        }
        case 'atlassian.searchJira': {
          const { jql, fields, maxResults } = rest as { jql: string; fields?: string[]; maxResults?: number };
          if (!jql) throw new Error('atlassian.searchJira requires "jql"');
          return searchJira(getClient(), { jql, fields, maxResults });
        }
        case 'atlassian.searchConfluence': {
          const { cql } = rest as { cql: string };
          if (!cql) throw new Error('atlassian.searchConfluence requires "cql"');
          return searchConfluence(getClient(), { cql });
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
        case 'atlassian.editJiraIssueFields': {
          const { issueIdOrKey, fields } = rest as { issueIdOrKey: string; fields: Record<string, unknown> };
          if (!issueIdOrKey || !fields) {
            throw new Error('atlassian.editJiraIssueFields requires "issueIdOrKey" and "fields"');
          }
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
