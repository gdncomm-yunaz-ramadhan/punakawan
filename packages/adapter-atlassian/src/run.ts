import { serveStdio } from '@punakawan/adapter-sdk';
import { createHandlers } from './adapter.js';

/**
 * Atlassian adapter process entry point: a real Punakawan adapter conforming
 * to protocol/adapter.schema.json (§5.4), spawned by Go-core over stdio per
 * punakawan-go-typescript-detailed-plan.md §5.1/§5.3. Mirrors
 * packages/adapter-sdk/src/prototypeAdapter.ts's initialize/execute/shutdown
 * wiring, backed by the real Atlassian MCP client instead of a fake sleep op.
 */
serveStdio(createHandlers());
