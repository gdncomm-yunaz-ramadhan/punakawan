import { serveStdio } from './stdio.js';
import { AdapterManifestSchema } from '@punakawan/schema-types';

/**
 * M0 prototype adapter: proves a Go-spawned TypeScript adapter process can
 * complete a JSON-RPC handshake and honor cancellation/timeout, per
 * punakawan-go-typescript-detailed-plan.md §22 Milestone 0 acceptance
 * criteria. Not a real adapter — exercised by the Go integration test only.
 */

/**
 * Fixed manifest returned by `capabilities` (§5.3), independent of whatever
 * a caller sends `initialize`, matching how real adapters (e.g.
 * packages/adapter-atlassian) validate their own compiled-in manifest
 * rather than trusting caller-supplied capability claims.
 */
const manifest = {
  id: 'prototype',
  name: 'Prototype Adapter',
  version: '0.1.0',
  protocol: 'punakawan.adapter/v1',
  runtime: 'node',
  provides: ['knowledge-source'],
  permissions: {
    network: { hosts: [] },
    filesystem: { read: [], write: [] },
    secrets: [],
  },
  operations: {
    sleep: { side_effect: false },
  },
};

serveStdio({
  async initialize(params) {
    const parsed = AdapterManifestSchema.parse(params);
    return { ok: true, id: parsed.id, version: parsed.version };
  },

  async capabilities() {
    return AdapterManifestSchema.parse(manifest);
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
