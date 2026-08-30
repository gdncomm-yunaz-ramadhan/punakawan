import type { AdapterManifest } from '@punakawan/schema-types';

/**
 * Manifest for the Atlassian adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2.
 *
 * Read operations are side-effect free. A write operation declares
 * `side_effect: true`, which routes it through validation/outbox/audit on
 * the Go core side — it never means the write is gated on user
 * confirmation; execution proceeds once authorized by policy.
 *
 * Every operation also declares a description and an input_schema: the Go
 * core validates a payload against input_schema before it is ever enqueued
 * or executed, so a malformed call fails fast with a precise diagnostic
 * instead of reaching this adapter process at all.
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
    'atlassian.searchJira': {
      side_effect: false,
      description: 'Search Jira issues by JQL, returning compact normalized results.',
      input_schema: {
        type: 'object',
        required: ['jql'],
        properties: {
          jql: { type: 'string' },
          fields: { type: 'array', items: { type: 'string' } },
          maxResults: { type: 'number' },
          includeRaw: { type: 'boolean' },
        },
      },
    },
    'atlassian.searchConfluence': {
      side_effect: false,
      description: 'Search Confluence pages by CQL, returning compact normalized results.',
      input_schema: {
        type: 'object',
        required: ['cql'],
        properties: { cql: { type: 'string' }, includeRaw: { type: 'boolean' } },
      },
    },
    'atlassian.getJiraIssue': {
      side_effect: false,
      description: 'Fetch one Jira issue by id or key, normalized to its compact planning fields.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: {
          issueIdOrKey: { type: 'string' },
          fields: { type: 'array', items: { type: 'string' } },
          includeRaw: { type: 'boolean' },
        },
      },
    },
    'atlassian.getJiraComments': {
      side_effect: false,
      description: 'Fetch every comment on a Jira issue, paginating until Jira reports no more remain.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: { issueIdOrKey: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.getJiraRemoteLinks': {
      side_effect: false,
      description: 'List the remote links attached to a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: { issueIdOrKey: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.getJiraEpic': {
      side_effect: false,
      description: 'Fetch an epic and its compact child issues.',
      input_schema: {
        type: 'object',
        required: ['epicIdOrKey'],
        properties: { epicIdOrKey: { type: 'string' }, maxChildren: { type: 'number' } },
      },
    },
    'atlassian.listJiraAttachments': {
      side_effect: false,
      description: 'List an issue\'s attachment metadata (no file contents).',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: { issueIdOrKey: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.listJiraWorklogs': {
      side_effect: false,
      description: 'Fetch every worklog entry on a Jira issue, paginating until Jira reports no more remain.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: { issueIdOrKey: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.downloadJiraAttachment': {
      side_effect: true,
      description: 'Download one Jira attachment to a file inside the workspace.',
      input_schema: {
        type: 'object',
        required: ['attachmentId', 'outputPath'],
        properties: { attachmentId: { type: 'string' }, outputPath: { type: 'string' } },
      },
    },
    'atlassian.uploadJiraAttachment': {
      side_effect: true,
      description: 'Upload a file from the workspace as a new attachment on a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey', 'filePath'],
        properties: { issueIdOrKey: { type: 'string' }, filePath: { type: 'string' } },
      },
    },
    'atlassian.deleteJiraAttachment': {
      side_effect: true,
      description: 'Delete one Jira attachment by id.',
      input_schema: {
        type: 'object',
        required: ['attachmentId'],
        properties: { attachmentId: { type: 'string' } },
      },
    },
    'atlassian.getConfluencePage': {
      side_effect: false,
      description: 'Fetch one Confluence page by id, normalized to compact content.',
      input_schema: {
        type: 'object',
        required: ['pageId'],
        properties: { pageId: { type: 'string' }, contentFormat: { type: 'string' }, includeRaw: { type: 'boolean' } },
      },
    },
    'atlassian.addJiraComment': {
      side_effect: true,
      description: 'Post a comment (Markdown, rendered as Atlassian Document Format) on a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey', 'commentBody'],
        properties: { issueIdOrKey: { type: 'string' }, commentBody: { type: 'string' } },
      },
    },
    'atlassian.getTransitionsForJiraIssue': {
      side_effect: false,
      description: 'List the workflow transitions currently available on a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: { issueIdOrKey: { type: 'string' } },
      },
    },
    'atlassian.transitionJiraIssue': {
      side_effect: true,
      description: 'Apply one available workflow transition to a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey', 'transitionId'],
        properties: { issueIdOrKey: { type: 'string' }, transitionId: { type: 'string' } },
      },
    },
    'atlassian.editJiraIssueFields': {
      side_effect: true,
      description: 'Overwrite arbitrary fields on a Jira issue.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey', 'fields'],
        properties: { issueIdOrKey: { type: 'string' }, fields: { type: 'object' } },
      },
    },
    'atlassian.editJiraIssue': {
      side_effect: true,
      description: 'Edit a Jira issue\'s common fields (summary, description, estimates, story points) by convenience name.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey'],
        properties: {
          issueIdOrKey: { type: 'string' },
          summary: { type: 'string' },
          title: { type: 'string' },
          description: { type: 'string' },
          originalEstimate: { type: 'string' },
          remainingEstimate: { type: 'string' },
          storyPoints: { type: 'number' },
          storyPointsFieldId: { type: 'string' },
          fields: { type: 'object' },
        },
      },
    },
    'atlassian.addWorklog': {
      side_effect: true,
      description: 'Log time spent on a Jira issue as a worklog entry.',
      input_schema: {
        type: 'object',
        required: ['issueIdOrKey', 'timeSpentSeconds'],
        properties: { issueIdOrKey: { type: 'string' }, timeSpentSeconds: { type: 'number' }, comment: { type: 'string' } },
      },
    },
    'atlassian.getIssueTypeFieldMeta': {
      side_effect: false,
      description: 'Fetch the create-time field metadata for a project and issue type.',
      input_schema: {
        type: 'object',
        required: ['projectIdOrKey', 'issueTypeId'],
        properties: { projectIdOrKey: { type: 'string' }, issueTypeId: { type: 'string' } },
      },
    },
    'atlassian.createJiraIssue': {
      side_effect: true,
      description: 'Create a new Jira issue.',
      input_schema: {
        type: 'object',
        required: ['projectKey', 'issueTypeName', 'summary'],
        properties: {
          projectKey: { type: 'string' },
          issueTypeName: { type: 'string' },
          summary: { type: 'string' },
          description: { type: 'string' },
          parent: { type: 'string' },
          additionalFields: { type: 'object' },
        },
      },
    },
    'atlassian.createJiraSubtask': {
      side_effect: true,
      description: 'Create every candidate subtask under a parent issue that does not already exist, idempotently within and across calls.',
      input_schema: {
        type: 'object',
        required: ['parentKey', 'projectKey', 'issueTypeName', 'candidates'],
        properties: {
          parentKey: { type: 'string' },
          projectKey: { type: 'string' },
          issueTypeName: { type: 'string' },
          candidates: {
            type: 'array',
            items: {
              type: 'object',
              required: ['summary'],
              properties: {
                summary: { type: 'string' },
                description: { type: 'string' },
                additionalFields: { type: 'object' },
                intentMarker: { type: 'string' },
              },
            },
          },
        },
      },
    },
    'atlassian.searchJiraUsers': {
      side_effect: false,
      description: 'Resolve a display name or email to Jira account ids.',
      input_schema: {
        type: 'object',
        required: ['query'],
        properties: { query: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.createIssueLink': {
      side_effect: true,
      description: 'Create a link between two Jira issues.',
      input_schema: {
        type: 'object',
        required: ['linkType', 'inwardIssueKey', 'outwardIssueKey'],
        properties: { linkType: { type: 'string' }, inwardIssueKey: { type: 'string' }, outwardIssueKey: { type: 'string' } },
      },
    },
    'atlassian.listJiraBoards': {
      side_effect: false,
      description: 'List Jira Agile boards, optionally scoped to one project.',
      input_schema: {
        type: 'object',
        properties: { projectKeyOrId: { type: 'string' }, type: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
    'atlassian.listJiraSprints': {
      side_effect: false,
      description: 'List sprints on one Agile board, optionally filtered by state.',
      input_schema: {
        type: 'object',
        required: ['boardId'],
        properties: { boardId: { type: 'number' }, state: { type: 'string' }, maxResults: { type: 'number' } },
      },
    },
  },
};
