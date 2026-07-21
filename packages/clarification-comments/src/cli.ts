import { readFileSync } from 'node:fs';
import { renderClarificationComment } from './render.js';
import type { OpenQuestion } from './types.js';

/**
 * Minimal CLI entry point so a non-Node caller (Punakawan's Go core, via
 * internal/tools.Supervisor) can reuse this package's rendering logic
 * without reimplementing it. Reads a single OpenQuestion as JSON from the
 * file at argv[2] and writes the rendered Markdown comment body to stdout.
 * Deliberately supports only this one shape: nothing in Punakawan currently
 * needs the batch renderer from outside a TypeScript process.
 */
function main(): void {
  const inputPath = process.argv[2];
  if (!inputPath) {
    process.stderr.write('usage: cli.js <path-to-open-question.json>\n');
    process.exit(1);
  }

  let question: OpenQuestion;
  try {
    question = JSON.parse(readFileSync(inputPath, 'utf8'));
  } catch (err) {
    process.stderr.write(`failed to read/parse ${inputPath}: ${(err as Error).message}\n`);
    process.exit(1);
    return;
  }

  process.stdout.write(renderClarificationComment(question));
}

main();
