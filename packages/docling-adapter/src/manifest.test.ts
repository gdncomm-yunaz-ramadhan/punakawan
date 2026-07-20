import { test } from 'node:test';
import assert from 'node:assert/strict';
import { AdapterManifestSchema } from '@punakawan/schema-types';
import { manifest, DOCLING_SERVE_URL } from './manifest.js';

/**
 * Mirrors the M0 prototype adapter's `initialize` handler
 * (packages/adapter-sdk/src/prototypeAdapter.ts), which parses its manifest
 * with AdapterManifestSchema and returns { ok, id, version }. The Go
 * integration test (test/integration/adapter_stdio_test.go) exercises this
 * same handshake end to end against a spawned adapter process.
 */

test('the docling adapter manifest validates against AdapterManifestSchema', () => {
  const parsed = AdapterManifestSchema.parse(manifest);
  assert.equal(parsed.id, 'docling');
  assert.equal(parsed.protocol, 'punakawan.adapter/v1');
  assert.equal(parsed.runtime, 'node');
  assert.deepEqual(parsed.provides, ['document-conversion']);
  assert.deepEqual(parsed.operations, { 'docling.convert': { side_effect: false } });
});

test('the manifest declares network access scoped to the configured Docling Serve host', () => {
  const expectedHost = new URL(DOCLING_SERVE_URL).host;
  assert.deepEqual(manifest.permissions.network.hosts, [expectedHost]);
});

test('the manifest declares no filesystem or secret permissions', () => {
  assert.deepEqual(manifest.permissions.filesystem, { read: [], write: [] });
  assert.deepEqual(manifest.permissions.secrets, []);
});

test('AdapterManifestSchema rejects a malformed manifest (missing required fields)', () => {
  assert.throws(() => AdapterManifestSchema.parse({ id: 'docling' }));
});

test('AdapterManifestSchema rejects a manifest with an invalid protocol literal', () => {
  assert.throws(() =>
    AdapterManifestSchema.parse({
      ...manifest,
      protocol: 'not-the-right-protocol',
    }),
  );
});

test('AdapterManifestSchema rejects a manifest with an invalid id pattern', () => {
  assert.throws(() =>
    AdapterManifestSchema.parse({
      ...manifest,
      id: 'Not_A_Valid_Id!',
    }),
  );
});
