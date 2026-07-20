import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import type { Transport } from '@modelcontextprotocol/sdk/shared/transport.js';

/**
 * Real Atlassian remote MCP endpoint (token-header variant, not the
 * interactive OAuth 2.1 variant). Confirmed via Context7
 * `/atlassian/atlassian-mcp-server`. See
 * punakawan-go-typescript-detailed-plan.md §13.2.
 */
export const ATLASSIAN_MCP_ENDPOINT = 'https://mcp.atlassian.com/v1/mcp';

export interface AtlassianConfig {
  /** Bearer token for the Atlassian MCP server. Read from ATLASSIAN_MCP_TOKEN. */
  token: string;
  /** Atlassian Cloud ID, required by every tool call. Read from ATLASSIAN_CLOUD_ID. */
  cloudId: string;
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
      'Missing required environment variable ATLASSIAN_MCP_TOKEN: the Atlassian adapter cannot authenticate to the remote MCP server without a bearer token.',
    );
  }

  const cloudId = env.ATLASSIAN_CLOUD_ID;
  if (!cloudId) {
    throw new Error(
      'Missing required environment variable ATLASSIAN_CLOUD_ID: every Atlassian MCP tool call requires a cloudId, and this adapter does not implement site enumeration (no confirmed tool for it) — supply it as operator configuration.',
    );
  }

  return { token, cloudId };
}

/**
 * A factory for the transport used to reach the MCP server. Defaults to a
 * real StreamableHTTPClientTransport pointed at the live Atlassian endpoint;
 * tests override this to point at an in-process fake server instead.
 */
export type TransportFactory = (token: string) => Transport;

export function defaultTransportFactory(token: string): Transport {
  return new StreamableHTTPClientTransport(new URL(ATLASSIAN_MCP_ENDPOINT), {
    requestInit: { headers: { Authorization: `Bearer ${token}` } },
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
      const transport = this.transportFactory(this.config.token);
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
