import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';

/**
 * Real Atlassian remote MCP endpoint (headless variant, not the interactive
 * OAuth 2.1 `/mcp/authv2` variant). Confirmed via Context7
 * `/atlassian/atlassian-mcp-server` and
 * https://support.atlassian.com/atlassian-rovo-mcp-server/docs/configuring-authentication-via-api-token/.
 * This single endpoint serves both headless auth variants below - only the
 * Authorization header differs. See
 * punakawan-go-typescript-detailed-plan.md §13.2.
 *
 * Requires an organization admin to have enabled API token authentication
 * for the Rovo MCP server (Atlassian Administration -> Rovo -> Rovo MCP
 * server -> Authentication) - neither variant below works otherwise.
 */
export const ATLASSIAN_MCP_ENDPOINT = 'https://mcp.atlassian.com/v1/mcp';

export interface AtlassianConfig {
  /**
   * Read from ATLASSIAN_MCP_TOKEN. Two distinct credential types share this
   * field, distinguished by whether `email` is also set:
   *  - A personal API token (created by an individual Atlassian user,
   *    scoped to their own account) - requires `email` alongside it.
   *  - A service-account API key (org-level, not tied to one person) - used
   *    alone, with no email.
   * Confirmed via the docs above: these are NOT interchangeable header
   * formats (see buildAuthorizationHeader).
   */
  token: string;
  /**
   * Atlassian site hostname (e.g. "yourteam.atlassian.net"), read from
   * ATLASSIAN_HOST. The cloudId every tool call actually needs is derived
   * from this via resolveCloudId rather than configured directly - operators
   * already know their site's hostname, unlike its cloud ID, which otherwise
   * has to be looked up separately.
   */
  host: string;
  /**
   * Email of the personal-API-token owner. Read from ATLASSIAN_EMAIL.
   * Optional: a service-account API key has no associated email. When set,
   * this is a personal token and the Authorization header must be Basic
   * base64(email:token); when unset, token is treated as a service-account
   * Bearer key.
   */
  email?: string;
}

/**
 * Reads and validates required configuration from the environment. Fails
 * fast with a clear message when a required var is missing, rather than
 * attempting a request that would 401.
 */
export function loadConfigFromEnv(env: NodeJS.ProcessEnv = process.env): AtlassianConfig {
  const token = env.ATLASSIAN_MCP_TOKEN;
  if (!token) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_MCP_TOKEN: the Atlassian adapter cannot authenticate to the remote MCP server without a token.',
    );
  }

  const host = env.ATLASSIAN_HOST;
  if (!host) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_HOST: the Atlassian adapter derives the cloudId every tool call needs from this site hostname (e.g. "yourteam.atlassian.net") — supply it as operator configuration.',
    );
  }

  const email = env.ATLASSIAN_EMAIL || undefined;

  return { token, host, email };
}

/**
 * Resolves a site hostname (e.g. "yourteam.atlassian.net") to its Atlassian
 * Cloud ID via the unauthenticated tenant-info endpoint, per
 * https://support.atlassian.com/jira/kb/retrieve-my-atlassian-sites-cloud-id/
 * (the "Tenant Info Endpoint" method documented there - no login required).
 * This lets an operator configure just the host they already know instead of
 * separately looking up and pasting a cloud ID.
 */
