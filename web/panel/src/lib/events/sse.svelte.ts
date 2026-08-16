// Live-update client for GET /api/v1/events (§12). The browser's native
// EventSource already reconnects automatically and resends the last
// event's id via the Last-Event-ID header, so this module only needs to
// track connection status and fan out incoming events to whichever views
// are currently mounted - per §12's "never assume an SSE event alone
// contains a complete canonical object," a listener's job is to refetch
// its own summary, not to reconstruct state from the event payload.

export type ConnectionStatus = "connecting" | "open" | "error";

let status = $state<ConnectionStatus>("connecting");
const listeners = new Set<(evt: MessageEvent) => void>();

export function getConnectionStatus(): ConnectionStatus {
  return status;
}

export function onPanelEvent(callback: (evt: MessageEvent) => void): () => void {
  listeners.add(callback);
  return () => listeners.delete(callback);
}

// Structured shape of the JSON every frame's `data:` line carries
// (protocol.PanelEvent), for callers that need to filter by entity
// rather than just refetching unconditionally on any event.
export interface PanelEventEnvelope {
  id: string;
  type: string;
  entity_id?: string;
  payload?: Record<string, unknown>;
}

export function parsePanelEvent(evt: MessageEvent): PanelEventEnvelope | null {
  try {
    return JSON.parse(evt.data) as PanelEventEnvelope;
  } catch {
    return null;
  }
}

// internal/panel/events/sse.go's writeEvent always sets an explicit
// `event: <type>` line - never the default/unnamed "message" type - so
// EventSource.onmessage (which per the SSE spec only fires for
// unnamed/"message" frames) never sees any of these. Every known
// PanelEventType (pkg/protocol/types_generated.go) needs its own
// addEventListener to actually receive live events; onmessage is kept
// only as a harmless fallback.
const panelEventTypes = [
  "system.ready",
  "system.warning",
  "workspace.registered",
  "workspace.updated",
  "workspace.availability_changed",
  "session.started",
  "session.phase_changed",
  "session.progress",
  "session.completed",
  "session.failed",
  "task.created",
  "task.updated",
  "task.blocked",
  "task.completed",
  "knowledge.created",
  "knowledge.updated",
  "knowledge.superseded",
  "approval.requested",
  "approval.resolved",
  "evidence.created",
  "git.state_changed",
  "adapter.health_changed",
  "contradiction.detected",
  "contradiction.updated",
  "contradiction.resolved",
  "impact.snapshot_updated",
  "dossier.created",
  "dossier.status_changed",
  "dossier.finalized",
  "handoff.created",
  "handoff.validated",
  "handoff.superseded",
  "delivery.updated",
] as const;

function connect() {
  const source = new EventSource("/api/v1/events");
  source.onopen = () => {
    status = "open";
  };
  source.onerror = () => {
    status = "error";
  };
  const dispatch = (evt: MessageEvent) => {
    for (const listener of listeners) listener(evt);
  };
  source.onmessage = dispatch;
  for (const type of panelEventTypes) {
    source.addEventListener(type, dispatch);
  }
}

if (typeof window !== "undefined" && typeof EventSource !== "undefined") {
  connect();
}
