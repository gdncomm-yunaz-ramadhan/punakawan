import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

// Points at the compiled stdio.js next to this compiled test file (both land
// in dist/ via the same tsc build - see package.json's "test" script).
const stdioModulePath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'stdio.js');

/**
 * Regression test for the crash-diagnostics safety net installed by
 * serveStdio: an error that escapes the handler().then().catch() chain
 * (e.g. thrown from a timer or an event emitter, not rejected from the
 * handler's own Promise) must not crash the process silently - it should be
 * logged to stderr and exit deterministically, so a supervising process
 * (internal/adapters.Registry) can tell a crash happened and why, instead of
 * just seeing a closed pipe.
 */
test('serveStdio logs and exits deterministically on an exception outside the handler chain', async () => {
  const script = `
    import { serveStdio } from ${JSON.stringify(stdioModulePath)};
    serveStdio({});
    setImmediate(() => {
      throw new Error('boom-from-elsewhere');
    });
  `;
  const child = spawn(process.execPath, ['--input-type=module', '-e', script]);

  let stderr = '';
  child.stderr.on('data', (chunk) => {
    stderr += chunk.toString();
  });

  const [code] = (await once(child, 'exit')) as [number | null];

  assert.equal(code, 1, `expected exit code 1, got ${code}; stderr:\n${stderr}`);
  assert.match(stderr, /uncaught exception/);
  assert.match(stderr, /boom-from-elsewhere/);
});
