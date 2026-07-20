import { createHash } from 'node:crypto';

/**
 * Normalization of Docling Serve's `/v1/convert/source` response into
 * source-preserving sections, per punakawan-go-typescript-detailed-plan.md
 * §13.1's Punakawan responsibilities ("Split into source-preserving
 * sections", "Normalize tables, headings, and metadata", "Preserve page and
 * section provenance", "Track parser version and content hash", "Flag
 * uncertain extraction").
 *
 * Deliberately scoped to *structural* normalization. Candidate requirement,
 * constraint, claim, and decision extraction is Semar's job (a reasoning
 * role invoked over MCP, per §28) — not something this adapter attempts via
 * string-matching heuristics.
 */

/** Docling Serve conversion status values, per its documented response shape. */
export type DoclingStatus = 'success' | 'partial_success' | 'skipped' | 'failure';

/** A single Docling `TextItem`/`SectionHeaderItem`-like entry inside `texts`. */
export interface DoclingTextItem {
  label?: string;
  text?: string;
  prov?: Array<{ page_no?: number; bbox?: unknown; charspan?: unknown }>;
  [key: string]: unknown;
}

/** A single Docling `TableItem`-like entry inside `tables`. */
export interface DoclingTableItem {
  label?: string;
  data?: unknown;
  prov?: Array<{ page_no?: number; bbox?: unknown; charspan?: unknown }>;
  [key: string]: unknown;
}

/**
 * Docling's own document model (`DoclingDocument.export_to_dict()`), as
 * found in Docling Serve's `document.json_content`. Only the fields this
 * adapter confidently normalizes are typed; the rest of the real schema
 * (pictures, key_value_items, groups, furniture, page metadata, etc.) is
 * intentionally left untouched rather than guessed at. See
 * https://github.com/docling-project/docling (docs/concepts/docling_document.md)
 * for the full, real schema.
 */
export interface DoclingJsonContent {
  schema_name?: string;
  version?: string;
  texts?: DoclingTextItem[];
  tables?: DoclingTableItem[];
  [key: string]: unknown;
}

export interface DoclingDocumentPayload {
  md_content?: string | null;
  json_content?: DoclingJsonContent | null;
  html_content?: string | null;
  text_content?: string | null;
  doctags_content?: string | null;
}

/** The real Docling Serve `/v1/convert/source` response shape. */
export interface DoclingConvertResponse {
  document: DoclingDocumentPayload;
  status: DoclingStatus;
  processing_time?: number;
  timings?: Record<string, unknown>;
  errors?: unknown[];
}

/** Page/location provenance for a normalized section, when Docling provides it. */
export interface SectionProvenance {
  pageNo?: number;
  bbox?: unknown;
  charspan?: unknown;
}

/** A structurally normalized, source-preserving section of the converted document. */
export interface NormalizedSection {
  kind: 'heading' | 'text' | 'table';
  /** Docling's own item label (e.g. "section_header", "paragraph", "table"), if present. */
  label?: string;
  /** Section/heading text, for heading and text items. */
  text?: string;
  /** Structured table data, preserved as-is rather than flattened to prose. */
  table?: unknown;
  provenance: SectionProvenance[];
}

/**
 * Provenance for the whole conversion: content hash and retrieval time.
 *
 * `parserVersion` is deliberately omitted: Docling Serve's documented
 * `/v1/convert/source` response does not expose a parser/converter version
 * field, and `json_content.schema_name`/`.version` (confirmed via Context7
 * for `/docling-project/docling-serve`) describe the *DoclingDocument
 * schema* ("DoclingDocument"), not the Docling parser build that produced
 * it — using it as a parser version would be a guess, not a documented
 * fact. If Docling Serve starts exposing one (e.g. in `timings` or a
 * dedicated field), thread it through here instead of inventing a value.
 */
export interface ConversionProvenance {
  contentHash: string;
  retrievedAt: string;
}

