import { readFile } from 'node:fs/promises';
import { basename } from 'node:path';
import type { DoclingConvertResponse } from './normalize.js';

/** A `fetch`-compatible function, injectable so tests can avoid real network calls. */
export type FetchLike = (url: string, init?: RequestInit) => Promise<Response>;

export interface ConvertSourceOptions {
  toFormats?: string[];
  doOcr?: boolean;
  forceOcr?: boolean;
}

export interface ConvertSourceInput {
  url?: string;
  path?: string;
  toFormats?: string[];
  doOcr?: boolean;
  forceOcr?: boolean;
}

type DoclingSource =
  | { kind: 'http'; url: string }
  | { kind: 'file'; base64_string: string; filename: string };

/**
 * Build the real `/v1/convert/source` request body per Docling Serve's v1
 * source shape: `{ options, sources: [{ kind, ... }] }`. Confirmed via
 * Context7 (`/docling-project/docling-serve`, docs/v1_migration.md).
 */
export async function buildConvertSourceRequest(
  input: ConvertSourceInput,
): Promise<{ options: { to_formats: string[]; do_ocr?: boolean; force_ocr?: boolean }; sources: DoclingSource[] }> {
  if (!input.url && !input.path) {
    throw new Error('docling.convert requires at least one of "url" or "path"');
  }

  const sources: DoclingSource[] = [];
  if (input.url) {
    sources.push({ kind: 'http', url: input.url });
  }
  if (input.path) {
    const buf = await readFile(input.path);
    sources.push({ kind: 'file', base64_string: buf.toString('base64'), filename: basename(input.path) });
  }

  const options: { to_formats: string[]; do_ocr?: boolean; force_ocr?: boolean } = {
    to_formats: input.toFormats ?? ['json'],
  };
  if (input.doOcr !== undefined) options.do_ocr = input.doOcr;
  if (input.forceOcr !== undefined) options.force_ocr = input.forceOcr;

  return { options, sources };
}

/**
 * Call Docling Serve's `POST /v1/convert/source`. Returns the raw response
 * body text (for content hashing) alongside the parsed JSON.
 */
export async function convertSource(
  baseUrl: string,
  input: ConvertSourceInput,
  fetchImpl: FetchLike = fetch,
): Promise<{ rawBody: string; parsed: DoclingConvertResponse }> {
  const body = await buildConvertSourceRequest(input);

  const res = await fetchImpl(`${baseUrl}/v1/convert/source`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });

  const rawBody = await res.text();
  if (!res.ok) {
    throw new Error(`Docling Serve returned ${res.status} ${res.statusText}: ${rawBody.slice(0, 500)}`);
  }

  let parsed: DoclingConvertResponse;
  try {
    parsed = JSON.parse(rawBody) as DoclingConvertResponse;
  } catch (err) {
    throw new Error(`Docling Serve response was not valid JSON: ${err instanceof Error ? err.message : String(err)}`);
  }

  return { rawBody, parsed };
}
