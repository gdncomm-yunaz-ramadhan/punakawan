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

function connect() {
  const source = new EventSource("/api/v1/events");
  source.onopen = () => {
    status = "open";
  };
  source.onerror = () => {
    status = "error";
  };
  source.onmessage = (evt) => {
    for (const listener of listeners) listener(evt);
  };
}

if (typeof window !== "undefined" && typeof EventSource !== "undefined") {
  connect();
}
