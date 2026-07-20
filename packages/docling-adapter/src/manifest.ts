import type { AdapterManifest } from '@punakawan/schema-types';

/**
 * Docling Serve base URL. Defaults to Docling Serve's own documented default
 * port (5001). See punakawan-go-typescript-detailed-plan.md §13.1.
 */
export const DOCLING_SERVE_URL = process.env.DOCLING_SERVE_URL ?? 'http://localhost:5001';

function hostOf(url: string): string {
  return new URL(url).host;
}

/**
 * Manifest for the Docling adapter: submits files/URLs to a self-hosted
 * Docling Serve instance and normalizes the result into source-preserving
 * sections. Conversion is read-only (fetch + transform), so `docling.convert`
 * is declared with `side_effect: false` per §5.4.
 */
export const manifest: AdapterManifest = {
  id: 'docling',
  name: 'Docling Adapter',
  version: '0.1.0',
  protocol: 'punakawan.adapter/v1',
  runtime: 'node',
  provides: ['document-conversion'],
  permissions: {
    network: {
      hosts: [hostOf(DOCLING_SERVE_URL)],
    },
    filesystem: {
      read: [],
      write: [],
    },
    secrets: [],
  },
  operations: {
    'docling.convert': { side_effect: false },
  },
};
