/**
 * APIError is a typed error returned by the SaaS Agent API.
 *
 * The REST error body currently only carries a message string
 * (`{"error": "..."}`) — there is no explicit taxonomy code on the
 * wire for this transport (unlike the separate JSON-RPC layer).
 * `code` is therefore derived from `statusCode` using the same
 * mapping the server's writeMCPError uses, so callers get a stable,
 * switchable value instead of having to string-match `message`.
 *
 * Mirrors sdk/go/client.go's APIError 1:1.
 */
declare class APIError extends Error {
    readonly code: string;
    readonly statusCode: number;
    constructor(code: string, message: string, statusCode: number);
    /**
     * Formats the error the same way Go's `(*APIError).Error()` does:
     * `API error [<code>]: <message> (status <statusCode>)`.
     */
    toString(): string;
}
/**
 * Derives a stable taxonomy code from the HTTP status code,
 * mirroring the server-side status mapping used by
 * `apiErrorCodeForStatus` in sdk/go/client.go.
 */
declare function apiErrorCodeForStatus(statusCode: number): string;

/**
 * Type definitions for the SaaS Agent SDK.
 *
 * These mirror the Go SDK types 1:1 (see sdk/go/client.go). Field
 * names use the wire (snake_case JSON) representation so that
 * `JSON.parse`d server responses can be used directly without extra
 * mapping — the same way the Go structs rely on `json:"..."` tags.
 */
/** Login credentials. */
interface LoginRequest {
    email: string;
    password: string;
}
/** Response returned by a successful login or token refresh. */
interface LoginResponse {
    access_token: string;
    refresh_token: string;
    token_type: string;
    expires_in: number;
}
interface Tenant {
    id: string;
    name: string;
    status: string;
    /** ISO 8601 timestamp string, as returned by the server. */
    created_at: string;
    /** ISO 8601 timestamp string, present only when the tenant is soft-deleted. */
    deleted_at?: string | null;
}
interface User {
    id: string;
    tenant_id: string;
    email: string;
    role_id: string;
    status: string;
}
interface UsageEvent {
    id: string;
    tenant_id: string;
    event_type: string;
    metadata: Record<string, string>;
    /** ISO 8601 timestamp string, as returned by the server. */
    timestamp: string;
}
interface Agent {
    id: string;
    tenant_id: string;
    name: string;
    description: string;
    status: string;
    capabilities: string[];
    metadata: Record<string, string>;
    /** ISO 8601 timestamp string, as returned by the server. */
    created_at: string;
    /** ISO 8601 timestamp string, as returned by the server. */
    updated_at: string;
}
interface Tool {
    id: string;
    tenant_id: string;
    name: string;
    description: string;
    type: string;
    config: Record<string, string>;
    /** ISO 8601 timestamp string, as returned by the server. */
    created_at: string;
    /** ISO 8601 timestamp string, as returned by the server. */
    updated_at: string;
}
/**
 * An explicit tool-call request, mirroring mcp.MCPToolCall on the
 * server. When set on {@link MCPRequest}, it takes priority over
 * `query` for tool resolution.
 */
interface MCPToolCall {
    tool_name: string;
    params?: Record<string, unknown>;
}
/**
 * An MCP query request.
 *
 * Two modes are supported, mirroring the server's mcp.MCPRequest:
 *
 * 1. Query mode: set `query` (and optionally `params`/`context`).
 * 2. Explicit tool-call mode: set `tool_call`. If `tool_call` is
 *    set, it takes priority over `query` on the server side.
 */
interface MCPRequest {
    query?: string;
    context?: Record<string, string>;
    params?: Record<string, unknown>;
    tool_call?: MCPToolCall;
}
/** Response to an MCP query. */
interface MCPResponse {
    response: string;
    tools_used: string[];
    execution_time_ms: number;
    metadata: Record<string, string>;
}
/** GDPR deletion/restoration status for a tenant. */
interface DeletionStatus {
    status: string;
    tenant_id: string;
    /** ISO 8601 timestamp string, present only once deletion has completed. */
    deleted_at?: string | null;
    message: string;
}

/** Options accepted by {@link SaasAgentClient.constructor}. */
interface ClientOptions {
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
/**
 * Client for the SaaS Agent API.
 *
 * This is a 1:1 TypeScript port of the Go SDK (sdk/go/client.go):
 * every exported method here has the same name (in camelCase),
 * parameters, request shape, and error behavior as its Go
 * counterpart.
 */
declare class SaasAgentClient {
    readonly baseUrl: string;
    readonly apiKey: string;
    private readonly timeoutMs;
    private readonly fetchImpl;
    constructor(options: ClientOptions);
    private doRequest;
    /** Authenticates a user. */
    login(email: string, password: string): Promise<LoginResponse>;
    /** Refreshes the access token. */
    refreshToken(refreshToken: string): Promise<LoginResponse>;
    /** Creates a new tenant (root only). */
    createTenant(tenantID: string, name: string): Promise<Tenant>;
    /** Creates a new user in a tenant. */
    createUser(tenantID: string, email: string, password: string, roleID: string): Promise<User>;
    /** Gets usage events for a tenant. Pass `limit <= 0` to omit the limit query param. */
    getUsage(tenantID: string, limit: number): Promise<UsageEvent[]>;
    /** Creates a new agent. */
    createAgent(tenantID: string, name: string, description: string, capabilities: string[], metadata: Record<string, string>): Promise<Agent>;
    /** Lists agents for a tenant. */
    listAgents(tenantID: string): Promise<Agent[]>;
    /** Creates a new tool. */
    createTool(tenantID: string, name: string, description: string, toolType: string, config: Record<string, string>): Promise<Tool>;
    /** Executes a tool. */
    executeTool(tenantID: string, toolID: string, params: Record<string, unknown>): Promise<Record<string, unknown>>;
    /** Sends an MCP query. */
    mcpQuery(tenantID: string, req: MCPRequest): Promise<MCPResponse>;
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
    mcpToolCall(tenantID: string, toolName: string, params?: Record<string, unknown>): Promise<MCPResponse>;
    /** Soft-deletes a tenant (GDPR right to erasure). */
    softDeleteTenant(tenantID: string): Promise<DeletionStatus>;
    /** Restores a soft-deleted tenant. */
    restoreTenant(tenantID: string): Promise<DeletionStatus>;
}

export { APIError, type Agent, type ClientOptions, type DeletionStatus, type LoginRequest, type LoginResponse, type MCPRequest, type MCPResponse, type MCPToolCall, SaasAgentClient, type Tenant, type Tool, type UsageEvent, type User, apiErrorCodeForStatus };
