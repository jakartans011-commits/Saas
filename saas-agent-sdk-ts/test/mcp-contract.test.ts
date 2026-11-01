import { describe, expect, it } from "vitest";
import type { MCPRequest, MCPResponse, MCPToolCall } from "../src/index.js";

/**
 * Pins down the exact JSON shape the SDK produces/consumes for MCP
 * types, mirroring sdk/go/client_mcp_contract_test.go.
 *
 * `MCPRequest`/`MCPToolCall`/`MCPResponse` are hand-mirrored copies
 * of mcp.MCPRequest/mcp.MCPToolCall/mcp.MCPResponse on the server —
 * nothing enforces the two stay in sync, so these tests exist to
 * fail loudly (rather than silently against a live server) if a
 * field name or omit-when-absent behavior ever drifts.
 */
describe("MCPRequest wire shape", () => {
  it("query mode serializes only `query`", () => {
    const req: MCPRequest = { query: "check health" };
    expect(JSON.stringify(req)).toBe('{"query":"check health"}');
  });

  it("tool-call mode serializes only `tool_call`", () => {
    const req: MCPRequest = { tool_call: { tool_name: "health-check" } };
    expect(JSON.stringify(req)).toBe('{"tool_call":{"tool_name":"health-check"}}');
  });

  it("tool-call mode with params includes `params` under `tool_call`", () => {
    const req: MCPRequest = {
      tool_call: { tool_name: "echo", params: { text: "hi" } },
    };
    expect(JSON.stringify(req)).toBe(
      '{"tool_call":{"tool_name":"echo","params":{"text":"hi"}}}',
    );
  });

  it("includes context alongside query, omits tool_call/params when unset", () => {
    const req: MCPRequest = {
      query: "check health",
      context: { session_id: "abc" },
    };
    const decoded = JSON.parse(JSON.stringify(req));

    expect(decoded.query).toBe("check health");
    expect(decoded.context).toEqual({ session_id: "abc" });
    expect("tool_call" in decoded).toBe(false);
    expect("params" in decoded).toBe(false);
  });

  it("an empty request omits all fields", () => {
    const req: MCPRequest = {};
    expect(JSON.stringify(req)).toBe("{}");
  });
});

describe("MCPToolCall wire shape", () => {
  it("serializes tool_name and params together", () => {
    const tc: MCPToolCall = { tool_name: "metadata-lookup", params: { key: "region" } };
    expect(JSON.stringify(tc)).toBe('{"tool_name":"metadata-lookup","params":{"key":"region"}}');
  });

  it("omits params when not set", () => {
    const tc: MCPToolCall = { tool_name: "health-check" };
    expect(JSON.stringify(tc)).toBe('{"tool_name":"health-check"}');
  });
});

describe("MCPResponse decoding", () => {
  it("round-trips a full server-shaped payload", () => {
    const payload = `{
      "response": "healthy",
      "tools_used": ["health-check"],
      "execution_time_ms": 12,
      "metadata": {
        "tenant_id": "acme-corp",
        "user_id": "user-1",
        "tool_id": "tool-1",
        "tool_name": "health-check",
        "type": "health_check",
        "request_id": "9f4d9b0a-1c2e-4a6f-8b1a-000000000000"
      }
    }`;

    const resp = JSON.parse(payload) as MCPResponse;

    expect(resp.response).toBe("healthy");
    expect(resp.tools_used).toEqual(["health-check"]);
    expect(resp.execution_time_ms).toBe(12);
    expect(resp.metadata.request_id).toBe("9f4d9b0a-1c2e-4a6f-8b1a-000000000000");
    expect(resp.metadata.tenant_id).toBe("acme-corp");
  });

  it("tolerates a minimal payload with omitted optional fields", () => {
    const payload = `{"response": "ok", "execution_time_ms": 0}`;
    const resp = JSON.parse(payload) as MCPResponse;

    expect(resp.response).toBe("ok");
    expect(resp.tools_used).toBeUndefined();
    expect(resp.metadata).toBeUndefined();
  });
});
