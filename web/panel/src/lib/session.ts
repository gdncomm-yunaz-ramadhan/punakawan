// Client side of the panel's authenticated mutation session
// (internal/panel/session, per punakawan-artifact-review-plan-mutation-plan-v2.md
// §15). The panel process mints a one-time bootstrap token and opens the
// browser at `/?bootstrap=<token>`; this module exchanges it for a
// session cookie (set HttpOnly by the server - never read here) plus a
// CSRF token, which is the only piece of session state JS ever touches.
//
// The CSRF token is kept in a module-level variable, not
// localStorage/sessionStorage: it must die with the tab/reload exactly
// like the bootstrap flow intends (§15's "session invalidated when the
// panel stops" - a lost token can only be replaced by a fresh bootstrap
// from the panel process, never recovered client-side).

const CSRF_HEADER = "X-Csrf-Token";

let csrfToken: string | null = null;

export function getCsrfToken(): string | null {
  return csrfToken;
}

// Test-only seam: lets tests set up a session without going through a
// real bootstrap exchange.
export function setCsrfToken(token: string | null): void {
  csrfToken = token;
}

export interface ExchangeResult {
  csrfToken: string;
  expiresAt: string;
}

// exchangeBootstrapToken posts the one-time token to the session
// endpoint, stores the returned CSRF token in memory, and returns it.
// Throws on any non-2xx response - callers decide how to surface that
// (there is no client-side recovery per §15, only a fresh bootstrap).
export async function exchangeBootstrapToken(bootstrapToken: string): Promise<ExchangeResult> {
  const res = await fetch("/api/v1/session/exchange", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ bootstrap_token: bootstrapToken }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error ?? res.statusText);
  }
  const data = (await res.json()) as { csrf_token: string; expires_at: string };
  csrfToken = data.csrf_token;
  return { csrfToken: data.csrf_token, expiresAt: data.expires_at };
}

// initSessionFromUrl checks window.location.search for a `bootstrap`
// param on app startup. If present, it exchanges it and then strips the
// param from the URL via history.replaceState so a one-time token never
// sits in browser history/the URL bar. Returns true if a bootstrap
// exchange was attempted (regardless of success), so callers can
// distinguish "no bootstrap param present" from "exchange failed."
export async function initSessionFromUrl(): Promise<{ attempted: boolean; error?: string }> {
  if (typeof window === "undefined") return { attempted: false };

  const url = new URL(window.location.href);
  const token = url.searchParams.get("bootstrap");
  if (!token) return { attempted: false };

  url.searchParams.delete("bootstrap");
  const cleaned = url.pathname + (url.search ? url.search : "") + url.hash;
  window.history.replaceState({}, "", cleaned);

  try {
    await exchangeBootstrapToken(token);
    return { attempted: true };
  } catch (e) {
    return { attempted: true, error: e instanceof Error ? e.message : String(e) };
  }
}

// SessionExpiredError signals a 401/403 from a mutating request - per
// §15, there is no client-side recovery from this, only reopening the
// panel from the terminal to mint a fresh bootstrap token.
export class SessionExpiredError extends Error {
  constructor() {
    super("Your session has expired - reopen the panel from the terminal to continue.");
    this.name = "SessionExpiredError";
  }
}

// fetchWithCsrf wraps fetch for mutating (non-GET) requests: it attaches
// the stored CSRF token header and maps a 401/403 response to
// SessionExpiredError rather than letting callers misinterpret it as an
// ordinary API error. GET requests need no CSRF header (§15) - use plain
// fetch/getJSON for those.
export async function fetchWithCsrf(input: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  if (csrfToken) {
    headers.set(CSRF_HEADER, csrfToken);
  }
  const res = await fetch(input, { ...init, headers });
  if (res.status === 401 || res.status === 403) {
    throw new SessionExpiredError();
  }
  return res;
}
