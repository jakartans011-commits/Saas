import { APIError, parseError } from "./errors.js";
import type {
  Agent,
  DeletionStatus,
  LoginResponse,
  MCPRequest,
  MCPResponse,
  MCPToolCall,
  Tenant,
  Tool,
  UsageEvent,
  User,
} from "./types.js";

export type { APIError };

/** Options accepted by {@link SaasAgentClient.constructor}. */
export interface ClientOptions {
  /** Base URL of the SaaS Agent API, e.g. "https://api.example.com". */
  baseUrl: string;
  /** Bearer token sent as `Authorization: Bearer <apiKey>`. Omit or pass "" to send no auth header. */
  apiKey?: string;
  /**
   * Request timeout in milliseconds. Mirrors the Go client's
   * `http.Client{Timeout: 30 * time.Second}` default.
   * Defaults to 30000 (30s).
   */
  timeoutMs?: number;
  /** Override the fetch implementation (mainly for testing). Defaults to the global `fetch`. */
  fetch?: typeof fetch;
}

interface RequestOptions {
  method: string;
  path: string;
  body?: unknown;
  headers?: Record<string, string>;
}

/**
 * Client for the SaaS Agent API.
 *
 * This is a 1:1 TypeScript port of the Go SDK (sdk/go/client.go):
 * every exported method here has the same name (in camelCase),
 * parameters, request shape, and error behavior as its Go
 * counterpart.
 */
export class SaasAgentClient {
  readonly baseUrl: string;
  readonly apiKey: string;
  private readonly timeoutMs: number;
  private readonly fetchImpl: typeof fetch;

  constructor(options: ClientOptions) {
    if (!options.baseUrl) {
      throw new Error("SaasAgentClient: baseUrl is required");
    }
    this.baseUrl = options.baseUrl;
    this.apiKey = options.apiKey ?? "";
    this.timeoutMs = options.timeoutMs ?? 30_000;
    this.fetchImpl = options.fetch ?? globalThis.fetch;

    if (!this.fetchImpl) {
      throw new Error(
        "SaasAgentClient: no fetch implementation available. Use Node.js 18+, " +
          "or pass a `fetch` implementation via ClientOptions.",
      );
    }
  }

  private async doRequest(opts: RequestOptions): Promise<Response> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.timeoutMs);

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (this.apiKey !== "") {
      headers["Authorization"] = `Bearer ${this.apiKey}`;
    }
    if (opts.headers) {
      Object.assign(headers, opts.headers);
    }

    try {
      return await this.fetchImpl(this.baseUrl + opts.path, {
        method: opts.method,
        headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
        signal: controller.signal,
      });
    } catch (err) {
      if (err instanceof Error && err.name === "AbortError") {
        throw new Error(`request timed out after ${this.timeoutMs}ms`);
      }
      throw err;
    } finally {
      clearTimeout(timeout);
    }
  }

  // ============================================================
  // Auth
  // ============================================================

  /** Authenticates a user. */
  async login(email: string, password: string): Promise<LoginResponse> {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/auth/login",
      body: { email, password },
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as LoginResponse;
  }

  /** Refreshes the access token. */
  async refreshToken(refreshToken: string): Promise<LoginResponse> {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/auth/refresh",
      body: { refresh_token: refreshToken },
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as LoginResponse;
  }

  // ============================================================
  // Tenant
  // ============================================================

  /** Creates a new tenant (root only). */
  async createTenant(tenantID: string, name: string): Promise<Tenant> {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/admin/tenants",
      body: { tenant_id: tenantID, name },
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { tenant: Tenant };
    return result.tenant;
  }

  // ============================================================
  // User
  // ============================================================

  /** Creates a new user in a tenant. */
  async createUser(
    tenantID: string,
    email: string,
    password: string,
    roleID: string,
  ): Promise<User> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/users`,
      body: { email, password, role_id: roleID },
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { user: User };
    return result.user;
  }

  // ============================================================
  // Usage
  // ============================================================

  /** Gets usage events for a tenant. Pass `limit <= 0` to omit the limit query param. */
  async getUsage(tenantID: string, limit: number): Promise<UsageEvent[]> {
    let path = `/api/tenants/${tenantID}/usage`;
    if (limit > 0) {
      path += `?limit=${limit}`;
    }

    const resp = await this.doRequest({ method: "GET", path });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { events: UsageEvent[] };
    return result.events;
  }

  // ============================================================
  // Agent
  // ============================================================

  /** Creates a new agent. */
  async createAgent(
    tenantID: string,
    name: string,
    description: string,
    capabilities: string[],
    metadata: Record<string, string>,
  ): Promise<Agent> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/agents`,
      body: { name, description, capabilities, metadata },
    });

    if (resp.status !== 201) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { agent: Agent };
    return result.agent;
  }

  /** Lists agents for a tenant. */
  async listAgents(tenantID: string): Promise<Agent[]> {
    const resp = await this.doRequest({
      method: "GET",
      path: `/api/tenants/${tenantID}/agents`,
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { agents: Agent[] };
    return result.agents;
  }

  // ============================================================
  // Tool
  // ============================================================

  /** Creates a new tool. */
  async createTool(
    tenantID: string,
    name: string,
    description: string,
    toolType: string,
    config: Record<string, string>,
  ): Promise<Tool> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/tools`,
      body: { name, description, type: toolType, config },
    });

    if (resp.status !== 201) {
      throw await parseError(resp);
    }

    const result = (await resp.json()) as { tool: Tool };
    return result.tool;
  }

  /** Executes a tool. */
  async executeTool(
    tenantID: string,
    toolID: string,
    params: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/tools/${toolID}/execute`,
      body: { params },
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as Record<string, unknown>;
  }

  // ============================================================
  // MCP
  // ============================================================

  /** Sends an MCP query. */
  async mcpQuery(tenantID: string, req: MCPRequest): Promise<MCPResponse> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/mcp/query`,
      body: req,
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as MCPResponse;
  }

  /**
   * Sends an explicit tool-call MCP request, bypassing query
   * resolution on the server. Use this when the tool to invoke is
   * already known, instead of relying on Query-based resolution.
   *
   * `params` is omitted from the request body entirely when
   * `undefined`/omitted (matching the `omitempty` tag on
   * `MCPToolCall.Params` in the Go SDK) rather than being sent as
   * `null` or `{}`.
   */
  async mcpToolCall(
    tenantID: string,
    toolName: string,
    params?: Record<string, unknown>,
  ): Promise<MCPResponse> {
    const toolCall: MCPToolCall =
      params !== undefined ? { tool_name: toolName, params } : { tool_name: toolName };

    return this.mcpQuery(tenantID, { tool_call: toolCall });
  }

  // ============================================================
  // GDPR
  // ============================================================

  /** Soft-deletes a tenant (GDPR right to erasure). */
  async softDeleteTenant(tenantID: string): Promise<DeletionStatus> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/data/delete`,
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as DeletionStatus;
  }

  /** Restores a soft-deleted tenant. */
  async restoreTenant(tenantID: string): Promise<DeletionStatus> {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/data/restore`,
    });

    if (resp.status !== 200) {
      throw await parseError(resp);
    }

    return (await resp.json()) as DeletionStatus;
  }
}
