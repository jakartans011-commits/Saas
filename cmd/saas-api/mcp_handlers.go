package main

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"

	"trailseal/mcp"
	"trailseal/middleware"
)

// ============================================================
// MCP HANDLERS
// ============================================================

// MCPQueryHandler — POST /api/tenants/:id/mcp/query
func MCPQueryHandler(gateway *mcp.Gateway) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := strings.TrimSpace(c.Params("id"))

		if tenantID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "tenant_id is required",
			})
		}

		claims, ok := middleware.ClaimsFromFiberCtx(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthenticated",
			})
		}

		// The tenant in the authenticated JWT is authoritative.
		// MCP request body/context can never override it.
		if claims.TenantID != tenantID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "tenant mismatch",
			})
		}

		var req mcp.MCPRequest

		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_body",
			})
		}

		response, err := gateway.Query(
			tenantID,
			claims.UserID,
			&req,
		)
		if err != nil {
			return writeMCPError(c, err)
		}

		return c.Status(fiber.StatusOK).JSON(response)
	}
}

// writeMCPError memetakan MCP error taxonomy ke HTTP.
//
// Mapping:
//
//	invalid_request       -> 400
//	tool_not_found        -> 404
//	unsupported_tool_type -> 400
//	internal_error        -> 500
//
// Tenant mismatch tetap ditangani langsung oleh HTTP handler
// sebagai 403 karena itu merupakan authentication/authorization
// boundary, bukan protocol execution error.
func writeMCPError(
	c *fiber.Ctx,
	err error,
) error {

	if err == nil {
		return nil
	}

	var mcpErr *mcp.MCPError

	if errors.As(err, &mcpErr) && mcpErr != nil {
		switch mcpErr.Code {
		case mcp.MCPErrorInvalidRequest:
			return c.Status(
				fiber.StatusBadRequest,
			).JSON(fiber.Map{
				"error": mcpErr.Message,
			})

		case mcp.MCPErrorToolNotFound:
			return c.Status(
				fiber.StatusNotFound,
			).JSON(fiber.Map{
				"error": mcpErr.Message,
			})

		case mcp.MCPErrorUnsupportedTool:
			return c.Status(
				fiber.StatusBadRequest,
			).JSON(fiber.Map{
				"error": mcpErr.Message,
			})

		case mcp.MCPErrorInternal:
			return c.Status(
				fiber.StatusInternalServerError,
			).JSON(fiber.Map{
				"error": "mcp_internal_error",
			})
		}
	}

	// --------------------------------------------------------
	// Backward-compatible fallback.
	//
	// Dipertahankan agar error legacy dari dependency lain
	// tidak mengubah behavior existing secara tiba-tiba.
	// --------------------------------------------------------
	message := err.Error()
	lower := strings.ToLower(message)

	switch {
	case strings.Contains(lower, "request is required"),
		strings.Contains(lower, "query is required"),
		strings.Contains(lower, "tool_name is required"),
		strings.Contains(lower, "echo message is required"):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": message,
		})

	case strings.Contains(lower, "tool not found"),
		strings.Contains(lower, "no matching tool found"):
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": message,
		})

	case strings.Contains(lower, "unsupported tool type"):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": message,
		})

	case errors.Is(err, fiber.ErrBadRequest):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": message,
		})

	default:
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "mcp_internal_error",
		})
	}
}
