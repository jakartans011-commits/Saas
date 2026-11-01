package agent

import "time"

// ============================================================
// AGENT — unit eksekusi dalam tenant
// ============================================================

type AgentStatus string

const (
	AgentStatusActive   AgentStatus = "active"
	AgentStatusInactive AgentStatus = "inactive"
	AgentStatusError    AgentStatus = "error"
)

type Agent struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Status       AgentStatus       `json:"status"`
	Capabilities []string          `json:"capabilities"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	DeletedAt    *time.Time        `json:"deleted_at,omitempty"` // GDPR soft delete
}
