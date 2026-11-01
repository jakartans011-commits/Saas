package mcp

import (
	"fmt"
	"time"

	"trailseal/agent"
	"trailseal/audit"
	"trailseal/tenant"
)

// ============================================================
// MCP GATEWAY
// ============================================================

type Gateway struct {
	agentStore *agent.BoltAgentStore
	auditStore audit.Store

	// quota — OPSIONAL. nil berarti quota TIDAK di-enforce (backward
	// compatible untuk caller lama yang belum di-update ke
	// SetQuotaEnforcer). WAJIB di-set sebelum production/commercial
	// launch — lihat app.go: billingService/tenantStore sudah
	// implement tenant.QuotaEnforcer secara otomatis (BoltTenantStore).
	quota tenant.QuotaEnforcer
}

func NewGateway(agentStore *agent.BoltAgentStore, auditStore audit.Store) *Gateway {
	return &Gateway{
		agentStore: agentStore,
		auditStore: auditStore,
	}
}

// SetQuotaEnforcer mengaktifkan quota enforcement pada gateway yang
// sudah dibuat lewat NewGateway. Dipisah dari constructor supaya
// TIDAK mengubah signature NewGateway yang sudah dipakai di
// app.go/test lain — cukup panggil ini sekali setelah NewGateway.
func (g *Gateway) SetQuotaEnforcer(q tenant.QuotaEnforcer) {
	g.quota = q
}

func (g *Gateway) Query(
	tenantID string,
	userID string,
	req *MCPRequest,
) (*MCPResponse, error) {

	start := time.Now()

	requestID := newRequestID()

	if g == nil || g.agentStore == nil {
		return nil, InternalMCPError(
			fmt.Errorf("mcp gateway is not initialized"),
		)
	}

	if tenantID == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"tenant_id is required",
			nil,
		)
	}

	if userID == "" {
		return nil, newMCPError(
			MCPErrorInvalidRequest,
			"user_id is required",
			nil,
		)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// QUOTA CHECK — WAJIB sebelum tool discovery/execution.
	//
	// Atomic check-and-increment (lihat BoltTenantStore.TryConsumeQuota
	// / InMemoryTenantStore.TryConsumeQuota) — aman dari race
	// condition saat banyak request paralel masuk untuk tenant yang
	// sama. 1 MCP query = 1 unit kuota.
	// --------------------------------------------------------
	if g.quota != nil {
		allowed, _, err := g.quota.TryConsumeQuota(tenantID, 1)
		if err != nil {
			return nil, InternalMCPError(
				fmt.Errorf("quota check failed: %w", err),
			)
		}
		if !allowed {
			return nil, newMCPError(
				MCPErrorQuotaExceeded,
				"monthly event quota exceeded",
				nil,
			)
		}
	}

	tools, err := g.agentStore.ListTools(tenantID)
	if err != nil {
		return nil, InternalMCPError(
			fmt.Errorf("failed to list tools: %w", err),
		)
	}

	tool, err := resolveTool(req, tools)
	if err != nil {
		return nil, ensureMCPError(err)
	}

	params := req.Params

	if req.ToolCall != nil && req.ToolCall.Params != nil {
		params = req.ToolCall.Params
	} else if req.ToolCall == nil && params == nil {
		params = map[string]interface{}{
			"message": req.Query,
		}
	}

	// --------------------------------------------------------
	// FIX: AuditStore sebelumnya TIDAK pernah diteruskan ke
	// execution layer walau Gateway sudah memilikinya lewat
	// NewGateway(agentStore, auditStore).
	//
	// Akibatnya setiap tool bertipe ToolTypeAuditLookup SELALU
	// gagal dengan "audit lookup unavailable: auditStore not
	// configured" (lihat executor.go: executeAuditLookup),
	// walaupun auditStore sebenarnya tersedia.
	//
	// Field ini diteruskan sebagai read-only dependency untuk
	// tool yang butuh query audit log — bukan untuk menulis
	// audit trail MCP call itu sendiri (itu ditangani terpisah
	// di HTTP layer lewat middleware.AuditLogger).
	// --------------------------------------------------------
	execCtx := MCPExecutionContext{
		TenantID:   tenantID,
		UserID:     userID,
		Context:    cloneContext(req.Context),
		RequestID:  requestID,
		AuditStore: g.auditStore,
	}

	result, err := executeTool(
		tool,
		params,
		execCtx,
	)
	if err != nil {
		return nil, ensureMCPError(err)
	}

	executionMs := time.Since(start).Milliseconds()

	return &MCPResponse{
		Response:        formatExecutionResult(result),
		ToolsUsed:       []string{tool.Name},
		ExecutionTimeMs: executionMs,
		Metadata: map[string]string{
			"tenant_id":  tenantID,
			"user_id":    userID,
			"tool_id":    tool.ID,
			"tool_name":  tool.Name,
			"type":       string(tool.Type),
			"request_id": requestID,
		},
	}, nil
}

func cloneContext(
	input map[string]string,
) map[string]string {

	if input == nil {
		return nil
	}

	output := make(map[string]string, len(input))

	for key, value := range input {
		output[key] = value
	}

	return output
}