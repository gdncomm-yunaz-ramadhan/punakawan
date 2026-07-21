import type { AdapterManifest } from '@punakawan/schema-types';

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
    // '*.atlassian.net' covers the one-time, unauthenticated cloudId lookup
    // (resolveCloudId in mcpClient.ts) against the operator-configured
    // ATLASSIAN_HOST - the real hostname varies per Atlassian site, so it
    // cannot be declared as a single literal value the way mcp.atlassian.com
    // can.
    network: { hosts: ['mcp.atlassian.com', '*.atlassian.net'] },
    filesystem: { read: [], write: [] },
    secrets: ['ATLASSIAN_MCP_TOKEN', 'ATLASSIAN_EMAIL'],
  },
  operations: {
    'atlassian.searchJira': { side_effect: false },
    'atlassian.searchConfluence': { side_effect: false },
    'atlassian.getJiraIssue': { side_effect: false },
    'atlassian.getConfluencePage': { side_effect: false },
    'atlassian.addJiraComment': { side_effect: true, approval: 'required' },
    'atlassian.getTransitionsForJiraIssue': { side_effect: false },
    'atlassian.transitionJiraIssue': { side_effect: true, approval: 'required' },
    'atlassian.editJiraIssueFields': { side_effect: true, approval: 'required' },
    'atlassian.addWorklog': { side_effect: true, approval: 'required' },
    'atlassian.getIssueTypeFieldMeta': { side_effect: false },
    'atlassian.createJiraSubtask': { side_effect: true, approval: 'required' },
  },
};
