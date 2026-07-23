// Lazy singleton loader for Cytoscape.js (§ Artifact Review Phase 2).
//
// Same discipline as chartLoader.ts: cytoscape is only dynamically
// imported the moment a graph view is actually about to render, never at
// app bundle time. No layout-extension packages are used - only
// cytoscape's built-in "cose" and "breadthfirst" layouts, per the CI
// budget (<300KB compressed for the graph chunk).
import type cytoscape from "cytoscape";

type CytoscapeModule = typeof cytoscape;

let loadPromise: Promise<CytoscapeModule> | null = null;

export async function loadCytoscape(): Promise<CytoscapeModule> {
  if (!loadPromise) {
    loadPromise = import("cytoscape").then((mod) => mod.default);
  }
  return loadPromise;
}

// Test-only hook: resets the cached loader so each test file can mock
// "cytoscape" freshly without a prior test's real import lingering.
export function __resetGraphLoaderForTests(): void {
  loadPromise = null;
}
