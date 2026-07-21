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
  /** Atlassian Cloud ID, required by every tool call. Read from ATLASSIAN_CLOUD_ID. */
  cloudId: string;
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

  const cloudId = env.ATLASSIAN_CLOUD_ID;
  if (!cloudId) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_CLOUD_ID: every Atlassian MCP tool call requires a cloudId, and this adapter does not implement site enumeration (no confirmed tool for it) — supply it as operator configuration.',
    );
  }

  const email = env.ATLASSIAN_EMAIL || undefined;

  return { token, cloudId, email };
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
  private client: Client | undefined;
  private connecting: Promise<Client> | undefined;

  constructor(config: AtlassianConfig, transportFactory: TransportFactory = defaultTransportFactory) {
    this.config = config;
    this.transportFactory = transportFactory;
  }

  get cloudId(): string {
    return this.config.cloudId;
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

  async callTool(name: string, args: Record<string, unknown>): Promise<{
    content: unknown;
    structuredContent?: Record<string, unknown>;
  }> {
    const client = await this.connect();
    const result = await client.callTool({ name, arguments: args });
    if (result.isError) {
      const text = Array.isArray(result.content)
        ? result.content
            .map((block) => (block && typeof block === 'object' && 'text' in block ? String((block as { text: unknown }).text) : ''))
            .filter(Boolean)
            .join('; ')
        : '';
      throw new Error(`Atlassian MCP tool "${name}" returned an error${text ? `: ${text}` : ''}`);
    }
    return { content: result.content, structuredContent: result.structuredContent as Record<string, unknown> | undefined };
  }

  async close(): Promise<void> {
    if (this.client) {
      await this.client.close();
      this.client = undefined;
    }
  }
}
