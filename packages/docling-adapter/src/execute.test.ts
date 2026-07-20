import { test } from 'node:test';
import assert from 'node:assert/strict';
import { runConvert } from './execute.js';
import type { FetchLike } from './doclingClient.js';
import { successResponse, partialSuccessResponse, jsonResponse } from './fixtures.js';

test('runConvert calls the real /v1/convert/source endpoint with the documented sources/kind shape and returns normalized sections', async () => {
  let capturedUrl: string | undefined;
  let capturedBody: unknown;

  const fakeFetch: FetchLike = async (url, init) => {
    capturedUrl = url;
    capturedBody = JSON.parse(String(init?.body));
    return jsonResponse(successResponse);
  };

  const result = await runConvert({ url: 'https://example.com/doc.pdf' }, fakeFetch, 'http://localhost:5001');

  assert.equal(capturedUrl, 'http://localhost:5001/v1/convert/source');
  assert.deepEqual(capturedBody, {
    options: { to_formats: ['json'] },
    sources: [{ kind: 'http', url: 'https://example.com/doc.pdf' }],
  });

  assert.equal(result.status, 'success');
  assert.equal(result.uncertain, false);
  assert.equal(result.sections.length, 3);
  assert.match(result.provenance.contentHash, /^sha256:[0-9a-f]{64}$/);
});

test('runConvert defaults to_formats to ["json"] when not specified', async () => {
  let capturedBody: any;
  const fakeFetch: FetchLike = async (_url, init) => {
    capturedBody = JSON.parse(String(init?.body));
    return jsonResponse(successResponse);
  };

  await runConvert({ url: 'https://example.com/doc.pdf' }, fakeFetch, 'http://localhost:5001');
  assert.deepEqual(capturedBody.options.to_formats, ['json']);
});

test('runConvert honors an explicit toFormats list', async () => {
  let capturedBody: any;
  const fakeFetch: FetchLike = async (_url, init) => {
    capturedBody = JSON.parse(String(init?.body));
    return jsonResponse(successResponse);
  };

  await runConvert(
    { url: 'https://example.com/doc.pdf', toFormats: ['json', 'md'] },
    fakeFetch,
    'http://localhost:5001',
  );
  assert.deepEqual(capturedBody.options.to_formats, ['json', 'md']);
});

test('runConvert flags a partial_success response as uncertain', async () => {
  const fakeFetch: FetchLike = async () => jsonResponse(partialSuccessResponse);

  const result = await runConvert({ url: 'https://example.com/doc.pdf' }, fakeFetch, 'http://localhost:5001');

  assert.equal(result.status, 'partial_success');
  assert.equal(result.uncertain, true);
  assert.ok(result.uncertaintyReasons.length > 0);
});

test('runConvert flags do_ocr usage as uncertain and forwards it to the request', async () => {
  let capturedBody: any;
  const fakeFetch: FetchLike = async (_url, init) => {
    capturedBody = JSON.parse(String(init?.body));
    return jsonResponse(successResponse);
  };

  const result = await runConvert(
    { url: 'https://example.com/doc.pdf', doOcr: true },
    fakeFetch,
    'http://localhost:5001',
  );

  assert.equal(capturedBody.options.do_ocr, true);
  assert.equal(result.uncertain, true);
  assert.ok(result.uncertaintyReasons.some((r: string) => r.includes('do_ocr')));
});

test('runConvert rejects params missing both url and path', async () => {
  const fakeFetch: FetchLike = async () => jsonResponse(successResponse);
  await assert.rejects(
    () => runConvert({}, fakeFetch, 'http://localhost:5001'),
    /requires at least one of "url" or "path"/,
  );
});

test('runConvert rejects malformed params', async () => {
  const fakeFetch: FetchLike = async () => jsonResponse(successResponse);
  await assert.rejects(
    () => runConvert({ url: 123 }, fakeFetch, 'http://localhost:5001'),
    /params must be an object/,
  );
});

test('runConvert surfaces a non-2xx Docling Serve response as a rejected promise', async () => {
  const fakeFetch: FetchLike = async () => jsonResponse({ detail: 'boom' }, { status: 500 });
  await assert.rejects(
    () => runConvert({ url: 'https://example.com/doc.pdf' }, fakeFetch, 'http://localhost:5001'),
    /Docling Serve returned 500/,
  );
});
