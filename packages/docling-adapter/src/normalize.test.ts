import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  assessUncertainty,
  buildNormalizedConversion,
  contentHashOf,
  normalizeJsonContent,
} from './normalize.js';
import { successResponse, partialSuccessResponse } from './fixtures.js';

test('contentHashOf produces a sha256:<hex> string matching protocol/knowledge.schema.json pattern', () => {
  const hash = contentHashOf('hello world');
  assert.match(hash, /^sha256:[0-9a-f]{64}$/);
  // Deterministic for identical input.
  assert.equal(hash, contentHashOf('hello world'));
  // Different for different input.
  assert.notEqual(hash, contentHashOf('hello world!'));
});

test('normalizeJsonContent splits texts into heading/text sections and preserves tables as structured data', () => {
  const sections = normalizeJsonContent(successResponse.document.json_content);

  assert.equal(sections.length, 3);

  const heading = sections[0];
  assert.equal(heading.kind, 'heading');
  assert.equal(heading.text, 'Title');
  assert.equal(heading.provenance[0]?.pageNo, 1);

  const paragraph = sections[1];
  assert.equal(paragraph.kind, 'text');
  assert.equal(paragraph.text, 'Some paragraph text.');

  const table = sections[2];
  assert.equal(table.kind, 'table');
  // Table data is preserved as structured data, not flattened to prose.
  assert.equal(typeof table.table, 'object');
  assert.deepEqual(table.table, successResponse.document.json_content?.tables?.[0]?.data);
  assert.equal(table.provenance[0]?.pageNo, 1);
});

test('normalizeJsonContent tolerates missing json_content', () => {
  assert.deepEqual(normalizeJsonContent(undefined), []);
  assert.deepEqual(normalizeJsonContent(null), []);
});

test('assessUncertainty is clean for a successful, error-free, non-OCR conversion', () => {
  const { uncertain, reasons } = assessUncertainty({ status: 'success', errors: [] });
  assert.equal(uncertain, false);
  assert.deepEqual(reasons, []);
});

test('assessUncertainty flags partial_success status', () => {
  const { uncertain, reasons } = assessUncertainty({ status: 'partial_success' });
  assert.equal(uncertain, true);
  assert.ok(reasons.some((r) => r.includes('partial_success')));
});

test('assessUncertainty flags non-empty errors', () => {
  const { uncertain, reasons } = assessUncertainty({ status: 'success', errors: ['boom'] });
  assert.equal(uncertain, true);
  assert.ok(reasons.some((r) => r.includes('1 error')));
});

test('assessUncertainty flags do_ocr and force_ocr usage', () => {
  const ocr = assessUncertainty({ status: 'success', doOcr: true });
  assert.equal(ocr.uncertain, true);
  assert.ok(ocr.reasons.some((r) => r.includes('do_ocr')));

  const forced = assessUncertainty({ status: 'success', forceOcr: true });
  assert.equal(forced.uncertain, true);
  assert.ok(forced.reasons.some((r) => r.includes('force_ocr')));
});

test('buildNormalizedConversion produces a clean, non-uncertain result for a successful response', () => {
  const raw = JSON.stringify(successResponse);
  const result = buildNormalizedConversion(raw, successResponse, {}, '2026-07-20T00:00:00.000Z');

  assert.equal(result.status, 'success');
  assert.equal(result.uncertain, false);
  assert.deepEqual(result.uncertaintyReasons, []);
  assert.equal(result.sections.length, 3);
  assert.equal(result.provenance.contentHash, contentHashOf(raw));
  assert.equal(result.provenance.retrievedAt, '2026-07-20T00:00:00.000Z');
  assert.equal(result.markdown, successResponse.document.md_content);
});

test('buildNormalizedConversion flags a partial_success/errors-present response as uncertain rather than silently accepting it', () => {
  const raw = JSON.stringify(partialSuccessResponse);
  const result = buildNormalizedConversion(raw, partialSuccessResponse, {}, '2026-07-20T00:00:00.000Z');

  assert.equal(result.status, 'partial_success');
  assert.equal(result.uncertain, true);
  assert.ok(result.uncertaintyReasons.length > 0);
  assert.deepEqual(result.errors, partialSuccessResponse.errors);
});
