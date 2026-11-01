// project/project_test.go
package project

import "testing"

func TestNewProject(t *testing.T) {
	t.Run("creates_project_with_defaults", func(t *testing.T) {
		p, err := NewProject("tenant-1", "My Project")
		if err != nil {
			t.Fatalf("NewProject failed: %v", err)
		}
		if p.TenantID != "tenant-1" {
			t.Fatalf("expected TenantID tenant-1, got %s", p.TenantID)
		}
		if p.Name != "My Project" {
			t.Fatalf("expected Name 'My Project', got %s", p.Name)
		}
		if p.Status != ProjectStatusActive {
			t.Fatalf("expected status active, got %s", p.Status)
		}
		if p.MonthlyEventLimit != DefaultMonthlyEventLimit {
			t.Fatalf("expected default limit %d, got %d", DefaultMonthlyEventLimit, p.MonthlyEventLimit)
		}
		if p.RetentionDays != DefaultRetentionDays {
			t.Fatalf("expected default retention %d, got %d", DefaultRetentionDays, p.RetentionDays)
		}
		if p.CreatedAt.IsZero() {
			t.Fatal("expected CreatedAt to be set")
		}
	})

	t.Run("empty_tenant_id_returns_error", func(t *testing.T) {
		_, err := NewProject("", "Some Project")
		if err == nil {
			t.Fatal("expected error for empty tenant_id")
		}
	})

	t.Run("empty_name_returns_error", func(t *testing.T) {
		_, err := NewProject("tenant-1", "")
		if err != ErrNameRequired {
			t.Fatalf("expected ErrNameRequired, got %v", err)
		}
	})
}