/** This adapter's normalized, JSON-serializable output for `docling.convert`. */
export interface NormalizedConversion {
  status: DoclingStatus;
  uncertain: boolean;
  uncertaintyReasons: string[];
  sections: NormalizedSection[];
  markdown?: string;
  provenance: ConversionProvenance;
  errors: unknown[];
}

/** `sha256:<hex>` content hash, matching protocol/knowledge.schema.json's `source.content_hash` pattern. */
export function contentHashOf(rawBody: string): string {
  return `sha256:${createHash('sha256').update(rawBody).digest('hex')}`;
}

const HEADING_LABELS = new Set(['section_header', 'title', 'page_header']);

function toProvenance(prov: DoclingTextItem['prov']): SectionProvenance[] {
  if (!Array.isArray(prov)) return [];
  return prov.map((p) => ({ pageNo: p?.page_no, bbox: p?.bbox, charspan: p?.charspan }));
}

/**
 * Normalize Docling's `json_content` document model into source-preserving
 * sections. Only `texts` (headings/paragraphs) and `tables` are confidently
 * normalized here; `pictures`, `key_value_items`, `groups`, and the
 * `body`/`furniture` tree structure are left unnormalized rather than
 * guessed at (see module doc comment).
 */
export function normalizeJsonContent(json: DoclingJsonContent | null | undefined): NormalizedSection[] {
  if (!json) return [];
  const sections: NormalizedSection[] = [];

  for (const item of json.texts ?? []) {
    if (typeof item?.text !== 'string') continue;
    sections.push({
      kind: HEADING_LABELS.has(item.label ?? '') ? 'heading' : 'text',
      label: item.label,
      text: item.text,
      provenance: toProvenance(item.prov),
    });
  }

  for (const item of json.tables ?? []) {
    sections.push({
      kind: 'table',
      label: item.label,
      table: item.data,
      provenance: toProvenance(item.prov),
    });
  }

  return sections;
}

export interface UncertaintyInput {
  status: DoclingStatus;
  errors?: unknown[];
  doOcr?: boolean;
  forceOcr?: boolean;
}

/**
 * Flags uncertain extraction per §13.1 ("Flag uncertain extraction"): a
 * partial or failed status, any reported errors, or OCR having been used to
 * derive text (bitmap-derived text is inherently less reliable than native
 * text extraction) all count as reasons not to treat the result as clean.
 */
export function assessUncertainty(input: UncertaintyInput): { uncertain: boolean; reasons: string[] } {
  const reasons: string[] = [];
  if (input.status === 'partial_success' || input.status === 'failure') {
    reasons.push(`conversion status was "${input.status}"`);
  }
  if (input.errors && input.errors.length > 0) {
    reasons.push(`conversion reported ${input.errors.length} error(s)`);
  }
  if (input.forceOcr) {
    reasons.push('force_ocr was enabled (text forcibly replaced with OCR output)');
  } else if (input.doOcr) {
    reasons.push('do_ocr was enabled (bitmap content may have been OCR-derived)');
  }
  return { uncertain: reasons.length > 0, reasons };
}

/**
 * Build the full normalized conversion result from a raw Docling Serve
 * response body and the request options that produced it.
 */
export function buildNormalizedConversion(
  rawBody: string,
  parsed: DoclingConvertResponse,
  requestOptions: { doOcr?: boolean; forceOcr?: boolean },
  retrievedAt: string,
): NormalizedConversion {
  const { uncertain, reasons } = assessUncertainty({
    status: parsed.status,
    errors: parsed.errors,
    doOcr: requestOptions.doOcr,
    forceOcr: requestOptions.forceOcr,
  });

  return {
    status: parsed.status,
    uncertain,
    uncertaintyReasons: reasons,
    sections: normalizeJsonContent(parsed.document?.json_content),
    markdown: parsed.document?.md_content ?? undefined,
    provenance: {
      contentHash: contentHashOf(rawBody),
      retrievedAt,
    },
    errors: parsed.errors ?? [],
  };
}
