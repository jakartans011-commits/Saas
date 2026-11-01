/**
 * Type definitions for the SaaS Agent SDK.
 *
 * These mirror the Go SDK types 1:1 (see sdk/go/client.go). Field
 * names use the wire (snake_case JSON) representation so that
 * `JSON.parse`d server responses can be used directly without extra
 * mapping — the same way the Go structs rely on `json:"..."` tags.
 */

// ============================================================
// Auth
// ============================================================

/** Login credentials. */
export interface LoginRequest {
  email: string;
  password: string;
}

/** Response returned by a successful login or token refresh. */
export interface LoginResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

// ============================================================
// Tenant
// ============================================================

export interface Tenant {
  id: string;
  name: string;
  status: string;
  /** ISO 8601 timestamp string, as returned by the server. */
  created_at: string;
  /** ISO 8601 timestamp string, present only when the tenant is soft-deleted. */
  deleted_at?: string | null;
}

// ============================================================
// User
// ============================================================

export interface User {
  id: string;
  tenant_id: string;
  email: string;
  role_id: string;
  status: string;
}

// ============================================================
// Usage
// ============================================================

export interface UsageEvent {
  id: string;
  tenant_id: string;
  event_type: string;
  metadata: Record<string, string>;
  /** ISO 8601 timestamp string, as returned by the server. */
  timestamp: string;
}

// ============================================================
// Agent
// ============================================================

export interface Agent {
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

// ============================================================
// Tool
// ============================================================

export interface Tool {
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

// ============================================================
// MCP
// ============================================================

/**
 * An explicit tool-call request, mirroring mcp.MCPToolCall on the
 * server. When set on {@link MCPRequest}, it takes priority over
 * `query` for tool resolution.
 */
export interface MCPToolCall {
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
export interface MCPRequest {
  query?: string;
  context?: Record<string, string>;
  params?: Record<string, unknown>;
  tool_call?: MCPToolCall;
}

/** Response to an MCP query. */
export interface MCPResponse {
  response: string;
  tools_used: string[];
  execution_time_ms: number;
  metadata: Record<string, string>;
}

// ============================================================
// GDPR
// ============================================================

/** GDPR deletion/restoration status for a tenant. */
export interface DeletionStatus {
  status: string;
  tenant_id: string;
  /** ISO 8601 timestamp string, present only once deletion has completed. */
  deleted_at?: string | null;
  message: string;
}
