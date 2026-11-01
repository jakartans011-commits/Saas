package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ============================================================
// AGENT REGISTRY — helper untuk create/update agent
// ============================================================

func NewAgentID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "agent-" + hex.EncodeToString(b)
}

func NewToolID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "tool-" + hex.EncodeToString(b)
}

// RegisterAgent membuat agent baru dengan status active
func RegisterAgent(
	store *BoltAgentStore,
	tenantID string,
	name string,
	description string,
	capabilities []string,
	metadata map[string]string,
) (*Agent, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("agent name is required")
	}

	agent := &Agent{
		ID:           NewAgentID(),
		TenantID:     tenantID,
		Name:         name,
		Description:  description,
		Status:       AgentStatusActive,
		Capabilities: capabilities,
		Metadata:     metadata,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := store.SaveAgent(agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// UpdateAgentStatus mengubah status agent
func UpdateAgentStatus(
	store *BoltAgentStore,
	tenantID string,
	agentID string,
	status AgentStatus,
) (*Agent, error) {
	agent, err := store.GetAgent(tenantID, agentID)
	if err != nil {
		return nil, err
	}

	agent.Status = status
	agent.UpdatedAt = time.Now()

	if err := store.SaveAgent(agent); err != nil {
		return nil, err
	}
	return agent, nil
}

// RegisterTool membuat tool baru
func RegisterTool(
	store *BoltAgentStore,
	tenantID string,
	name string,
	description string,
	toolType ToolType,
	config map[string]string,
) (*Tool, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	if toolType == "" {
		return nil, fmt.Errorf("tool type is required")
	}

	// Validate tool type
	switch toolType {
	case ToolTypeHealthCheck, ToolTypeMetadataLookup, ToolTypeEcho, ToolTypeAuditLookup:
		// valid
	default:
		return nil, fmt.Errorf("invalid tool type: %s", toolType)
	}

	tool := &Tool{
		ID:          NewToolID(),
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		Type:        toolType,
		Config:      config,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.SaveTool(tool); err != nil {
		return nil, err
	}
	return tool, nil
}
