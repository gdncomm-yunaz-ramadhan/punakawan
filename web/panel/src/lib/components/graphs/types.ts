// Shared typed shapes for graph view components. These are presentation
// types only - the actual data-fetching/API layer is out of scope here,
// callers pass already-fetched nodes/edges.
export interface GraphNode {
  id: string;
  label: string;
  type?: string;
  data?: Record<string, unknown>;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  label?: string;
  type?: string;
}

export type GraphLayoutName = "cose" | "breadthfirst";

// Sane default cap on rendered node count (§ "large graphs start from
// focused, bounded datasets"). GraphCanvas accepts this as an overridable
// prop but defaults to it.
export const DEFAULT_NODE_CAP = 150;
