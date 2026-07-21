/**
 * Normalization of raw Atlassian REST results into shapes that map
 * cleanly onto `protocol.KnowledgeRecord`'s `source` sub-shape (provider,
 * external_id, version, uri, retrieved_at) without being full
 * KnowledgeRecords themselves — per
 * punakawan-go-typescript-detailed-plan.md §13.2 ("Normalize Jira issues and
 * Confluence pages", "Preserve external IDs and versions").
 *
 * Deliberately does NOT attempt candidate-requirement/claim extraction from
 * issue or page text — per §28, Punakawan itself never reasons about
 * content; that interpretation is a role's job (Semar), not this adapter's.
 */

/** Mirrors KnowledgeRecordSchema's `source` sub-shape from @punakawan/schema-types. */
export interface NormalizedSource {
  provider: 'jira' | 'confluence';
  external_id: string;
  version?: string | number;
  uri: string;
  retrieved_at: string;
}

export interface NormalizedJiraIssue {
  source: NormalizedSource;
  key: string;
  summary: string;
  description: string | undefined;
  status: string | undefined;
  issueType?: string;
  priority?: string;
  assignee?: string;
  labels?: string[];
  parent?: { key: string; summary?: string };
  subtasks?: { key: string; summary?: string; status?: string }[];
  timeTracking?: Record<string, unknown>;
  customFields?: Record<string, string | number | boolean | null | (string | number | boolean)[]>;
}

export interface NormalizedJiraSearchIssue {
  key: string;
  summary: string;
  status?: string;
  issueType?: string;
  priority?: string;
  assignee?: string;
  parent?: { key: string; summary?: string };
  updated?: string;
  customFields?: NormalizedJiraIssue['customFields'];
}

export interface NormalizedConfluencePage {
  source: NormalizedSource;
  id: string;
  title: string;
  spaceKey: string | undefined;
  contentFormat: string | undefined;
  content: string | undefined;
}

function nowIso(): string {
  return new Date().toISOString();
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
}

/**
 * Extracts a plain-text/ADF-ish description out of Jira's various possible
 * description shapes (plain string, or an Atlassian Document Format object)
 * without attempting semantic interpretation.
 */
function compactText(text: string): string | undefined {
  const compact = text.replace(/\s+/g, ' ').trim();
  return compact || undefined;
}

function collectAdfText(value: unknown, text: string[]): void {
  if (!value || typeof value !== 'object') return;
  const node = value as Record<string, unknown>;
  if (typeof node.text === 'string') text.push(node.text);
  if (Array.isArray(node.content)) {
    for (const child of node.content) collectAdfText(child, text);
  }
}

function extractDescription(description: unknown): string | undefined {
  if (typeof description === 'string') return compactText(description);
  const text: string[] = [];
  collectAdfText(description, text);
  return compactText(text.join(' '));
}

function compactAssignee(value: unknown): string | undefined {
  const identity = asRecord(value);
  return asString(identity.displayName) ?? asString(identity.accountId);
}

function compactParent(value: unknown): { key: string; summary?: string } | undefined {
  const parent = asRecord(value);
  const key = asString(parent.key);
  if (!key) return undefined;
  return { key, summary: asString(asRecord(parent.fields).summary) };
}

function compactSubtasks(value: unknown): { key: string; summary?: string; status?: string }[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const result = value.flatMap((entry) => {
    const subtask = asRecord(entry);
    const key = asString(subtask.key);
    if (!key) return [];
    const fields = asRecord(subtask.fields);
    return [{ key, summary: asString(fields.summary), status: asString(asRecord(fields.status).name) }];
  });
  return result.length ? result : undefined;
}

function compactCustomFields(fields: Record<string, unknown>): NormalizedJiraIssue['customFields'] {
  const result: NonNullable<NormalizedJiraIssue['customFields']> = {};
  for (const [key, value] of Object.entries(fields)) {
    if (!key.startsWith('customfield_')) continue;
    if (value === null || ['string', 'number', 'boolean'].includes(typeof value)) {
      result[key] = value as string | number | boolean | null;
    } else if (Array.isArray(value) && value.length <= 50 && value.every((item) => ['string', 'number', 'boolean'].includes(typeof item))) {
      result[key] = value as (string | number | boolean)[];
    }
  }
  return Object.keys(result).length ? result : undefined;
}