export async function resolveCloudId(host: string, fetchImpl: typeof fetch = fetch): Promise<string> {
  const url = `https://${host}/_edge/tenant_info`;
  let response: Response;
  try {
    response = await fetchImpl(url);
  } catch (err) {
    throw new Error(`Failed to resolve Atlassian cloudId from host "${host}" (${url}): ${(err as Error).message}`);
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
 * Builds the Authorization header value for AtlassianConfig, per
 * https://support.atlassian.com/atlassian-rovo-mcp-server/docs/configuring-authentication-via-api-token/:
 * a personal API token (email set) uses `Basic base64(email:token)`; a
 * service-account API key (no email) uses `Bearer <token>`. These are not
 * interchangeable - sending a personal token as a Bearer value (or vice
 * versa) is rejected by the server.
 */
export function buildAuthorizationHeader(config: Pick<AtlassianConfig, 'token' | 'email'>): string {
  if (config.email) {
    const encoded = Buffer.from(`${config.email}:${config.token}`, 'utf8').toString('base64');
    return `Basic ${encoded}`;
  }
  return `Bearer ${config.token}`;
}

/**
 * A factory for the transport used to reach the MCP server. Defaults to a
 * real StreamableHTTPClientTransport pointed at the live Atlassian endpoint;
 * tests override this to point at an in-process fake server instead.
 */
export type TransportFactory = (config: Pick<AtlassianConfig, 'token' | 'email'>) => Transport;

export interface AtlassianRemoteTool {
  name: string;
  inputSchema: {
    type: 'object';
    properties?: Record<string, object>;
    required?: string[];
    [key: string]: unknown;
  };
}

const TOOL_ACCESS: Record<string, { group: string; scope: string }> = {
  getJiraIssue: { group: 'read_jira', scope: 'read:jira-work' },
  getJiraIssueTypeMetaWithFields: { group: 'read_jira', scope: 'read:jira-work' },
  getTransitionsForJiraIssue: { group: 'read_jira', scope: 'read:jira-work' },
  searchJiraIssuesUsingJql: { group: 'search_jira', scope: 'search:jira-work' },
  getConfluencePage: { group: 'read_confluence', scope: 'read:page:confluence' },
  searchConfluenceUsingCql: { group: 'search_confluence', scope: 'search:confluence' },
  addCommentToJiraIssue: { group: 'write_jira', scope: 'write:jira-work' },
  addWorklogToJiraIssue: { group: 'write_jira', scope: 'write:jira-work' },
  createJiraIssue: { group: 'write_jira', scope: 'write:jira-work' },
  editJiraIssue: { group: 'write_jira', scope: 'write:jira-work' },
  transitionJiraIssue: { group: 'write_jira', scope: 'write:jira-work' },
};

const ATLASSIAN_TOKEN_AUTH_DOC =
  'https://support.atlassian.com/atlassian-rovo-mcp-server/docs/configuring-authentication-via-api-token/';

function apiTokenPermissionError(detail?: string): Error {
  return new Error(
    [
      'Atlassian MCP rejected API-token access.',
      detail,
      'Ask an organization admin to enable API-token authentication for the Rovo MCP server, then ensure this personal token or service-account key has the required product scopes.',
      'Restart Punakawan after access is changed so it reconnects and refreshes the advertised tool list.',
      `Setup guide: ${ATLASSIAN_TOKEN_AUTH_DOC}`,
    ]
      .filter(Boolean)
      .join(' '),
  );
}

function unavailableToolError(name: string, availableTools: readonly AtlassianRemoteTool[]): Error {
  const access = TOOL_ACCESS[name];
  const availableNames = availableTools.map((tool) => tool.name).sort();
  const advertised = availableNames.length > 0 ? availableNames.join(', ') : '(none)';
  const requirement = access
    ? `The official ${name} tool belongs to permission group "${access.group}" and requires scope "${access.scope}".`
    : `The requested tool is not advertised for this connection.`;

  return new Error(
    [
      `Atlassian MCP does not advertise required tool "${name}" for this authenticated connection.`,
      requirement,
      `Advertised tools: ${advertised}.`,
      'Ask an organization admin to enable API-token authentication and the required Rovo MCP permission group, and recreate or update the token/key with the required scope.',
      'Restart Punakawan after access is changed so it reconnects and refreshes the advertised tool list.',
      `Setup guide: ${ATLASSIAN_TOKEN_AUTH_DOC}`,
    ].join(' '),
  );
}

export function defaultTransportFactory(config: Pick<AtlassianConfig, 'token' | 'email'>): Transport {
  return new StreamableHTTPClientTransport(new URL(ATLASSIAN_MCP_ENDPOINT), {
    requestInit: { headers: { Authorization: buildAuthorizationHeader(config) } },
  });
}

/**
 * Lazily-connecting MCP client wrapper. Connects once per adapter process on
 * first use and reuses the connection across subsequent calls, per
 * punakawan-go-typescript-detailed-plan.md §13.2 ("Use official Atlassian MCP
 * for Cloud where possible").
 */
export class AtlassianMcpClient {
  private readonly config: AtlassianConfig;
  private readonly transportFactory: TransportFactory;
  private readonly cloudIdResolver: (host: string) => Promise<string>;
  private client: Client | undefined;
  private connecting: Promise<Client> | undefined;
  private cloudIdPromise: Promise<string> | undefined;
  private toolsPromise: Promise<AtlassianRemoteTool[]> | undefined;

  constructor(
    config: AtlassianConfig,
    transportFactory: TransportFactory = defaultTransportFactory,
    cloudIdResolver: (host: string) => Promise<string> = resolveCloudId,
  ) {
    this.config = config;
    this.transportFactory = transportFactory;
    this.cloudIdResolver = cloudIdResolver;
  }

  /**
   * Resolves and memoizes the cloudId for this client's configured host - a
   * site's cloudId never changes within a process's lifetime, so this
   * mirrors how connect() below memoizes the MCP connection itself rather
   * than reconnecting per call.
   */
  getCloudId(): Promise<string> {
    if (!this.cloudIdPromise) {
      this.cloudIdPromise = this.cloudIdResolver(this.config.host);
    }
    return this.cloudIdPromise;
  }

  private async connect(): Promise<Client> {
    if (this.client) return this.client;
    if (this.connecting) return this.connecting;

    this.connecting = (async () => {
      const client = new Client({ name: 'punakawan-atlassian-adapter', version: '0.1.0' });
      const transport = this.transportFactory({ token: this.config.token, email: this.config.email });
      await client.connect(transport);
      this.client = client;
      return client;
    })();

    try {
      return await this.connecting;
    } finally {
      this.connecting = undefined;
    }
  }

  /**
   * Returns the exact tools advertised for this authenticated connection.
   * Atlassian filters tools by authentication mode, organization permission
   * groups, product access, and token scopes, so the public supported-tools
   * list is not sufficient to know what this caller can actually invoke.
   */
  async listTools(): Promise<readonly AtlassianRemoteTool[]> {
    if (!this.toolsPromise) {
      this.toolsPromise = (async () => {
        const client = await this.connect();
        const tools: AtlassianRemoteTool[] = [];
        let cursor: string | undefined;
        do {
          const result = await client.listTools(cursor ? { cursor } : undefined);
          tools.push(...(result.tools as AtlassianRemoteTool[]));
          cursor = result.nextCursor;
        } while (cursor);
        return tools;
      })();
    }
    return this.toolsPromise;
  }

  async callTool(name: string, args: Record<string, unknown>): Promise<{
    content: unknown;
    structuredContent?: Record<string, unknown>;
  }> {
    const client = await this.connect();
    const tools = await this.listTools();
    if (!tools.some((tool) => tool.name === name)) {
      throw unavailableToolError(name, tools);
    }

    const result = await client.callTool({ name, arguments: args });
    if (result.isError) {
      const text = Array.isArray(result.content)
        ? result.content
            .map((block) => (block && typeof block === 'object' && 'text' in block ? String((block as { text: unknown }).text) : ''))
            .filter(Boolean)
            .join('; ')
        : '';
      if (/don't have permission to connect via API token/i.test(text)) {
        throw apiTokenPermissionError(text);
      }
      throw new Error(`Atlassian MCP tool "${name}" returned an error${text ? `: ${text}` : ''}`);
    }
    return { content: result.content, structuredContent: result.structuredContent as Record<string, unknown> | undefined };
  }

  async close(): Promise<void> {
    if (this.client) {
      await this.client.close();
      this.client = undefined;
      this.toolsPromise = undefined;
    }
  }
}
