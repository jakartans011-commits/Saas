// mcp/executor_audit_test.go
package mcp

import (
	"testing"
	"time"

	"trailseal/agent"
	"trailseal/audit"
)

// fakeAuditStore adalah implementasi audit.Store in-memory untuk testing.
type fakeAuditStore struct {
	events []*audit.AuditEvent
	err    error
}

func (f *fakeAuditStore) Save(event *audit.AuditEvent) error { return nil }

func (f *fakeAuditStore) ListForTenant(tenantID string, limit int) ([]*audit.AuditEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	if limit < len(f.events) {
		return f.events[:limit], nil
	}
	return f.events, nil
}

func (f *fakeAuditStore) CleanupForTenant(tenantID string, policy *audit.RetentionPolicy) (deleted int, anonymized int, err error) {
	return 0, 0, nil
}

func (f *fakeAuditStore) DeleteAllForTenant(tenantID string) error {
	return nil
}

func newTestAuditStore() *fakeAuditStore {
	return &fakeAuditStore{events: []*audit.AuditEvent{
		{Action: "test.action", Resource: "test.resource", Outcome: audit.OutcomeSuccess, Timestamp: time.Now()},
		{Action: "test.action2", Resource: "test.resource2", Outcome: audit.OutcomeSuccess, Timestamp: time.Now()},
	}}
}

func TestExecuteAuditLookup(t *testing.T) {
	tool := &agent.Tool{Type: agent.ToolTypeAuditLookup}

	t.Run("default_limit_is_10", func(t *testing.T) {
		execCtx := MCPExecutionContext{TenantID: "tenant-1", AuditStore: newTestAuditStore()}
		result, err := executeAuditLookup(tool, map[string]interface{}{}, execCtx)
		if err != nil {
			t.Fatalf("executeAuditLookup failed: %v", err)
		}
		m := result.(map[string]interface{})
		query := m["query"].(map[string]interface{})
		if query["limit"] != 10 {
			t.Fatalf("expected default limit 10, got %v", query["limit"])
		}
		if query["tenant"] != "tenant-1" {
			t.Fatalf("expected tenant tenant-1, got %v", query["tenant"])
		}
	})

	t.Run("respects_custom_limit", func(t *testing.T) {
		execCtx := MCPExecutionContext{TenantID: "tenant-1", AuditStore: newTestAuditStore()}
		params := map[string]interface{}{"limit": float64(5)}
		result, err := executeAuditLookup(tool, params, execCtx)
		if err != nil {
			t.Fatalf("executeAuditLookup failed: %v", err)
		}
		m := result.(map[string]interface{})
		query := m["query"].(map[string]interface{})
		if query["limit"] != 5 {
			t.Fatalf("expected limit 5, got %v", query["limit"])
		}
	})

	t.Run("caps_limit_at_50", func(t *testing.T) {
		execCtx := MCPExecutionContext{TenantID: "tenant-1", AuditStore: newTestAuditStore()}
		params := map[string]interface{}{"limit": float64(999)}
		result, err := executeAuditLookup(tool, params, execCtx)
		if err != nil {
			t.Fatalf("executeAuditLookup failed: %v", err)
		}
		m := result.(map[string]interface{})
		query := m["query"].(map[string]interface{})
		if query["limit"] != 50 {
			t.Fatalf("expected limit capped at 50, got %v", query["limit"])
		}
	})

	t.Run("includes_events_and_total_count", func(t *testing.T) {
		execCtx := MCPExecutionContext{TenantID: "tenant-1", AuditStore: newTestAuditStore()}
		result, err := executeAuditLookup(tool, map[string]interface{}{}, execCtx)
		if err != nil {
			t.Fatalf("executeAuditLookup failed: %v", err)
		}
		m := result.(map[string]interface{})
		if m["total_count"] != 2 {
			t.Fatalf("expected total_count 2, got %v", m["total_count"])
		}
		events, ok := m["events"].([]map[string]interface{})
		if !ok || len(events) != 2 {
			t.Fatalf("expected 2 real events from store, got %v", m["events"])
		}
	})

	t.Run("nil_audit_store_returns_error", func(t *testing.T) {
		execCtx := MCPExecutionContext{TenantID: "tenant-1"}
		_, err := executeAuditLookup(tool, map[string]interface{}{}, execCtx)
		if err == nil {
			t.Fatal("expected error when AuditStore is nil, got nil")
		}
	})
}
