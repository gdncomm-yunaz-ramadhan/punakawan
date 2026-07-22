import { serveStdio } from '@punakawan/adapter-sdk';
import { createHandlers } from './adapter.js';

/**
 * GitHub adapter process entry point: a real Punakawan adapter conforming to
 * protocol/adapter.schema.json (§5.4), spawned by Go-core over stdio per
 * punakawan-go-typescript-detailed-plan.md §5.1/§5.3. Mirrors
 * packages/adapter-atlassian/src/run.ts's wiring, backed by the direct
 * GitHub REST/GraphQL client.
 */
serveStdio(createHandlers());
