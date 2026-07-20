import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { buildConvertSourceRequest, convertSource } from './doclingClient.js';
import type { FetchLike } from './doclingClient.js';
import { successResponse, jsonResponse } from './fixtures.js';

test('buildConvertSourceRequest builds an http source for a url input', async () => {
  const req = await buildConvertSourceRequest({ url: 'https://example.com/doc.pdf' });
  assert.deepEqual(req.sources, [{ kind: 'http', url: 'https://example.com/doc.pdf' }]);
  assert.deepEqual(req.options, { to_formats: ['json'] });
});

test('buildConvertSourceRequest reads a local file and base64-encodes it as a file source', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'docling-adapter-test-'));
  const filePath = join(dir, 'doc.pdf');
  await writeFile(filePath, 'fake pdf bytes');

  try {
    const req = await buildConvertSourceRequest({ path: filePath });
    assert.equal(req.sources.length, 1);
    const source = req.sources[0] as { kind: string; base64_string: string; filename: string };
    assert.equal(source.kind, 'file');
    assert.equal(source.filename, 'doc.pdf');
    assert.equal(Buffer.from(source.base64_string, 'base64').toString('utf8'), 'fake pdf bytes');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('buildConvertSourceRequest rejects input missing both url and path', async () => {
  await assert.rejects(() => buildConvertSourceRequest({}), /requires at least one of "url" or "path"/);
});

test('convertSource throws with a descriptive error on a non-JSON response body', async () => {
  const fakeFetch: FetchLike = async () =>
    new Response('not json', { status: 200, headers: { 'Content-Type': 'text/plain' } });

  await assert.rejects(
    () => convertSource('http://localhost:5001', { url: 'https://example.com/doc.pdf' }, fakeFetch),
    /not valid JSON/,
  );
});

test('convertSource returns both the raw body text and parsed JSON for a successful call', async () => {
  const fakeFetch: FetchLike = async () => jsonResponse(successResponse);
  const { rawBody, parsed } = await convertSource(
    'http://localhost:5001',
    { url: 'https://example.com/doc.pdf' },
    fakeFetch,
  );

  assert.equal(JSON.parse(rawBody).status, 'success');
  assert.equal(parsed.status, 'success');
});
