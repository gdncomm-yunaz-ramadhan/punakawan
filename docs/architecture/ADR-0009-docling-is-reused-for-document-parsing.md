# ADR-0009: Docling is reused for document parsing

## Status
Accepted

## Context
The plan's guiding principle is to reuse mature providers rather than reimplement them, naming Docling MCP or Docling Serve specifically for rich document conversion (§2.1 Orchestrate rather than reimplement; §13.1 Docling).

## Decision
Docling is reused for document parsing.

## Consequences
Punakawan submits files or URLs to Docling and normalizes the structured output — splitting into source-preserving sections, normalizing tables/headings/metadata, and extracting candidate requirements, constraints, claims, and decisions while preserving page/section provenance and tracking parser version and content hash (§13.1). Punakawan must avoid embedding-only storage as the source of truth and must flag uncertain extraction, feeding directly into the knowledge model's provenance and validity-state requirements (§13.1; §7.3 Required provenance fields).