/**
 * Normalizes a Jira REST issue response into a stable shape.
 * The `version` field always reflects the live, just-fetched value from
 * Atlassian, so a caller can compare it against a previously-stored version
 * to detect staleness (§13.2) — this module does not perform that
 * comparison itself.
 */
export function normalizeJiraIssue(raw: Record<string, unknown>, cloudId: string): NormalizedJiraIssue {
  const key = asString(raw.key) ?? asString(raw.id);
  if (!key) {
    throw new Error('Jira issue result is missing both "key" and "id"; cannot normalize.');
  }

  const fields = asRecord(raw.fields);
  const summary = asString(fields.summary) ?? '';
  const description = extractDescription(fields.description);
  const status = asString(asRecord(fields.status).name);
  const updated = asString(fields.updated);
  const version = updated ?? (typeof raw.version === 'number' || typeof raw.version === 'string' ? raw.version : undefined);
  const labels = Array.isArray(fields.labels) ? fields.labels.filter((label): label is string => typeof label === 'string') : undefined;
  const timeTracking = Object.keys(asRecord(fields.timetracking)).length ? asRecord(fields.timetracking) : undefined;

  return {
    source: {
      provider: 'jira',
      external_id: key,
      version,
      uri: `jira://${cloudId}/${key}`,
      retrieved_at: nowIso(),
    },
    key,
    summary,
    description,
    status,
    issueType: asString(asRecord(fields.issuetype).name),
    priority: asString(asRecord(fields.priority).name),
    assignee: compactAssignee(fields.assignee),
    labels: labels?.length ? labels : undefined,
    parent: compactParent(fields.parent),
    subtasks: compactSubtasks(fields.subtasks),
    timeTracking,
    customFields: compactCustomFields(fields),
  };
}

/** Search results omit per-row provenance and descriptions to stay cheap. */
export function normalizeJiraSearchIssue(raw: Record<string, unknown>): NormalizedJiraSearchIssue {
  const key = asString(raw.key) ?? asString(raw.id);
  if (!key) throw new Error('Jira search result is missing both "key" and "id"; cannot normalize.');
  const fields = asRecord(raw.fields);
  return {
    key,
    summary: asString(fields.summary) ?? '',
    status: asString(asRecord(fields.status).name),
    issueType: asString(asRecord(fields.issuetype).name),
    priority: asString(asRecord(fields.priority).name),
    assignee: compactAssignee(fields.assignee),
    parent: compactParent(fields.parent),
    updated: asString(fields.updated),
    customFields: compactCustomFields(fields),
  };
}

/**
 * Normalizes a raw `getConfluencePage` tool result into a stable shape.
 * `version.number` (Confluence's native version counter) is surfaced
 * honestly as the live version for staleness comparison by the caller.
 */
export function normalizeConfluencePage(raw: Record<string, unknown>, cloudId: string): NormalizedConfluencePage {
  const id = asString(raw.id);
  if (!id) {
    throw new Error('Confluence page result is missing "id"; cannot normalize.');
  }

  const title = asString(raw.title) ?? '';
  const versionBlock = asRecord(raw.version);
  const version = typeof versionBlock.number === 'number' ? versionBlock.number : asString(versionBlock.number);
  const spaceKey = asString(raw.spaceKey) ?? asString(asRecord(raw.space).key);
  const bodyBlock = asRecord(raw.body);
  const contentFormat = asString(raw.contentFormat);
  const content =
    asString(raw.content) ??
    (contentFormat ? asString(asRecord(bodyBlock[contentFormat]).value) : undefined);

  return {
    source: {
      provider: 'confluence',
      external_id: id,
      version,
      uri: `confluence://${cloudId}/${id}`,
      retrieved_at: nowIso(),
    },
    id,
    title,
    spaceKey,
    contentFormat,
    content: content ? compactText(content) : undefined,
  };
}
