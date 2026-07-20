import type { OpenQuestion } from './types.js';

/**
 * Mechanical templating only: this module formats the already-worded fields
 * that Semar produced in a `semar_synthesis.open_questions` entry (see
 * punakawan-go-typescript-detailed-plan.md §8.1, §9.2) into a Markdown
 * comment body suitable for posting to an external system (Jira, Confluence,
 * ...). It never composes new wording, judges content, or decides what to
 * ask — it only decides how to lay out what Semar already said. Punakawan's
 * architecture (§28) keeps Punakawan itself out of the LLM call path, so
 * this package must stay a pure string-formatting utility.
 *
 * Output format: Markdown. Neither Jira's `addCommentToJiraIssue` nor
 * Confluence's comment surface has a single documented plain-text
 * requirement available to this package, but Confluence's own
 * `getConfluencePage` tool already deals in `contentFormat: "markdown"`
 * elsewhere in this integration, and Markdown degrades gracefully to
 * readable plain text if a target renders it verbatim. Treating Markdown as
 * the lingua franca here is a judgment call, not a documented requirement —
 * see the task report for details.
 */

const SYSTEM_DRAFTED_NOTICE =
  '_This clarification was drafted automatically by Punakawan based on observed conflicts or gaps in the source material. It was not written by a human reviewer._';

/**
 * Renders a single open question into a Markdown comment body.
 *
 * Sections are emitted only when the corresponding field is present on the
 * input — this function never invents placeholder text or leaves literal
 * "undefined" in the output for absent optional fields.
 */
export function renderClarificationComment(question: OpenQuestion): string {
  const lines: string[] = [];

  lines.push('### Clarification needed', '');
  lines.push(SYSTEM_DRAFTED_NOTICE, '');

  if (question.question) {
    lines.push(`**Question:** ${question.question}`, '');
  }

  if (question.blocking) {
    lines.push('**This is blocking — work cannot proceed until this is answered.**', '');
  }

  if (question.why_it_matters) {
    lines.push('**Why it matters**', '', question.why_it_matters, '');
  }

  if (question.observed_conflict) {
    lines.push('**Observed conflict**', '', question.observed_conflict, '');
  }

  if (question.recommended_default) {
    lines.push('**Recommended default**', '', question.recommended_default, '');
  }

  if (question.impact_if_unanswered) {
    lines.push('**If left unanswered**', '', question.impact_if_unanswered, '');
  }

  // Trim a single trailing blank line so callers get a clean string without
  // depending on how many optional sections were emitted.
  while (lines.length > 0 && lines[lines.length - 1] === '') {
    lines.pop();
  }

  return lines.join('\n');
}
