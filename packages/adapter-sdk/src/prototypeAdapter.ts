import { serveStdio } from './stdio.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';

/**
 * M0 prototype adapter: proves a Go-spawned TypeScript adapter process can
 * complete a JSON-RPC handshake and honor cancellation/timeout, per
 * punakawan-go-typescript-detailed-plan.md §22 Milestone 0 acceptance
 * criteria. Not a real adapter — exercised by the Go integration test only.
 */

serveStdio({
  async initialize(params) {
    const manifest = AdapterManifestSchema.parse(params);
    return { ok: true, id: manifest.id, version: manifest.version };
  },

  async execute(params, signal) {
    const { op, ms } = params as { op: string; ms?: number };
    if (op !== 'sleep') throw new Error(`Unsupported op: ${op}`);

    const durationMs = ms ?? 0;
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(resolve, durationMs);
      signal.addEventListener('abort', () => {
        clearTimeout(timer);
        reject(new Error('cancelled'));
      });
    });
    return { ok: true, slept_ms: durationMs };
  },

  async shutdown() {
    setImmediate(() => process.exit(0));
    return { ok: true };
  },
});
