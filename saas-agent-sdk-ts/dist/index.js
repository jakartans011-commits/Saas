// src/errors.ts
var APIError = class _APIError extends Error {
  constructor(code, message, statusCode) {
    super(`API error [${code}]: ${message} (status ${statusCode})`);
    this.name = "APIError";
    this.code = code;
    this.statusCode = statusCode;
    this.message = message === void 0 ? "" : message;
    Object.setPrototypeOf(this, _APIError.prototype);
  }
  /**
   * Formats the error the same way Go's `(*APIError).Error()` does:
   * `API error [<code>]: <message> (status <statusCode>)`.
   */
  toString() {
    return `API error [${this.code}]: ${this.message} (status ${this.statusCode})`;
  }
};
function apiErrorCodeForStatus(statusCode) {
  switch (statusCode) {
    case 400:
      return "invalid_request";
    case 401:
      return "unauthenticated";
    case 403:
      return "forbidden";
    case 404:
      return "tool_not_found";
    case 500:
      return "internal_error";
    default:
      return "unknown_error";
  }
}
async function parseError(response) {
  const code = apiErrorCodeForStatus(response.status);
  let body;
  try {
    body = await response.text();
  } catch (err) {
    return new APIError(
      code,
      `failed to read error body: ${err instanceof Error ? err.message : String(err)}`,
      response.status
    );
  }
  let message = body;
  try {
    const parsed = JSON.parse(body);
    if (typeof parsed?.error === "string" && parsed.error !== "") {
      message = parsed.error;
    }
  } catch {
  }
  return new APIError(code, message, response.status);
}

// src/client.ts
var SaasAgentClient = class {
  constructor(options) {
    if (!options.baseUrl) {
      throw new Error("SaasAgentClient: baseUrl is required");
    }
    this.baseUrl = options.baseUrl;
    this.apiKey = options.apiKey ?? "";
    this.timeoutMs = options.timeoutMs ?? 3e4;
    this.fetchImpl = options.fetch ?? globalThis.fetch;
    if (!this.fetchImpl) {
      throw new Error(
        "SaasAgentClient: no fetch implementation available. Use Node.js 18+, or pass a `fetch` implementation via ClientOptions."
      );
    }
  }
  async doRequest(opts) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), this.timeoutMs);
    const headers = {
      "Content-Type": "application/json"
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
        body: opts.body !== void 0 ? JSON.stringify(opts.body) : void 0,
        signal: controller.signal
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
  async login(email, password) {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/auth/login",
      body: { email, password }
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
  }
  /** Refreshes the access token. */
  async refreshToken(refreshToken) {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/auth/refresh",
      body: { refresh_token: refreshToken }
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
  }
  // ============================================================
  // Tenant
  // ============================================================
  /** Creates a new tenant (root only). */
  async createTenant(tenantID, name) {
    const resp = await this.doRequest({
      method: "POST",
      path: "/api/admin/tenants",
      body: { tenant_id: tenantID, name }
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.tenant;
  }
  // ============================================================
  // User
  // ============================================================
  /** Creates a new user in a tenant. */
  async createUser(tenantID, email, password, roleID) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/users`,
      body: { email, password, role_id: roleID }
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.user;
  }
  // ============================================================
  // Usage
  // ============================================================
  /** Gets usage events for a tenant. Pass `limit <= 0` to omit the limit query param. */
  async getUsage(tenantID, limit) {
    let path = `/api/tenants/${tenantID}/usage`;
    if (limit > 0) {
      path += `?limit=${limit}`;
    }
    const resp = await this.doRequest({ method: "GET", path });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.events;
  }
  // ============================================================
  // Agent
  // ============================================================
  /** Creates a new agent. */
  async createAgent(tenantID, name, description, capabilities, metadata) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/agents`,
      body: { name, description, capabilities, metadata }
    });
    if (resp.status !== 201) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.agent;
  }
  /** Lists agents for a tenant. */
  async listAgents(tenantID) {
    const resp = await this.doRequest({
      method: "GET",
      path: `/api/tenants/${tenantID}/agents`
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.agents;
  }
  // ============================================================
  // Tool
  // ============================================================
  /** Creates a new tool. */
  async createTool(tenantID, name, description, toolType, config) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/tools`,
      body: { name, description, type: toolType, config }
    });
    if (resp.status !== 201) {
      throw await parseError(resp);
    }
    const result = await resp.json();
    return result.tool;
  }
  /** Executes a tool. */
  async executeTool(tenantID, toolID, params) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/tools/${toolID}/execute`,
      body: { params }
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
  }
  // ============================================================
  // MCP
  // ============================================================
  /** Sends an MCP query. */
  async mcpQuery(tenantID, req) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/mcp/query`,
      body: req
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
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
  async mcpToolCall(tenantID, toolName, params) {
    const toolCall = params !== void 0 ? { tool_name: toolName, params } : { tool_name: toolName };
    return this.mcpQuery(tenantID, { tool_call: toolCall });
  }
  // ============================================================
  // GDPR
  // ============================================================
  /** Soft-deletes a tenant (GDPR right to erasure). */
  async softDeleteTenant(tenantID) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/data/delete`
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
  }
  /** Restores a soft-deleted tenant. */
  async restoreTenant(tenantID) {
    const resp = await this.doRequest({
      method: "POST",
      path: `/api/tenants/${tenantID}/data/restore`
    });
    if (resp.status !== 200) {
      throw await parseError(resp);
    }
    return await resp.json();
  }
};
export {
  APIError,
  SaasAgentClient,
  apiErrorCodeForStatus
};
//# sourceMappingURL=index.js.map