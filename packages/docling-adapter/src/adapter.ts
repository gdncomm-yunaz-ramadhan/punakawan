import { serveStdio } from '@punakawan/adapter-sdk';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import { manifest } from './manifest.js';
import { runConvert } from './execute.js';

/**
 * Docling adapter process, spawned by Go core over stdio per §5.1/§5.2/§5.3.
 * Submits files/URLs to a self-hosted Docling Serve instance and returns
 * structurally normalized, source-preserving sections — see
 * punakawan-go-typescript-detailed-plan.md §13.1 and §3.2 ("Docling result
 * normalization" is TS-owned).
 */

serveStdio({
  async initialize() {
    const validated = AdapterManifestSchema.parse(manifest);
    return { ok: true, id: validated.id, version: validated.version };
  },

  async capabilities() {
    return AdapterManifestSchema.parse(manifest);
  },

  async execute(params) {
    const { op, ...rest } = params as { op: string } & Record<string, unknown>;
    if (op !== 'docling.convert') {
      throw new Error(`Unsupported op: ${op}`);
    }
    return runConvert(rest);
  },

  async shutdown() {
    setImmediate(() => process.exit(0));
    return { ok: true };
  },
});
