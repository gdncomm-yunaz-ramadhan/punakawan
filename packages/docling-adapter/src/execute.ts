import { DOCLING_SERVE_URL } from './manifest.js';
import { convertSource, type ConvertSourceInput, type FetchLike } from './doclingClient.js';
import { buildNormalizedConversion, type NormalizedConversion } from './normalize.js';

export interface DoclingConvertParams {
  url?: string;
  path?: string;
  toFormats?: string[];
  /** Optional pass-through of Docling Serve's own `do_ocr`/`force_ocr` options (see docs/usage.md). */
  doOcr?: boolean;
  forceOcr?: boolean;
}

function isDoclingConvertParams(value: unknown): value is DoclingConvertParams {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  if (v.url !== undefined && typeof v.url !== 'string') return false;
  if (v.path !== undefined && typeof v.path !== 'string') return false;
  if (v.toFormats !== undefined && !Array.isArray(v.toFormats)) return false;
  if (v.doOcr !== undefined && typeof v.doOcr !== 'boolean') return false;
  if (v.forceOcr !== undefined && typeof v.forceOcr !== 'boolean') return false;
  return true;
}

/**
 * Run the `docling.convert` operation: submit a URL or local file to Docling
 * Serve and return a normalized, source-preserving result. This is the
 * testable core of the `execute` handler, independent of JSON-RPC framing.
 */
export async function runConvert(
  params: unknown,
  fetchImpl: FetchLike = fetch,
  baseUrl: string = DOCLING_SERVE_URL,
): Promise<NormalizedConversion> {
  if (!isDoclingConvertParams(params)) {
    throw new Error('docling.convert params must be an object with optional url/path/toFormats fields');
  }
  if (!params.url && !params.path) {
    throw new Error('docling.convert requires at least one of "url" or "path"');
  }

  const input: ConvertSourceInput = {
    url: params.url,
    path: params.path,
    toFormats: params.toFormats,
    doOcr: params.doOcr,
    forceOcr: params.forceOcr,
  };
  const { rawBody, parsed } = await convertSource(baseUrl, input, fetchImpl);

  return buildNormalizedConversion(
    rawBody,
    parsed,
    { doOcr: params.doOcr, forceOcr: params.forceOcr },
    new Date().toISOString(),
  );
}
