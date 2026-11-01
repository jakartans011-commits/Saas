// compliance/deletion_extra_test.go
package compliance

import (
	"context"
	"testing"

	"trailseal/agent"
	"trailseal/auth"
	"trailseal/tenant"
)

func TestDefaultDeletionConfig(t *testing.T) {
	cfg := DefaultDeletionConfig()
	if cfg.GracePeriod != DefaultGracePeriod {
		t.Errorf("expected GracePeriod %v, got %v", DefaultGracePeriod, cfg.GracePeriod)
	}
}

func TestSoftDeleteTenant_AlreadyDeleted(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "t1", Name: "T1", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "t1"); err != nil {
		t.Fatalf("first soft delete failed: %v", err)
	}
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "t1"); err == nil {
		t.Fatal("expected error deleting an already soft-deleted tenant")
	}
}

func TestSoftDeleteTenant_TenantNotFound(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestSoftDeleteAndRestoreTenant_WithAgentsAndTools(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "t1", Name: "T1", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	user := &auth.User{ID: "u1", TenantID: "t1", Email: "u1@test.test", Status: auth.UserStatusActive}
	if err := userStore.Put(user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	ag := &agent.Agent{ID: "a1", TenantID: "t1"}
	if err := agentStore.SaveAgent(ag); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	tool := &agent.Tool{ID: "tool1", TenantID: "t1"}
	if err := agentStore.SaveTool(tool); err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "t1"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	gotAgent, err := agentStore.GetAgentIncludingDeleted("t1", "a1")
	if err != nil {
		t.Fatalf("GetAgentIncludingDeleted failed: %v", err)
	}
	if gotAgent.DeletedAt == nil {
		t.Error("expected agent to be soft-deleted")
	}

	gotTool, err := agentStore.GetToolIncludingDeleted("t1", "tool1")
	if err != nil {
		t.Fatalf("GetToolIncludingDeleted failed: %v", err)
	}
	if gotTool.DeletedAt == nil {
		t.Error("expected tool to be soft-deleted")
	}

	deletedUsers, err := userStore.ListDeletedForTenant("t1")
	if err != nil {
		t.Fatalf("ListDeletedForTenant failed: %v", err)
	}
	if len(deletedUsers) != 1 {
		t.Errorf("expected 1 deleted user, got %d", len(deletedUsers))
	}

	if err := RestoreTenant(tenantStore, userStore, agentStore, "t1"); err != nil {
		t.Fatalf("RestoreTenant failed: %v", err)
	}

	restoredAgent, err := agentStore.GetAgent("t1", "a1")
	if err != nil {
		t.Fatalf("expected agent restored and accessible: %v", err)
	}
	if restoredAgent.DeletedAt != nil {
		t.Error("expected agent DeletedAt nil after restore")
	}

	restoredTool, err := agentStore.GetTool("t1", "tool1")
	if err != nil {
		t.Fatalf("expected tool restored and accessible: %v", err)
	}
	if restoredTool.DeletedAt != nil {
		t.Error("expected tool DeletedAt nil after restore")
	}

	restoredUser, err := userStore.GetByID("u1")
	if err != nil {
		t.Fatalf("expected user restored and accessible: %v", err)
	}
	if restoredUser.DeletedAt != nil {
		t.Error("expected user DeletedAt nil after restore")
	}
}

func TestRestoreTenant_NotSoftDeleted(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "t1", Name: "T1", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	if err := RestoreTenant(tenantStore, userStore, agentStore, "t1"); err == nil {
		t.Fatal("expected error restoring a tenant that isn't soft-deleted")
	}
}

func TestRestoreTenant_TenantNotFound(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	if err := RestoreTenant(tenantStore, userStore, agentStore, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestPurgeTenant_TenantNotFound(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)
	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "does-not-exist", nil); err == nil {
		t.Fatal("expected error for unknown tenant")
	}
}

func TestPurgeTenant_NotSoftDeleted(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "t1", Name: "T1", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)
	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "t1", nil); err == nil {
		t.Fatal("expected error purging a tenant that isn't soft-deleted")
	}
}

func TestPurgeTenant_UsesDefaultConfigWhenNil(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	tenantStore, _ := tenant.NewBoltTenantStore(db)
	userStore, _ := auth.NewBoltUserStore(db)
	agentStore, _ := agent.NewBoltAgentStore(db)

	tn := &tenant.Tenant{ID: "t1", Name: "T1", Status: tenant.TenantStatusActive}
	if err := tenantStore.Put(tn); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}
	if err := SoftDeleteTenant(tenantStore, userStore, agentStore, "t1"); err != nil {
		t.Fatalf("SoftDeleteTenant failed: %v", err)
	}

	// nil config -> DefaultDeletionConfig (30-day grace) -> still within it.
	projectStore, apiKeyStore, auditStore := newTestPurgeStores(db)
	if err := PurgeTenant(context.Background(), tenantStore, userStore, agentStore, projectStore, apiKeyStore, auditStore, "t1", nil); err == nil {
		t.Fatal("expected grace period error when config is nil right after soft delete")
	}
}