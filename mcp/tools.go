package mcp

import (
	"fmt" 
	"trailseal/agent"
)

// ============================================================
// MCP TOOLS
// ============================================================

// MCPToolDefinition adalah representasi tool pada MCP tools/list.
type MCPToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// MCPToolsListParams adalah params tools/list.
type MCPToolsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// MCPToolsListResult adalah result tools/list.
type MCPToolsListResult struct {
	Tools      []MCPToolDefinition `json:"tools"`
	NextCursor string              `json:"nextCursor,omitempty"`
}

// MCPToolsCallParams adalah params tools/call.
type MCPToolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// MCPContentBlock adalah content block MCP.
type MCPContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPToolsCallResult adalah result tools/call.
type MCPToolsCallResult struct {
	Content []MCPContentBlock `json:"content"`
	IsError bool              `json:"isError,omitempty"`
}

// buildMCPToolDefinition mengubah internal agent.Tool menjadi
// MCP tool definition.
//
// ID internal TIDAK diekspos sebagai MCP name.
// MCP name menggunakan tool.Name.
func buildMCPToolDefinition(
	tool *agent.Tool,
) MCPToolDefinition {
	definition := MCPToolDefinition{
		InputSchema: map[string]interface{}{
			"type": "object",
		},
	}

	if tool == nil {
		return definition
	}

	definition.Name = tool.Name

	switch tool.Type {
	case agent.ToolTypeEcho:
		definition.Description = "Echo back any message provided by the user. " +
        "This tool is useful for testing connectivity, validating the MCP gateway, " +
        "and simple response generation. Returns the exact message with prefix 'Echo: '. " +
        "Supports messages up to 16KB in size."

		definition.InputSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo.",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "Alternative text value to echo.",
				},
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Alternative query value to echo.",
				},
			},
		}

	case agent.ToolTypeHealthCheck:
		definition.Description = "Check the health status of all systems. " +
        "Returns current system status including database connectivity, " +
        "service availability, and response times. No parameters required."

		definition.InputSchema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}

	case agent.ToolTypeMetadataLookup:
		definition.Description = "Look up metadata and configuration details for this tool. " +
        "Returns tool metadata including creation time, configuration parameters, " +
        "and operational status. No parameters required."

		definition.InputSchema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}

	default:
		definition.Description = fmt.Sprintf("Execute the '%s' tool with provided parameters. "+
        "This tool is of type '%s' and accepts the specified input schema.", tool.Name, tool.Type)

		definition.InputSchema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}

	return definition
}

// buildMCPToolsList membuat hasil tools/list.
//
// Hanya tools yang sudah diberikan oleh tenant-scoped AgentStore
// yang boleh masuk ke discovery.
func buildMCPToolsList(
	tools []*agent.Tool,
) MCPToolsListResult {
	result := MCPToolsListResult{
		Tools: make([]MCPToolDefinition, 0, len(tools)),
	}

	for _, tool := range tools {
		if tool == nil {
			continue
		}

		result.Tools = append(
			result.Tools,
			buildMCPToolDefinition(tool),
		)
	}

	return result
}
