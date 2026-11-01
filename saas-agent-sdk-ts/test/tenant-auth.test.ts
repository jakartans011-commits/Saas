import { describe, expect, it, vi } from "vitest";
import { SaasAgentClient } from "../src/index.js";

function fakeFetch() {
  const calls: { url: string; init: RequestInit }[] = [];
  const impl = vi.fn(async (url: string | URL, init?: RequestInit) => {
    calls.push({ url: String(url), init: init ?? {} });
    return new Response(JSON.stringify({ response: "ok" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  return { impl: impl as unknown as typeof fetch, calls };
}

describe("tenant path propagation", () => {
  it("mcpQuery uses a tenant-scoped path", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await client.mcpQuery("acme-corp", { query: "check health" });

    expect(new URL(calls[0].url).pathname).toBe("/api/tenants/acme-corp/mcp/query");
  });

  it("mcpToolCall uses a tenant-scoped path", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await client.mcpToolCall("tenant-xyz", "health-check");

    expect(new URL(calls[0].url).pathname).toBe("/api/tenants/tenant-xyz/mcp/query");
  });

  it("reflects the tenantID given on each call, not a stale/hardcoded one", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    for (const tenantID of ["tenant-a", "tenant-b", "tenant-c"]) {
      await client.mcpQuery(tenantID, { query: "ping" });
    }

    expect(calls.map((c) => new URL(c.url).pathname)).toEqual([
      "/api/tenants/tenant-a/mcp/query",
      "/api/tenants/tenant-b/mcp/query",
      "/api/tenants/tenant-c/mcp/query",
    ]);
  });
});

describe("auth propagation", () => {
  it("sets a Bearer Authorization header when apiKey is set", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", apiKey: "secret-token", fetch: impl });

    await client.mcpQuery("acme-corp", { query: "check health" });

    const headers = new Headers(calls[0].init.headers);
    expect(headers.get("Authorization")).toBe("Bearer secret-token");
  });

  it("omits the Authorization header when apiKey is empty", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await client.mcpQuery("acme-corp", { query: "check health" });

    const headers = new Headers(calls[0].init.headers);
    expect(headers.has("Authorization")).toBe(false);
  });

  it("sets Content-Type: application/json", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({ baseUrl: "http://test", fetch: impl });

    await client.mcpQuery("acme-corp", { query: "check health" });

    const headers = new Headers(calls[0].init.headers);
    expect(headers.get("Content-Type")).toBe("application/json");
  });

  it("propagates tenant path, auth header, and body together", async () => {
    const { impl, calls } = fakeFetch();
    const client = new SaasAgentClient({
      baseUrl: "http://test",
      apiKey: "tenant-scoped-token",
      fetch: impl,
    });

    await client.mcpToolCall("tenant-42", "echo", { text: "hi" });

    const headers = new Headers(calls[0].init.headers);
    const body = JSON.parse(String(calls[0].init.body));

    expect(new URL(calls[0].url).pathname).toBe("/api/tenants/tenant-42/mcp/query");
    expect(headers.get("Authorization")).toBe("Bearer tenant-scoped-token");
    expect(body.tool_call.tool_name).toBe("echo");
  });
});
