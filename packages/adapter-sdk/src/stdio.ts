import { createInterface } from 'node:readline';

/**
 * Minimal JSON-RPC 2.0 server over newline-delimited JSON on stdio. Implements
 * the subset of punakawan-go-typescript-detailed-plan.md §5.1/§5.3 needed for
 * the M0 prototype: request/response dispatch, notifications, and
 * cancellation via a "cancel" notification carrying the target request id.
 */

export interface JsonRpcRequest {
  jsonrpc: '2.0';
  id?: string | number;
  method: string;
  params?: unknown;
}

export interface JsonRpcResponse {
  jsonrpc: '2.0';
  id: string | number | null;
  result?: unknown;
  error?: { code: number; message: string; data?: unknown };
}

export type Handler = (params: unknown, signal: AbortSignal) => Promise<unknown>;

const PARSE_ERROR = -32700;
const METHOD_NOT_FOUND = -32601;
const INTERNAL_ERROR = -32603;

export function serveStdio(handlers: Record<string, Handler>): void {
  const inflight = new Map<string | number, AbortController>();
  const rl = createInterface({ input: process.stdin, terminal: false });

  const write = (msg: JsonRpcResponse): void => {
    process.stdout.write(`${JSON.stringify(msg)}\n`);
  };

  rl.on('line', (line) => {
    const trimmed = line.trim();
    if (!trimmed) return;

    let req: JsonRpcRequest;
    try {
      req = JSON.parse(trimmed) as JsonRpcRequest;
    } catch {
      write({ jsonrpc: '2.0', id: null, error: { code: PARSE_ERROR, message: 'Parse error' } });
      return;
    }

    if (req.method === 'cancel') {
      const target = (req.params as { id?: string | number } | undefined)?.id;
      if (target !== undefined) inflight.get(target)?.abort();
      return;
    }

    const handler = handlers[req.method];
    if (!handler) {
      if (req.id !== undefined) {
        write({
          jsonrpc: '2.0',
          id: req.id,
          error: { code: METHOD_NOT_FOUND, message: `Method not found: ${req.method}` },
        });
      }
      return;
    }

    const controller = new AbortController();
    if (req.id !== undefined) inflight.set(req.id, controller);

    handler(req.params, controller.signal)
      .then((result) => {
        if (req.id !== undefined) write({ jsonrpc: '2.0', id: req.id, result });
      })
      .catch((err: unknown) => {
        if (req.id !== undefined) {
          write({
            jsonrpc: '2.0',
            id: req.id,
            error: { code: INTERNAL_ERROR, message: err instanceof Error ? err.message : String(err) },
          });
        }
      })
      .finally(() => {
        if (req.id !== undefined) inflight.delete(req.id);
      });
  });
}
