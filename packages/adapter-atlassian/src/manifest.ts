import type { AdapterManifest } from '@punakawan/schema-types';

/**
 * Manifest for the Atlassian adapter. Declares identity, capabilities, and
 * permissions per punakawan-go-typescript-detailed-plan.md §5.4/§13.2.
 *
 * Read operations are side-effect free. A write operation declares
 * `side_effect: true`, which routes it through validation/outbox/audit on
 * the Go core side — it never means the write is gated on user
 * confirmation; execution proceeds once authorized by policy.
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
    'atlassian.searchJira': { side_effect: false },
    'atlassian.searchConfluence': { side_effect: false },
    'atlassian.getJiraIssue': { side_effect: false },
    'atlassian.getJiraComments': { side_effect: false },
    'atlassian.getJiraRemoteLinks': { side_effect: false },
    'atlassian.getJiraEpic': { side_effect: false },
    'atlassian.listJiraAttachments': { side_effect: false },
    'atlassian.downloadJiraAttachment': { side_effect: true },
    'atlassian.uploadJiraAttachment': { side_effect: true },
    'atlassian.deleteJiraAttachment': { side_effect: true },
    'atlassian.getConfluencePage': { side_effect: false },
    'atlassian.addJiraComment': { side_effect: true },
    'atlassian.getTransitionsForJiraIssue': { side_effect: false },
    'atlassian.transitionJiraIssue': { side_effect: true },
    'atlassian.editJiraIssueFields': { side_effect: true },
    'atlassian.editJiraIssue': { side_effect: true },
    'atlassian.addWorklog': { side_effect: true },
    'atlassian.getIssueTypeFieldMeta': { side_effect: false },
    'atlassian.createJiraIssue': { side_effect: true },
    'atlassian.createJiraSubtask': { side_effect: true },
    'atlassian.searchJiraUsers': { side_effect: false },
    'atlassian.createIssueLink': { side_effect: true },
    'atlassian.listJiraBoards': { side_effect: false },
    'atlassian.listJiraSprints': { side_effect: false },
  },
};
