export interface GitHubConfig {
  token: string;
  /** REST base, e.g. "https://api.github.com" (default) or a GHES instance's "/api/v3" base. */
  apiBaseUrl: string;
  /** GraphQL endpoint, e.g. "https://api.github.com/graphql" (default) or a GHES instance's "/api/graphql". */
  graphqlUrl: string;
}

export interface RestRequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  headers?: Record<string, string>;
}

export interface RestResponse<T = unknown> {
  status: number;
  data: T;
  url: string;
}

function normalizeApiBaseUrl(value: string): string {
  return value.replace(/\/+$/, '');
}

/** Reads direct GitHub REST/GraphQL credentials from the environment. */
export function loadConfigFromEnv(env: NodeJS.ProcessEnv = process.env): GitHubConfig {
  // GITHUB_TOKEN is GitHub Actions' own convention; GH_TOKEN is the `gh` CLI's.
  // Accepting both means a token already exported for either tool works here
  // with no extra configuration.
  const token = env.GITHUB_TOKEN || env.GH_TOKEN;
  if (!token) {
    throw new Error('Missing required environment variable GITHUB_TOKEN (a GitHub personal access token or GitHub App installation token).');
  }

  const apiBaseUrl = normalizeApiBaseUrl(env.GITHUB_API_URL || 'https://api.github.com');
  // GHES's GraphQL endpoint is "<host>/api/graphql", sibling to its REST base
  // "<host>/api/v3" - only the "/v3" suffix is dropped, not "/api" too.
  // github.com itself has no "/api/v3" suffix to strip, so this is a no-op
  // there and the result is simply "https://api.github.com/graphql".
  const graphqlUrl = env.GITHUB_GRAPHQL_URL || `${apiBaseUrl.replace(/\/v3$/, '')}/graphql`;

  return { token, apiBaseUrl, graphqlUrl };
}

function errorDetail(data: unknown): string {
  if (typeof data === 'string') return data;
  try {
    return JSON.stringify(data);
  } catch {
    return String(data);
  }
}

/** Direct GitHub REST + GraphQL client; no separate MCP proxy is involved. */
export class GitHubRestClient {
  constructor(
    private readonly config: GitHubConfig,
    private readonly fetchImpl: typeof fetch = fetch,
  ) {}

  private headers(extra?: Record<string, string>): Record<string, string> {
    return {
      Accept: 'application/vnd.github+json',
      Authorization: `Bearer ${this.config.token}`,
      'X-GitHub-Api-Version': '2022-11-28',
      ...extra,
    };
  }

  async request<T = unknown>(path: string, options: RestRequestOptions = {}): Promise<RestResponse<T>> {
    const normalizedPath = path.startsWith('/') ? path : `/${path}`;
    const url = new URL(`${this.config.apiBaseUrl}${normalizedPath}`);
    for (const [key, value] of Object.entries(options.query ?? {})) {
      if (value !== undefined) url.searchParams.set(key, String(value));
    }

    const headers = this.headers(options.headers);
    let body: BodyInit | undefined;
    if (options.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(options.body);
    }

    let response: Response;
    try {
      response = await this.fetchImpl(url, { method: options.method ?? 'GET', headers, body });
    } catch (error) {
      throw new Error(`Direct GitHub REST request failed for ${url}: ${(error as Error).message}`);
    }

    const text = response.status === 204 ? '' : await response.text();
    let data: unknown = {};
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = text;
      }
    }

    if (!response.ok) {
      const authHint =
        response.status === 401 || response.status === 403
          ? ' Check the token and its repository/PR permissions (contents, pull-requests, checks scopes).'
          : '';
      throw new Error(
        `GitHub REST ${options.method ?? 'GET'} ${url.pathname} failed with HTTP ${response.status}: ${errorDetail(data)}.${authHint}`,
      );
    }

    return { status: response.status, data: data as T, url: url.toString() };
  }

  /**
   * Runs a GraphQL query/mutation. Only used for operations REST has no
   * equivalent for (resolving a review thread has no REST endpoint - it is
   * GraphQL-only: https://docs.github.com/en/graphql/reference/mutations#resolvereviewthread).
   */
  async graphql<T = unknown>(query: string, variables: Record<string, unknown>): Promise<T> {
    let response: Response;
    try {
      response = await this.fetchImpl(this.config.graphqlUrl, {
        method: 'POST',
        headers: this.headers({ 'Content-Type': 'application/json' }),
        body: JSON.stringify({ query, variables }),
      });
    } catch (error) {
      throw new Error(`GitHub GraphQL request failed: ${(error as Error).message}`);
    }

    const payload = (await response.json()) as { data?: T; errors?: { message: string }[] };
    if (!response.ok || payload.errors?.length) {
      const detail = payload.errors?.map((e) => e.message).join('; ') ?? `HTTP ${response.status}`;
      throw new Error(`GitHub GraphQL request failed: ${detail}`);
    }
    return payload.data as T;
  }

  async close(): Promise<void> {
    // fetch has no persistent protocol session to close.
  }
}
