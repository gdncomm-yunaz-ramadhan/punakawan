// Thin fetch wrapper for /api/v1: the generic JSON transport every
// domain-specific call in client.ts builds on.

import { fetchWithCsrf } from "../session";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    // Machine-readable error code carried on validation (400) and
    // conflict (409) responses so the UI can render a specific message
    // (e.g. "duplicate_key", "secret_rejected") rather than only the
    // human-readable string. Undefined for errors that carry no code.
    public code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    headers: { Accept: "application/json" },
    signal,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, body.error ?? res.statusText);
  }
  return res.json() as Promise<T>;
}

// mutateJSON is the write-side sibling of getJSON: it goes through
// fetchWithCsrf (attaching the session CSRF header and mapping 401/403 to
// SessionExpiredError) and, on any other non-2xx, throws an ApiError that
// preserves both the server's `code` (409 conflict / 400 validation) and
// its human-readable message so the UI can branch on either.
export async function mutateJSON<T>(path: string, init: RequestInit): Promise<T> {
  const res = await fetchWithCsrf(`/api/v1${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", Accept: "application/json", ...(init.headers ?? {}) },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { error?: string; message?: string; code?: string });
    throw new ApiError(res.status, body.error ?? body.message ?? res.statusText, body.code);
  }
  return res.json() as Promise<T>;
}
