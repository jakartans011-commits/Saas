# TrailSeal - Audit Log & MCP Gateway Platform

SaaS multi-tenant untuk audit logging, MCP gateway, dan LLM/agent integration.

## 🚀 Features

- 🔐 **JWT Authentication** with refresh tokens
- 🏢 **Multi-Tenant** isolation with RBAC + Capability-based authorization
- 📊 **Usage Metering** for billing and quota
- 📝 **Audit Logging** for compliance and forensics
- 🇪🇺 **GDPR tooling** (right to erasure, right to access)
- 🚀 **Rate Limiting** (1000 req/60s per tenant)
- 🤖 **Agent & Tool Management**
- 🔌 **MCP Gateway** for LLM/Agent integration
- 🔗 **Immutable Event Chain** with cryptographic verification

## 📋 Quick Start

### 1. Start Server

```bash
go run ./cmd/saas-api
```
