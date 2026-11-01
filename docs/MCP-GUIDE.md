# 🚀 MCP Integration Guide - Quick Start

Panduan cepat untuk menggunakan MCP (Model Context Protocol) Gateway di TrailSeal.

---

## TL;DR

```bash
# 1. Buat tool
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/tools \
  -H "Authorization: Bearer {token}" \
  -d '{"name":"echo","type":"echo"}'

# 2. Query Mode
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/mcp/query \
  -H "Authorization: Bearer {token}" \
  -d '{"query":"echo hello"}'

# 3. ToolCall Mode
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/mcp/query \
  -H "Authorization: Bearer {token}" \
  -d '{"tool_call":{"tool_name":"echo","params":{"message":"Hello!"}}}'
```

---

## ✅ Format Request

| Mode              | Format                                                                  |
| ----------------- | ----------------------------------------------------------------------- |
| **Query Mode**    | `{"query": "echo hello"}`                                               |
| **ToolCall Mode** | `{"tool_call": {"tool_name": "echo", "params": {"message": "Hello!"}}}` |

---

### Query Mode (Natural Language)

Kirim pertanyaan atau perintah dalam bahasa natural.

```json
{
  "query": "echo hello"
}
```

**Response:**

```json
{
  "response": "Echo: echo hello",
  "execution_time_ms": 12,
  "metadata": {
    "tenant_id": "test-company",
    "tool_name": "echo",
    "request_id": "abc-123"
  }
}
```

---

### ToolCall Mode (Explicit Tool)

Panggil tool secara eksplisit dengan nama dan parameter.

```json
{
  "tool_call": {
    "tool_name": "echo",
    "params": {
      "message": "Hello World!"
    }
  }
}
```

**Response:**

```json
{
  "response": "Echo: Hello World!",
  "execution_time_ms": 8,
  "metadata": {
    "tenant_id": "test-company",
    "tool_name": "echo",
    "request_id": "def-456"
  }
}
```

---

## ❌ JANGAN PAKAI INI!

**Salah:** Kirim format JSON-RPC ke endpoint ini.

```json
❌ {"jsonrpc":"2.0","method":"tools/list"}
❌ {"jsonrpc":"2.0","method":"tools/call",...}
```

**JSON-RPC hanya untuk MCP Server internal, BUKAN untuk endpoint `/api/tenants/:id/mcp/query`!**

---

## 📊 Audit Log

Semua MCP calls otomatis tercatat di audit log.

```bash
curl -X GET http://localhost:8090/api/tenants/{tenant-id}/audit?limit=50 \
  -H "Authorization: Bearer {token}"
```

**Contoh Response:**

```json
{
  "tenant_id": "test-company",
  "limit": 50,
  "events": [
    {
      "id": "abc-123",
      "tenant_id": "test-company",
      "user_id": "user-123",
      "action": "POST /api/tenants/test-company/mcp/query",
      "resource": "/api/tenants/test-company/mcp/query",
      "outcome": "success",
      "timestamp": "2026-08-29T00:00:00Z",
      "metadata": {
        "method": "POST",
        "path": "/api/tenants/test-company/mcp/query",
        "status": "200",
        "ip": "127.0.0.1",
        "user_agent": "curl/7.68.0"
      }
    }
  ],
  "count": 1
}
```

---

## 🔧 Full Example (PowerShell)

```powershell
# ============================================================
# 1. LOGIN
# ============================================================
$login = Invoke-RestMethod -Uri "http://localhost:8090/api/auth/login" `
  -Method Post `
  -Body '{"email":"test@example.com","password":"Test123!"}' `
  -ContentType "application/json"

$token = $login.access_token
$tenantId = $login.tenant.id

Write-Host "✅ Login SUCCESS!" -ForegroundColor Green
Write-Host "Tenant ID: $tenantId"
Write-Host ""

# ============================================================
# 2. SET HEADERS
# ============================================================
$headers = @{
  "Authorization" = "Bearer $token"
  "Content-Type" = "application/json"
}

# ============================================================
# 3. BUAT TOOL
# ============================================================
$tool = Invoke-RestMethod -Uri "http://localhost:8090/api/tenants/$tenantId/tools" `
  -Method Post `
  -Headers $headers `
  -Body '{"name":"echo","type":"echo"}'

Write-Host "✅ Tool created: $($tool.tool.id)" -ForegroundColor Green
Write-Host ""

# ============================================================
# 4. MCP QUERY MODE
# ============================================================
$mcp = Invoke-RestMethod -Uri "http://localhost:8090/api/tenants/$tenantId/mcp/query" `
  -Method Post `
  -Headers $headers `
  -Body '{"query":"echo hello"}'

Write-Host "✅ MCP Query Mode SUCCESS!" -ForegroundColor Green
Write-Host "Response: $($mcp.response)"
Write-Host ""

# ============================================================
# 5. MCP TOOLCALL MODE
# ============================================================
$mcp = Invoke-RestMethod -Uri "http://localhost:8090/api/tenants/$tenantId/mcp/query" `
  -Method Post `
  -Headers $headers `
  -Body '{"tool_call":{"tool_name":"echo","params":{"message":"Hello World!"}}}'

Write-Host "✅ MCP ToolCall Mode SUCCESS!" -ForegroundColor Green
Write-Host "Response: $($mcp.response)"
Write-Host ""

# ============================================================
# 6. CEK AUDIT LOG
# ============================================================
$audit = Invoke-RestMethod -Uri "http://localhost:8090/api/tenants/$tenantId/audit?limit=50" `
  -Method Get `
  -Headers $headers

Write-Host "✅ Audit Log accessed!" -ForegroundColor Green
Write-Host "Total events: $($audit.events.Count)" -ForegroundColor Yellow
Write-Host ""
Write-Host "📋 AUDIT LOG:" -ForegroundColor Cyan
$audit.events | Format-Table id, action, outcome -AutoSize
```

---

## 🔧 Full Example (cURL)

```bash
# ============================================================
# 1. LOGIN
# ============================================================
curl -X POST http://localhost:8090/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"Test123!"}'

# ============================================================
# 2. BUAT TOOL
# ============================================================
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/tools \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{"name":"echo","type":"echo"}'

# ============================================================
# 3. MCP QUERY MODE
# ============================================================
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/mcp/query \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{"query":"echo hello"}'

# ============================================================
# 4. MCP TOOLCALL MODE
# ============================================================
curl -X POST http://localhost:8090/api/tenants/{tenant-id}/mcp/query \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{"tool_call":{"tool_name":"echo","params":{"message":"Hello World!"}}}'
```

---

## 📋 Checklist

| Step | Action               | Status |
| ---- | -------------------- | ------ |
| 1    | Login dapatkan token | ⬜     |
| 2    | Buat tool di tenant  | ⬜     |
| 3    | Test Query Mode      | ⬜     |
| 4    | Test ToolCall Mode   | ⬜     |
| 5    | Cek audit log        | ⬜     |

---

## 📚 Related Documentation

- [API Reference](./openapi.yaml)
- [Architecture](./rfc/ARCHITECTURE.md)
- [README](../../README.md)
