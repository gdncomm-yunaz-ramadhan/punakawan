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
  raw: Record<string, unknown>;
}

export interface NormalizedConfluencePage {
  source: NormalizedSource;
  id: string;
  title: string;
  spaceKey: string | undefined;
  contentFormat: string | undefined;
  content: string | undefined;
  raw: Record<string, unknown>;
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
function extractDescription(description: unknown): string | undefined {
  if (typeof description === 'string') return description;
  if (description && typeof description === 'object') {
    // Atlassian Document Format: best-effort plain text, not a full renderer.
    return JSON.stringify(description);
  }
  return undefined;
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
    raw,
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
    content,
    raw,
  };
}
