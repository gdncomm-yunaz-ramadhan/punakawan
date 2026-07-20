import { renderClarificationComment } from './render.js';
import type { OpenQuestion } from './types.js';

/**
 * `'combined'` renders every question into a single comment body (one post
 * to the target system, several `###` sections) — useful when a Jira issue
 * or Confluence page has multiple open questions and the caller wants one
 * comment instead of spamming several. `'per-question'` renders each
 * question independently, returned as an array in input order, for callers
 * that want to post (and later track/resolve) one comment per question.
 * The caller picks the mode; this package has no opinion about which is
 * "right" for a given target, since that's a policy/UX decision belonging
 * to the calling adapter, not to this templating utility.
 */
export type BatchMode = 'combined' | 'per-question';

const COMBINED_HEADER = '## Open clarification questions';

/**
 * Renders a batch of open questions.
 *
 * - `mode: 'combined'` (default) returns a single string containing all
 *   questions, each rendered by {@link renderClarificationComment} and
 *   separated by a horizontal rule so the sections stay visually distinct.
 * - `mode: 'per-question'` returns one rendered string per question, in the
 *   same order as the input, with no cross-question separation since each
 *   entry is meant to be posted as its own comment.
 *
 * An empty `questions` array returns `''` for `'combined'` and `[]` for
 * `'per-question'`.
 */
export function renderClarificationComments(questions: readonly OpenQuestion[], mode?: 'combined'): string;
export function renderClarificationComments(questions: readonly OpenQuestion[], mode: 'per-question'): string[];
export function renderClarificationComments(
  questions: readonly OpenQuestion[],
  mode: BatchMode = 'combined',
): string | string[] {
  if (mode === 'per-question') {
    return questions.map((question) => renderClarificationComment(question));
  }

  if (questions.length === 0) return '';

  const rendered = questions.map((question) => renderClarificationComment(question));
  return [COMBINED_HEADER, '', rendered.join('\n\n---\n\n')].join('\n');
}
