import { afterEach, describe, expect, it, vi } from "vitest";
import { APIError, SaasAgentClient } from "../src/index.js";

/**
 * Builds a fake `fetch` that captures the last request and responds
 * with the given status/body, mirroring the httptest.Server used in
 * sdk/go/client_test.go.
 */
function fakeFetch(status: number, body: unknown, isJson = true) {
  const calls: { url: string; init: RequestInit }[] = [];
  const impl = vi.fn(async (url: string | URL, init?: RequestInit) => {
    calls.push({ url: String(url), init: init ?? {} });
    return new Response(isJson ? JSON.stringify(body) : (body as string), {
      status,
      headers: isJson ? { "Content-Type": "application/json" } : undefined,
    });
  });
  return { impl: impl as unknown as typeof fetch, calls };
}

describe("SaasAgentClient.mcpToolCall", () => {
  it("sends tool_call body without a top-level query field", async () => {
    const { impl, calls } = fakeFetch(200, {
      response: "ok",
      tools_used: ["health-check"],
      execution_time_ms: 1,
      metadata: { request_id: "abc-123" },
    });

    const client = new SaasAgentClient({ baseUrl: "http://test", apiKey: "", fetch: impl });
    const resp = await client.mcpToolCall("tenant-1", "health-check", { foo: "bar" });

    const body = JSON.parse(String(calls[0].init.body));
    expect(body.query).toBeUndefined();
    expect(body.tool_call.tool_name).toBe("health-check");
    expect(body.tool_call.params).toEqual({ foo: "bar" });

    expect(resp.response).toBe("ok");
    expect(resp.tools_used).toEqual(["health-check"]);
    expect(resp.metadata.request_id).toBe("abc-123");
  });

  it("omits params when not provided", async () => {
    const { impl, calls } = fakeFetch(200, { response: "ok" });
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await client.mcpToolCall("tenant-1", "health-check");

    const body = JSON.parse(String(calls[0].init.body));
    expect("params" in body.tool_call).toBe(false);
  });

  it("propagates server errors as APIError", async () => {
    const { impl } = fakeFetch(404, { error: "tool not found" });
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await expect(client.mcpToolCall("tenant-1", "missing-tool")).rejects.toMatchObject({
      code: "tool_not_found",
      message: "tool not found",
      statusCode: 404,
    });
  });
});

describe("parseError / APIError", () => {
  const cases: { name: string; status: number; wantCode: string }[] = [
    { name: "bad request", status: 400, wantCode: "invalid_request" },
    { name: "unauthorized", status: 401, wantCode: "unauthenticated" },
    { name: "forbidden", status: 403, wantCode: "forbidden" },
    { name: "not found", status: 404, wantCode: "tool_not_found" },
    { name: "internal error", status: 500, wantCode: "internal_error" },
    { name: "unmapped status", status: 418, wantCode: "unknown_error" },
  ];

  for (const tc of cases) {
    it(`maps ${tc.name} (${tc.status}) to ${tc.wantCode}`, async () => {
      const { impl } = fakeFetch(tc.status, { error: "boom" });
      const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

      await expect(client.getUsage("tenant-1", 0)).rejects.toMatchObject({
        code: tc.wantCode,
        statusCode: tc.status,
        message: "boom",
      });
    });
  }

  it("falls back to the raw body when it isn't the {error} shape", async () => {
    const { impl } = fakeFetch(500, "plain text failure, not json", false);
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await expect(client.getUsage("tenant-1", 0)).rejects.toMatchObject({
      code: "internal_error",
      message: "plain text failure, not json",
    });
  });

  it("formats APIError.toString with all fields", () => {
    const err = new APIError("tool_not_found", "no such tool", 404);
    expect(err.toString()).toBe("API error [tool_not_found]: no such tool (status 404)");
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});
