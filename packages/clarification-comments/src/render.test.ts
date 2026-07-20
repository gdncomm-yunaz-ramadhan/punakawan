import assert from 'node:assert/strict';
import { test } from 'node:test';

import { renderClarificationComment } from './render.js';
import { renderClarificationComments } from './batch.js';
import type { OpenQuestion } from './types.js';

const FULL_QUESTION: OpenQuestion = {
  question: 'Should refunds for cancelled subscriptions be prorated or full?',
  why_it_matters:
    'Finance has flagged that inconsistent refund handling across regions caused a reconciliation discrepancy last quarter.',
  observed_conflict:
    'PAY-1842 says "always prorate" but the billing runbook in Confluence says "full refund within 14 days".',
  recommended_default: 'Prorate, matching the majority of existing customer contracts.',
  impact_if_unanswered: 'Refund logic ships with the runbook behavior, which may violate some contracts.',
  blocking: true,
  target: { system: 'jira', reference: 'PAY-1842' },
};

const MINIMAL_QUESTION: OpenQuestion = {
  question: 'What timezone should the nightly export job use?',
};

test('a fully-populated open question renders all expected sections', () => {
  const output = renderClarificationComment(FULL_QUESTION);

  assert.match(output, /Should refunds for cancelled subscriptions be prorated or full\?/);
  assert.match(output, /Why it matters/);
  assert.match(output, /Observed conflict/);
  assert.match(output, /Recommended default/);
  assert.match(output, /If left unanswered/);
  assert.match(output, /blocking/i);

  // Spot-check that Semar's exact wording survives verbatim, unparaphrased.
  assert.match(
    output,
    /Finance has flagged that inconsistent refund handling across regions caused a reconciliation discrepancy last quarter\./,
  );
  assert.match(
    output,
    /PAY-1842 says "always prorate" but the billing runbook in Confluence says "full refund within 14 days"\./,
  );
});

test('a minimal question renders sensibly with no empty or broken sections', () => {
  const output = renderClarificationComment(MINIMAL_QUESTION);

  assert.match(output, /What timezone should the nightly export job use\?/);
  assert.doesNotMatch(output, /undefined/);
  assert.doesNotMatch(output, /Why it matters/);
  assert.doesNotMatch(output, /Observed conflict/);
  assert.doesNotMatch(output, /Recommended default/);
  assert.doesNotMatch(output, /If left unanswered/);
  assert.doesNotMatch(output, /blocking/i);

  // No stray double-blank-line artifacts from skipped sections, and no
  // trailing blank line left dangling.
  assert.doesNotMatch(output, /\n{3,}/);
  assert.strictEqual(output, output.trimEnd());
});

test('an entirely empty question object renders without literal "undefined"', () => {
  const output = renderClarificationComment({});

  assert.doesNotMatch(output, /undefined/);
  assert.match(output, /drafted automatically by Punakawan/);
});

test('blocking: true vs blocking: false/absent produce visibly different framing', () => {
  const blocking = renderClarificationComment({ question: 'Q?', blocking: true });
  const notBlockingExplicit = renderClarificationComment({ question: 'Q?', blocking: false });
  const notBlockingAbsent = renderClarificationComment({ question: 'Q?' });

  assert.match(blocking, /blocking — work cannot proceed/i);
  assert.doesNotMatch(notBlockingExplicit, /blocking — work cannot proceed/i);
  assert.doesNotMatch(notBlockingAbsent, /blocking — work cannot proceed/i);

  assert.notStrictEqual(blocking, notBlockingExplicit);
  assert.strictEqual(notBlockingExplicit, notBlockingAbsent);
});

test('every rendered comment carries the system-drafted-not-human notice', () => {
  const output = renderClarificationComment(MINIMAL_QUESTION);
  assert.match(output, /drafted automatically by Punakawan/);
  assert.match(output, /not written by a human reviewer/);
});

test('the output never invents content absent from the input', () => {
  const output = renderClarificationComment(FULL_QUESTION);

  // The recommended default text should appear once, verbatim, and nothing
  // resembling a paraphrase-only alternate rendering should sneak in.
  const occurrences = output.split('Prorate, matching the majority of existing customer contracts.').length - 1;
  assert.strictEqual(occurrences, 1);
});

test('batch renderer: combined mode joins all questions into one comment', () => {
  const combined = renderClarificationComments([FULL_QUESTION, MINIMAL_QUESTION]);

  assert.match(combined, /Open clarification questions/);
  assert.match(combined, /Should refunds for cancelled subscriptions be prorated or full\?/);
  assert.match(combined, /What timezone should the nightly export job use\?/);
  // Separator between the two rendered questions.
  assert.match(combined, /\n\n---\n\n/);
  assert.strictEqual(combined.indexOf('---'), combined.lastIndexOf('---'));
});

test('batch renderer: combined mode on an empty list returns an empty string', () => {
  assert.strictEqual(renderClarificationComments([]), '');
});

test('batch renderer: per-question mode returns one entry per question, in order', () => {
  const perQuestion = renderClarificationComments([FULL_QUESTION, MINIMAL_QUESTION], 'per-question');

  assert.strictEqual(perQuestion.length, 2);
  assert.match(perQuestion[0], /Should refunds for cancelled subscriptions be prorated or full\?/);
  assert.match(perQuestion[1], /What timezone should the nightly export job use\?/);
  // Each entry stands alone: no combined header, no cross-question bleed.
  assert.doesNotMatch(perQuestion[0], /Open clarification questions/);
  assert.doesNotMatch(perQuestion[0], /nightly export job/);
});

test('batch renderer: per-question mode on an empty list returns an empty array', () => {
  assert.deepStrictEqual(renderClarificationComments([], 'per-question'), []);
});
