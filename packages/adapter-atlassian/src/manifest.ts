import type { AdapterManifest } from '@punakawan/schema-types';

type InputSchema = NonNullable<AdapterManifest['operations'][string]['input_schema']>;
type Operation = AdapterManifest['operations'][string];

const string = { type: 'string' };
const number = { type: 'number' };
const boolean = { type: 'boolean' };
const stringArray = { type: 'array', items: string };
const object = { type: 'object' };

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
 * Manifest for the Atlassian adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2/§16.
 *
 * Read operations are side-effect free. `atlassian.addJiraComment` is a write
 * and is declared with `approval: "required"` per §13.2 ("Apply policy before
 * writes") and the plan's broader approval-gate model (§16) — enforcing that
 * gate is separate Go-core work that reads this declaration; this adapter
 * only declares the requirement honestly.
 */
export const manifest: AdapterManifest = {
  id: 'atlassian',
  name: 'Atlassian adapter',
  version: '0.1.0',
  protocol: 'punakawan.adapter/v1',
  runtime: 'node',
  provides: ['jira', 'confluence'],
  permissions: {
    // Direct REST uses the site URL for unscoped personal tokens and
    // api.atlassian.com for scoped personal/service-account tokens.
    network: { hosts: ['api.atlassian.com', '*.atlassian.net'] },
    filesystem: { read: ['workspace://**'], write: ['workspace://**'] },
    secrets: ['ATLASSIAN_API_TOKEN', 'ATLASSIAN_EMAIL'],
  },
  operations: {
    'atlassian.searchJira': read('Search Jira issues using JQL.', ['jql'], { jql: string, fields: stringArray, maxResults: number, includeRaw: boolean }),
    'atlassian.searchConfluence': read('Search Confluence pages using CQL.', ['cql'], { cql: string, includeRaw: boolean }),
    'atlassian.getJiraIssue': read('Get one Jira issue.', ['issueIdOrKey'], { issueIdOrKey: string, fields: stringArray, includeRaw: boolean }),
    'atlassian.getJiraComments': read('List comments on a Jira issue.', ['issueIdOrKey'], { issueIdOrKey: string, startAt: number, maxResults: number }),
    'atlassian.getJiraRemoteLinks': read('List remote links on a Jira issue.', ['issueIdOrKey'], { issueIdOrKey: string, maxResults: number }),
    'atlassian.getJiraEpic': read('Get a Jira epic and its child issues.', ['epicIdOrKey'], { epicIdOrKey: string, maxChildren: number }),
    'atlassian.listJiraAttachments': read('List attachment metadata on a Jira issue.', ['issueIdOrKey'], { issueIdOrKey: string, maxResults: number }),
    'atlassian.downloadJiraAttachment': write('Download a Jira attachment into the workspace.', ['attachmentId', 'outputPath'], { attachmentId: string, outputPath: string }),
    'atlassian.uploadJiraAttachment': write('Upload a workspace file as a Jira attachment.', ['issueIdOrKey', 'filePath'], { issueIdOrKey: string, filePath: string }),
    'atlassian.deleteJiraAttachment': write('Delete a Jira attachment.', ['attachmentId'], { attachmentId: string }),
    'atlassian.getConfluencePage': read('Get one Confluence page.', ['pageId'], { pageId: string, contentFormat: string, includeRaw: boolean }),
    'atlassian.addJiraComment': write('Add an issue-level Jira comment.', ['issueIdOrKey', 'commentBody'], { issueIdOrKey: string, commentBody: string }),
    'atlassian.getTransitionsForJiraIssue': read('List available transitions for a Jira issue.', ['issueIdOrKey'], { issueIdOrKey: string }),
    'atlassian.transitionJiraIssue': write('Transition a Jira issue.', ['issueIdOrKey', 'transitionId'], { issueIdOrKey: string, transitionId: string }),
    'atlassian.editJiraIssueFields': write('Update Jira issue fields.', ['issueIdOrKey', 'fields'], { issueIdOrKey: string, fields: object }),
    'atlassian.editJiraIssue': write('Edit supported Jira issue fields.', ['issueIdOrKey'], { issueIdOrKey: string, summary: string, title: string, description: string, originalEstimate: string, remainingEstimate: string, storyPoints: number, storyPointsFieldId: string, fields: object }),
    'atlassian.addWorklog': write('Add Jira worklog time.', ['issueIdOrKey', 'timeSpentSeconds'], { issueIdOrKey: string, timeSpentSeconds: number, comment: string }),
    'atlassian.getIssueTypeFieldMeta': read('Get Jira field metadata for an issue type.', ['projectIdOrKey', 'issueTypeId'], { projectIdOrKey: string, issueTypeId: string }),
    'atlassian.createJiraIssue': write('Create a Jira issue.', ['projectKey', 'issueTypeName', 'summary'], { projectKey: string, issueTypeName: string, summary: string, description: string, parent: string, additionalFields: object }),
    'atlassian.createJiraSubtask': write('Create Jira subtasks from candidate summaries.', ['parentKey', 'projectKey', 'issueTypeName', 'candidates'], { parentKey: string, projectKey: string, issueTypeName: string, candidates: { type: 'array', items: { type: 'object', required: ['summary'], properties: { summary: string, description: string, additionalFields: object } } } }),
    'atlassian.searchJiraUsers': read('Search Jira users.', ['query'], { query: string, maxResults: number }),
    'atlassian.createIssueLink': write('Create a link between two Jira issues.', ['linkType', 'inwardIssueKey', 'outwardIssueKey'], { linkType: string, inwardIssueKey: string, outwardIssueKey: string }),
    'atlassian.listJiraBoards': read('List Jira Agile boards.', [], { projectKeyOrId: string, type: string, maxResults: number }),
    'atlassian.listJiraSprints': read('List sprints for one Jira Agile board.', ['boardId'], { boardId: number, state: string, maxResults: number }),
  },
};
