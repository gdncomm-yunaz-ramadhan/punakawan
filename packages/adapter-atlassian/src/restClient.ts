export interface AtlassianConfig {
  token: string;
  host: string;
  email?: string;
  /** Scoped tokens use Atlassian's API gateway; unscoped personal tokens use the site URL. */
  scoped: boolean;
}

export interface RestRequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  multipart?: FormData;
  headers?: Record<string, string>;
  /** Propagated straight into fetch's own options so an in-flight request aborts when the caller's operation is cancelled. */
  signal?: AbortSignal;
}

export interface RestResponse<T = unknown> {
  status: number;
  data: T;
  url: string;
}

export interface BinaryRestResponse {
  status: number;
  data: Uint8Array;
  url: string;
  contentType?: string;
}

function parseBoolean(value: string | undefined): boolean | undefined {
  if (value === undefined || value === '') return undefined;
  if (/^(1|true|yes)$/i.test(value)) return true;
  if (/^(0|false|no)$/i.test(value)) return false;
  throw new Error('ATLASSIAN_API_TOKEN_SCOPED must be true/false, yes/no, or 1/0.');
}

function normalizeHost(value: string): string {
  const candidate = value.includes('://') ? value : `https://${value}`;
  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    throw new Error(`ATLASSIAN_HOST is not a valid hostname: ${value}`);
  }
  if (url.protocol !== 'https:' || (url.pathname !== '/' && url.pathname !== '')) {
    throw new Error('ATLASSIAN_HOST must be an HTTPS hostname without a path (for example "team.atlassian.net").');
  }
  return url.host;
}

/** Reads direct Atlassian REST credentials from the environment. */
export function loadConfigFromEnv(env: NodeJS.ProcessEnv = process.env): AtlassianConfig {
  // Keep the old variable as a one-release migration path for existing global installations.
  const token = env.ATLASSIAN_API_TOKEN || env.ATLASSIAN_MCP_TOKEN;
  if (!token) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_API_TOKEN: create an Atlassian API token for direct Jira REST access.',
    );
  }

  const hostValue = env.ATLASSIAN_HOST;
  if (!hostValue) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_HOST (for example "team.atlassian.net").',
    );
  }

  const email = env.ATLASSIAN_EMAIL || undefined;
  const explicitScoped = parseBoolean(env.ATLASSIAN_API_TOKEN_SCOPED);
  // Service-account tokens are scoped. Personal tokens default to the simpler
  // unscoped/site-URL mode unless the installer or operator says otherwise.
  const scoped = explicitScoped ?? !email;

  return { token, host: normalizeHost(hostValue), email, scoped };
}

/** Personal tokens use Basic email:token; service-account tokens may use Bearer. */
export function buildAuthorizationHeader(config: Pick<AtlassianConfig, 'token' | 'email'>): string {
  if (config.email) {
    return `Basic ${Buffer.from(`${config.email}:${config.token}`, 'utf8').toString('base64')}`;
  }
  return `Bearer ${config.token}`;
}

/** Resolves a site hostname to the cloud ID needed by scoped API gateway URLs. */
export async function resolveCloudId(host: string, fetchImpl: typeof fetch = fetch): Promise<string> {
  const url = `https://${normalizeHost(host)}/_edge/tenant_info`;
  let response: Response;
  try {
    response = await fetchImpl(url);
  } catch (error) {
    throw new Error(`Failed to resolve Atlassian cloudId from host "${host}" (${url}): ${(error as Error).message}`);
  }
  if (!response.ok) {
    throw new Error(`Failed to resolve Atlassian cloudId from host "${host}": ${url} returned HTTP ${response.status}`);
  }

  const body = (await response.json()) as { cloudId?: unknown };
  if (typeof body.cloudId !== 'string' || !body.cloudId) {
    throw new Error(`Failed to resolve Atlassian cloudId from host "${host}": ${url} did not return a cloudId`);
  }
  return body.cloudId;
}

/**
 * An AbortError must reach the JSON-RPC layer (packages/adapter-sdk/src/stdio.ts)
 * with its own name intact so it can be reported as `data: {code:
 * "cancelled"}` instead of an ordinary rejection - wrapping it in a plain
 * Error the way every other network failure below is wrapped (for a
 * helpful "which request failed and why" message) would erase that
 * distinction. Only a non-abort failure gets the wrapped, contextual
 * message; an abort is rethrown exactly as fetch raised it.
 */
function rethrowUnlessAborted(error: unknown, context: string): Error {
  if (error instanceof Error && error.name === 'AbortError') return error;
  return new Error(`${context}: ${(error as Error).message}`);
}

function errorDetail(data: unknown): string {
  if (typeof data === 'string') return data;
  try {
    return JSON.stringify(data);
  } catch {
    return String(data);
  }
}

/** Direct Jira/Confluence REST client; no hosted Rovo MCP server is involved. */
export class AtlassianRestClient {
  private cloudIdPromise: Promise<string> | undefined;

  constructor(
    private readonly config: AtlassianConfig,
    private readonly fetchImpl: typeof fetch = fetch,
    private readonly cloudIdResolver: (host: string) => Promise<string> = (host) => resolveCloudId(host, fetchImpl),
  ) {}

