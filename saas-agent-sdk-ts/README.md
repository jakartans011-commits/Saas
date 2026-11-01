# @saas-agent/sdk (TypeScript / JavaScript)

TypeScript/JavaScript SDK for the SaaS Agent API. This is a **1:1 port**
of the Go SDK at `sdk/go/client.go` — same methods, same request
shapes, same error taxonomy, so behavior (and the Go SDK's test
suite) transfers directly.

- Runtime: Node.js 18+ (uses native `fetch`, no HTTP dependency)
- Module format: dual ESM + CJS build (`dist/index.js` / `dist/index.cjs`), with `.d.ts` types for both
- Scope: identical to the Go SDK baseline (auth, tenant, user, agent,
  tool, MCP query/tool-call, GDPR delete/restore). Other server
  routes (projects, API keys, billing, admin agents/tools, audit
  export, logout) exist on the server but are **out of scope** here
  because they aren't in `client.go` either — extend both SDKs
  together if/when needed.

## Install (once published)

```bash
npm install @saas-agent/sdk
```

## Usage

```ts
// ESM / TypeScript
import { SaasAgentClient } from "@saas-agent/sdk";

const client = new SaasAgentClient({
  baseUrl: "https://api.example.com",
  apiKey: process.env.SAAS_AGENT_API_KEY, // optional
});

const usage = await client.getUsage("acme-corp", 50);

const result = await client.mcpToolCall("acme-corp", "health-check", { foo: "bar" });
console.log(result.response);
```

```js
// CommonJS
const { SaasAgentClient } = require("@saas-agent/sdk");
const client = new SaasAgentClient({ baseUrl: "https://api.example.com", apiKey: "..." });
```

## Error handling

Every non-2xx response throws an `APIError` with a stable `code`
(`invalid_request`, `unauthenticated`, `forbidden`, `tool_not_found`,
`internal_error`, `unknown_error`), `statusCode`, and `message` —
mirroring the Go SDK's `*APIError`:

```ts
import { APIError } from "@saas-agent/sdk";

try {
  await client.mcpQuery("acme-corp", { query: "..." });
} catch (err) {
  if (err instanceof APIError) {
    console.error(err.code, err.statusCode, err.message);
  }
}
```

## API surface (mirrors sdk/go/client.go)

| Go method | TS method |
|---|---|
| `Login` | `login(email, password)` |
| `RefreshToken` | `refreshToken(refreshToken)` |
| `CreateTenant` | `createTenant(tenantID, name)` |
| `CreateUser` | `createUser(tenantID, email, password, roleID)` |
| `GetUsage` | `getUsage(tenantID, limit)` |
| `CreateAgent` | `createAgent(tenantID, name, description, capabilities, metadata)` |
| `ListAgents` | `listAgents(tenantID)` |
| `CreateTool` | `createTool(tenantID, name, description, toolType, config)` |
| `ExecuteTool` | `executeTool(tenantID, toolID, params)` |
| `MCPQuery` | `mcpQuery(tenantID, req)` |
| `MCPToolCall` | `mcpToolCall(tenantID, toolName, params?)` |
| `SoftDeleteTenant` | `softDeleteTenant(tenantID)` |
| `RestoreTenant` | `restoreTenant(tenantID)` |

## Development

```bash
npm install
npm run typecheck   # tsc --noEmit
npm test            # vitest — ports of client_test.go, client_tenant_auth_test.go, client_mcp_contract_test.go
npm run build        # tsup -> dist/index.js, dist/index.cjs, dist/index.d.ts, dist/index.d.cts
```
