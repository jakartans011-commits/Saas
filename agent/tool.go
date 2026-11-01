package agent

import "time"

// ============================================================
// TOOL — fungsi yang bisa dipanggil oleh agent
// ============================================================

type ToolType string

const (
	ToolTypeHealthCheck    ToolType = "health_check"
	ToolTypeMetadataLookup ToolType = "metadata_lookup"
	ToolTypeEcho           ToolType = "echo"
	ToolTypeAuditLookup    ToolType = "audit_lookup" 
)

type Tool struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Type        ToolType          `json:"type"`
	Config      map[string]string `json:"config"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	DeletedAt   *time.Time        `json:"deleted_at,omitempty"` // GDPR soft delete
}

// ToolExecutionRequest — request untuk mengeksekusi tool
type ToolExecutionRequest struct {
	ToolID   string                 `json:"tool_id"`
	TenantID string                 `json:"-"`
	UserID   string                 `json:"-"`
	Params   map[string]interface{} `json:"params"`
}

// ToolExecutionResult — hasil eksekusi tool
type ToolExecutionResult struct {
	Success     bool              `json:"success"`
	Result      interface{}       `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	ExecutionMs int64             `json:"execution_ms"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