  getCloudId(): Promise<string> {
    if (!this.cloudIdPromise) this.cloudIdPromise = this.cloudIdResolver(this.config.host);
    return this.cloudIdPromise;
  }

  async jira<T = unknown>(path: string, options: RestRequestOptions = {}): Promise<RestResponse<T>> {
    return this.request<T>('jira', path, options);
  }

  async confluence<T = unknown>(path: string, options: RestRequestOptions = {}): Promise<RestResponse<T>> {
    return this.request<T>('confluence', path, options);
  }

  /** Downloads binary Jira content without converting it to model-facing text. */
  async jiraBytes(path: string, signal?: AbortSignal): Promise<BinaryRestResponse> {
    const url = await this.buildURL('jira', path);
    let response: Response;
    try {
      response = await this.fetchImpl(url, {
        headers: {
          Accept: '*/*',
          Authorization: buildAuthorizationHeader(this.config),
        },
        signal,
      });
    } catch (error) {
      throw rethrowUnlessAborted(error, `Direct Atlassian REST request failed for ${url}`);
    }
    if (!response.ok) {
      const detail = await response.text();
      throw new Error(`Atlassian REST GET ${url.pathname} failed with HTTP ${response.status}: ${detail}`);
    }
    return {
      status: response.status,
      data: new Uint8Array(await response.arrayBuffer()),
      url: response.url || url.toString(),
      contentType: response.headers.get('content-type') ?? undefined,
    };
  }

  private async buildURL(product: 'jira' | 'confluence', path: string): Promise<URL> {
    const baseUrl = this.config.scoped
      ? `https://api.atlassian.com/ex/${product}/${await this.getCloudId()}`
      : `https://${this.config.host}`;
    const normalizedPath = path.startsWith('/') ? path : `/${path}`;
    return new URL(`${baseUrl}${normalizedPath}`);
  }

  /**
   * Jira Cloud does not answer 401 for a rejected credential on an issue
   * read - it treats the caller as anonymous and answers 404, the same
   * "issue does not exist or you do not have permission to see it" an
   * unknown key gets, localized to the site's own language. Read at face
   * value that says the issue is gone when in truth the token expired, so
   * a not-found is checked once against an endpoint that does distinguish
   * the two.
   *
   * The probe uses fetch directly rather than this.request, which would
   * recurse straight back into this check, and its result is memoized so
   * a burst of failures costs one extra request rather than one each.
   */
  private credentialsAcceptedPromise: Promise<boolean> | undefined;

  private credentialsAccepted(): Promise<boolean> {
    if (!this.credentialsAcceptedPromise) {
      this.credentialsAcceptedPromise = (async () => {
        try {
          const url = await this.buildURL('jira', '/rest/api/3/myself');
          const response = await this.fetchImpl(url, {
            headers: { Accept: 'application/json', Authorization: buildAuthorizationHeader(this.config) },
          });
          // Only an auth-shaped answer is evidence about the credential.
          // Any other failure - a missing endpoint, a server error - says
          // nothing about it and must not be reported as one.
          return response.status !== 401 && response.status !== 403;
        } catch {
          return true;
        }
      })();
    }
    return this.credentialsAcceptedPromise;
  }

  private async request<T>(
    product: 'jira' | 'confluence',
    path: string,
    options: RestRequestOptions,
  ): Promise<RestResponse<T>> {
    const url = await this.buildURL(product, path);
    for (const [key, value] of Object.entries(options.query ?? {})) {
      if (value !== undefined) url.searchParams.set(key, String(value));
    }

    const headers: Record<string, string> = {
      Accept: 'application/json',
      Authorization: buildAuthorizationHeader(this.config),
      ...options.headers,
    };
    let body: BodyInit | undefined;
    if (options.multipart !== undefined) {
      body = options.multipart;
    } else if (options.body !== undefined) {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(options.body);
    }

    let response: Response;
    try {
      response = await this.fetchImpl(url, { method: options.method ?? 'GET', headers, body, signal: options.signal });
    } catch (error) {
      throw rethrowUnlessAborted(error, `Direct Atlassian REST request failed for ${url}`);
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
      if (response.status === 404 && !(await this.credentialsAccepted())) {
        throw new Error(
          `Atlassian credentials rejected: ${this.config.host} answered HTTP ${response.status} for ${url.pathname}, and the ` +
            `configured token is not accepted for the authenticated-user read either. The API token is expired, revoked, or ` +
            `wrong for this account - the resource itself may well exist. Re-run \`punakawan setup\` with a fresh token.`,
        );
      }
      const authHint =
        response.status === 401 || response.status === 403
          ? ' Check the API token, account email, scoped-token mode, product scopes, and the account\'s Jira/Confluence permissions.'
          : '';
      throw new Error(
        `Atlassian REST ${options.method ?? 'GET'} ${url.pathname} failed with HTTP ${response.status}: ${errorDetail(data)}.${authHint}`,
      );
    }

    return { status: response.status, data: data as T, url: url.toString() };
  }

  async close(): Promise<void> {
    // fetch has no persistent protocol session to close.
  }
}
