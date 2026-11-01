package main

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"trailseal/agent"
	"trailseal/middleware"
)

// ============================================================
// AGENT HANDLERS
// ============================================================

// CreateAgentHandler — POST /api/tenants/:id/agents
func CreateAgentHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		var req struct {
			Name         string            `json:"name"`
			Description  string            `json:"description"`
			Capabilities []string          `json:"capabilities"`
			Metadata     map[string]string `json:"metadata"`
		}

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		if req.Name == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "name is required",
			})
		}

		newAgent, err := agent.RegisterAgent(
			agentStore,
			tenantID,
			req.Name,
			req.Description,
			req.Capabilities,
			req.Metadata,
		)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"agent": newAgent,
		})
	}
}

// ListAgentsHandler — GET /api/tenants/:id/agents
func ListAgentsHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		agents, err := agentStore.ListAgents(tenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to list agents",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"agents":    agents,
			"count":     len(agents),
		})
	}
}

// GetAgentHandler — GET /api/tenants/:id/agents/:agent_id
func GetAgentHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		agentID := c.Params("agent_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		ag, err := agentStore.GetAgent(tenantID, agentID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "agent not found",
			})
		}

		return c.JSON(fiber.Map{
			"agent": ag,
		})
	}
}

// UpdateAgentStatusHandler — PUT /api/tenants/:id/agents/:agent_id/status
func UpdateAgentStatusHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		agentID := c.Params("agent_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		var req struct {
			Status agent.AgentStatus `json:"status"`
		}

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		if req.Status != agent.AgentStatusActive &&
			req.Status != agent.AgentStatusInactive &&
			req.Status != agent.AgentStatusError {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid status",
			})
		}

		updated, err := agent.UpdateAgentStatus(agentStore, tenantID, agentID, req.Status)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "agent not found",
			})
		}

		return c.JSON(fiber.Map{
			"agent": updated,
		})
	}
}

// DeleteAgentHandler — DELETE /api/tenants/:id/agents/:agent_id
func DeleteAgentHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		agentID := c.Params("agent_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		if err := agentStore.DeleteAgent(tenantID, agentID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "agent not found",
			})
		}

		return c.JSON(fiber.Map{
			"status": "deleted",
		})
	}
}

// ============================================================
// TOOL HANDLERS
// ============================================================

// CreateToolHandler — POST /api/tenants/:id/tools
func CreateToolHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		var req struct {
			Name        string            `json:"name"`
			Description string            `json:"description"`
			Type        agent.ToolType    `json:"type"`
			Config      map[string]string `json:"config"`
		}

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		if req.Name == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "name is required",
			})
		}
		if req.Type == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "type is required",
			})
		}

		tool, err := agent.RegisterTool(
			agentStore,
			tenantID,
			req.Name,
			req.Description,
			req.Type,
			req.Config,
		)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"tool": tool,
		})
	}
}

// ListToolsHandler — GET /api/tenants/:id/tools
func ListToolsHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		tools, err := agentStore.ListTools(tenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to list tools",
			})
		}

		return c.JSON(fiber.Map{
			"tenant_id": tenantID,
			"tools":     tools,
			"count":     len(tools),
		})
	}
}

// GetToolHandler — GET /api/tenants/:id/tools/:tool_id
func GetToolHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		toolID := c.Params("tool_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		tool, err := agentStore.GetTool(tenantID, toolID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tool not found",
			})
		}

		return c.JSON(fiber.Map{
			"tool": tool,
		})
	}
}

// DeleteToolHandler — DELETE /api/tenants/:id/tools/:tool_id
func DeleteToolHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		toolID := c.Params("tool_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		if err := agentStore.DeleteTool(tenantID, toolID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tool not found",
			})
		}

		return c.JSON(fiber.Map{
			"status": "deleted",
		})
	}
}

// ExecuteToolHandler — POST /api/tenants/:id/tools/:tool_id/execute
func ExecuteToolHandler(agentStore *agent.BoltAgentStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Params("id")
		toolID := c.Params("tool_id")

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		// Get tool from store
		tool, err := agentStore.GetTool(tenantID, toolID)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "tool not found",
			})
		}

		// Parse request params
		var req struct {
			Params map[string]interface{} `json:"params"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		// Execute tool based on type (allowlisted handlers)
		start := time.Now()
		result, execErr := executeToolByType(tool, req.Params)
		executionMs := time.Since(start).Milliseconds()

		if execErr != nil {
			return c.JSON(fiber.Map{
				"success":      false,
				"error":        execErr.Error(),
				"execution_ms": executionMs,
			})
		}

		return c.JSON(fiber.Map{
			"success":      true,
			"result":       result,
			"execution_ms": executionMs,
			"tool": fiber.Map{
				"id":   tool.ID,
				"name": tool.Name,
				"type": tool.Type,
			},
		})
	}
}

// executeToolByType — allowlisted tool handlers
func executeToolByType(tool *agent.Tool, params map[string]interface{}) (interface{}, error) {
	switch tool.Type {
	case agent.ToolTypeHealthCheck:
		return map[string]interface{}{
			"status":    "healthy",
			"message":   "All systems operational",
			"timestamp": time.Now().Format(time.RFC3339),
		}, nil

	case agent.ToolTypeMetadataLookup:
		// Return metadata based on tool config
		metadata := make(map[string]string)
		if tool.Config != nil {
			for k, v := range tool.Config {
				metadata[k] = v
			}
		}
		metadata["lookup_time"] = time.Now().Format(time.RFC3339)
		return metadata, nil

	case agent.ToolTypeEcho:
		// Echo back the params
		return map[string]interface{}{
			"echo":   params,
			"tool":   tool.Name,
			"config": tool.Config,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported tool type: %s", tool.Type)
	}
}
