import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createInterface } from 'node:readline';
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

/**
 * A cancelled call's error must be distinguishable from an ordinary
 * rejection (`data: {code: "cancelled"}`), so the Go worker
 * (internal/providerwrite) can tell a side-effecting write's remote call
 * may have already been sent before treating a timed-out attempt as
 * ambiguous rather than simply retryable.
 */
test('serveStdio reports a handler AbortSignal rejection with data.code "cancelled"', async () => {
  const script = `
    import { serveStdio } from ${JSON.stringify(stdioModulePath)};
    serveStdio({
      async op(params, signal) {
        return new Promise((resolve, reject) => {
          signal.addEventListener('abort', () => {
            const err = new Error('aborted');
            err.name = 'AbortError';
            reject(err);
          });
        });
      },
    });
  `;
  const child = spawn(process.execPath, ['--input-type=module', '-e', script]);
  const rl = createInterface({ input: child.stdout, terminal: false });

  const response = new Promise<{ error?: { code: number; message: string; data?: { code: string } } }>((resolve, reject) => {
    rl.on('line', (line) => {
      const trimmed = line.trim();
      if (!trimmed) return;
      const parsed = JSON.parse(trimmed) as { id?: number };
      if (parsed.id === 1) resolve(parsed as { error?: { code: number; message: string; data?: { code: string } } });
    });
    child.on('error', reject);
  });

  child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'op', params: {} })}\n`);
  // Give the handler a moment to register its in-flight AbortController
  // before cancelling it, so the "abort" listener above is actually
  // attached when the cancel notification arrives.
  await new Promise((resolve) => setTimeout(resolve, 50));
  child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', method: 'cancel', params: { id: 1 } })}\n`);

  const result = await response;
  child.kill();

  assert.equal(result.error?.data?.code, 'cancelled');
});
